// Package recovery is init's recover and check phases: one prompt-only
// session per capability, at most Concurrency at once, all under init's one
// appetite; each capability's text, split into its folder, goes through the
// map check and back to the same session with the problems, up to
// MaxRetries times, before failing entries are dropped. Sessions read their
// sources whole, and none starts unless every first turn fits the appetite;
// retries the appetite cannot hold leave answers unchecked. It writes the
// store, never the repo.
package recovery

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Each number below is a first guess, to be tuned from real runs
// (measure-before-budget).
const (
	// Concurrency is the most sessions running at once.
	Concurrency = 3
	// MaxRetries is how many times a capability's text with problems goes back
	// to its session before the failing entries are dropped.
	MaxRetries = 2
	// DefaultAppetite is how many tokens all of a run's sessions may use
	// together when the run names no appetite.
	DefaultAppetite = 300_000
	// ReplyReserve is what a session's first turn, and each retry, keeps of
	// the appetite for its reply.
	ReplyReserve = 12_000
)

// TooSmall is returned by Run when its Appetite cannot hold the whole prompt
// plus ReplyReserve of each of its Capabilities: Need, an estimate. Nothing
// is cut to make them fit, and no session started.
type TooSmall struct {
	Capabilities int
	Need         int64
	Appetite     int64
}

func (e *TooSmall) Error() string {
	return fmt.Sprintf("recovering %s needs an appetite of about %s tokens (each prompt plus %s for its reply) and has %s; no session started, and nothing was spent on recovering", plural(e.Capabilities, "capability", "capabilities"), reading.Thousands(e.Need), reading.Thousands(ReplyReserve), reading.Thousands(e.Appetite))
}

// The values of Outcome.Status, as the store keeps them.
const (
	StatusRecovered = "recovered"
	StatusNotYet    = "not yet recovered"
	StatusFailed    = "failed"
)

// ErrNoProposal is returned by Run when the store holds no proposal.
var ErrNoProposal = errors.New("nothing proposed yet; run physa init --propose first")

// Store is what Run needs of the store: the gather and the proposal to read,
// and a place for what was recovered.
type Store interface {
	Gathered() (store.Gather, bool, error)
	Proposed() (store.Proposal, bool, error)
	PutRecovered([]store.Recovered) error
}

// Session says how Run runs its sessions.
type Session struct {
	// Agent resolves the agent to recover with, once, and only when nothing
	// refuses the run first.
	Agent func() (adapter.Adapter, error)
	// Appetite is the most tokens all sessions may use together; zero means
	// DefaultAppetite.
	Appetite int64
	// Only names the capabilities to recover, in the order to run them;
	// empty means every proposed capability, in the proposal's order.
	Only []string
	// Root is the repo's root, where `from <path>` anchors resolve. Nothing
	// is written under it.
	Root string
	// PRExists tells whether a PR exists, asked only for numbers the gather
	// does not hold. Nil checks #N anchors for form only.
	PRExists func(int) (bool, error)
	// OpenRepo opens Root's version control to resolve commit anchors. Nil
	// leaves each unresolved, a problem.
	OpenRepo func(string) (vcs.Repo, error)
	// Progress receives one line before the sessions start and one as each
	// capability ends; nil drops them.
	Progress io.Writer
	// Now dates questions and stored rows; nil means time.Now.
	Now func() time.Time
}

// Report is how one run ended: the appetite the sessions shared, what they
// used together, and one outcome per capability, in run order.
type Report struct {
	Appetite     int64          `json:"appetite"`
	Used         adapter.Tokens `json:"used"`
	Capabilities []Outcome      `json:"capabilities"`
}

// Outcome is how one capability's recovery ended. File is the capability's
// single text, front matter included, which mapfolder.Split makes its folder
// of, and Terms its glossary terms, both empty unless Status is
// StatusRecovered; Counts says what they hold. Dropped lists the entries,
// terms and overview taken out, each with its reasons. Problems is what
// failed the capability: what the check found outside any entry, or why the
// answer could not be read. Error is any other reason it failed:
// the agent's error, or the check's. Attempts counts the session's turns,
// the first and each retry, and Tokens is what they used together.
// SessionID is the id assigned to the session, empty when none started.
// Without its File and Terms, it is the JSON form of store.Recovered's
// Details.
type Outcome struct {
	Name      string               `json:"name"`
	Status    string               `json:"status"`
	File      string               `json:"file"`
	Terms     string               `json:"terms"`
	Counts    recovering.Counts    `json:"counts"`
	Dropped   []recovering.Dropped `json:"dropped"`
	Problems  []string             `json:"problems"`
	Attempts  int                  `json:"attempts"`
	SessionID string               `json:"session_id"`
	Model     string               `json:"model"`
	Tokens    adapter.Tokens       `json:"tokens"`
	Error     string               `json:"error,omitempty"`
}

// job is one capability ready to run: its proposal, its prompt and what its
// first attempt reserves of the appetite.
type job struct {
	capability mapping.Capability
	prompt     string
	reserve    int64
}

// Run is init's recover and check phases end to end. For each capability
// (those of s.Only, or all the proposal holds) it runs one prompt-only
// session with the recovering skill on its linked sources, whole, checks the
// text it writes, sends problems back up to MaxRetries times, then drops
// what is still at fault; its row replaces the capability's earlier one.
//
// No proposal (ErrNoProposal), a name in s.Only the proposal lacks, and an
// appetite that cannot hold every first turn (*TooSmall) are errors that
// resolve no agent and store nothing. A capability that fails, or that the
// appetite does not reach, is not an error: the Report says so.
func Run(ctx context.Context, st Store, s Session) (Report, error) {
	appetite := cmp.Or(s.Appetite, DefaultAppetite)
	rep := Report{Appetite: appetite}
	p, ok, err := st.Proposed()
	if err != nil {
		return rep, err
	}
	if !ok {
		return rep, ErrNoProposal
	}
	g, ok, err := st.Gathered()
	if err != nil {
		return rep, err
	}
	if !ok {
		return rep, mapping.ErrNoGather
	}
	var stored mapping.Stored
	if err := json.Unmarshal(p.Body, &stored); err != nil {
		return rep, fmt.Errorf("the stored proposal cannot be read: %w", err)
	}
	chosen, err := choose(stored.Proposal.Capabilities, s.Only)
	if err != nil {
		return rep, err
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}
	date := now().UTC()

	jobs := make([]job, len(chosen))
	var need int64
	for i, c := range chosen {
		prompt := recovering.Prompt(recovering.Input{
			Product: stored.Proposal.Product.Promise, Name: c.Name, Promise: c.Promise,
			Questions: c.Questions, Date: date, Sources: reading.Of(sources(g, c), recovering.Reads)})
		jobs[i] = job{capability: c, prompt: prompt, reserve: int64(reading.Tokens(prompt) + ReplyReserve)}
		need += jobs[i].reserve
	}
	if need > appetite {
		return rep, &TooSmall{Capabilities: len(jobs), Need: need, Appetite: appetite}
	}
	if s.Agent == nil {
		return rep, errors.New("no agent to recover with")
	}
	a, err := s.Agent()
	if err != nil {
		return rep, err
	}
	r := &runner{
		agent: a, ledger: newLedger(appetite, need), progress: s.Progress,
		check: checker{product: stored.Proposal.Product.Promise, root: s.Root, prExists: resolver(g, s.PRExists), openRepo: s.OpenRepo},
	}
	if s.Progress != nil {
		fmt.Fprintf(s.Progress, "physa init: recovering %s, at most %d at once; appetite %s\n", plural(len(jobs), "capability", "capabilities"), Concurrency, plural(int(appetite), "token", "tokens"))
	}
	rep.Capabilities = r.all(ctx, jobs)
	if err := dedupe(r.check, rep.Capabilities); err != nil {
		return rep, err
	}
	rows := make([]store.Recovered, len(rep.Capabilities))
	for i := range rep.Capabilities {
		o := &rep.Capabilities[i]
		if o.Dropped == nil {
			o.Dropped = []recovering.Dropped{} // written [] and never null
		}
		if o.Problems == nil {
			o.Problems = []string{}
		}
		add(&rep.Used, o.Tokens)
		d := *o
		d.File, d.Terms = "", ""
		details, err := json.Marshal(d)
		if err != nil {
			return rep, err
		}
		rows[i] = store.Recovered{
			Capability: o.Name, ProposalAt: p.At, At: now().UTC(), Status: o.Status, File: o.File, Terms: o.Terms, KeepEarlier: o.Status != StatusRecovered,
			Details: details, SessionID: o.SessionID, Model: o.Model, Attempts: o.Attempts, Appetite: appetite,
			Input: o.Tokens.Input, CachedInput: o.Tokens.CachedInput, Output: o.Tokens.Output,
		}
	}
	if err := st.PutRecovered(rows); err != nil {
		return rep, err
	}
	return rep, ctx.Err()
}

// choose returns the capabilities to recover: those only names, in its
// order and each once, or all of them. A name the proposal lacks is an
// error naming it and the names proposed.
func choose(proposed []mapping.Capability, only []string) ([]mapping.Capability, error) {
	if len(proposed) == 0 {
		return nil, errors.New("the proposal holds no capability; run physa init --propose again")
	}
	if len(only) == 0 {
		return proposed, nil
	}
	var out []mapping.Capability
	names := make([]string, len(proposed))
	for i, c := range proposed {
		names[i] = c.Name
	}
	seen := map[string]bool{}
	for _, name := range only {
		i := slices.Index(names, name)
		if i < 0 {
			return nil, fmt.Errorf("no capability %q in the proposal; it proposes: %s", name, strings.Join(names, ", "))
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, proposed[i])
		}
	}
	return out, nil
}

// plural counts n things: "1 rule", "3 rules".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// add adds t to sum.
func add(sum *adapter.Tokens, t adapter.Tokens) {
	sum.Input += t.Input
	sum.CachedInput += t.CachedInput
	sum.Output += t.Output
}
