// Package recovery is init's recover and check phases: one session per
// capability, read-only over the repo, or one per share, one after another,
// when its sources are more than one session holds, at most Concurrency
// capabilities at once. A capability the walk found reads its area, its
// files and the PRs and changes placed with it; one proposed from the
// history reads what it cites and what coverage places with it by files.
// Each capability's text, split into its folder, goes through the map check
// and the path lint and back to the same session with the problems, up to
// MaxRetries times, before failing entries are dropped. Sessions read their sources whole, with no appetite
// and no circuit breaker: init is never bounded (design 10 Q6). It writes
// the store, never the repo.
package recovery

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/coverage"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recoverinput"
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
)

// The values of Outcome.Status, as the store keeps them.
const (
	StatusRecovered = "recovered"
	StatusNotYet    = "not yet recovered"
	StatusFailed    = "failed"
)

// ErrNoProposal is returned by Run when nothing is proposed, or walked and placed.
var ErrNoProposal = errors.New("nothing proposed yet; run physa init --propose first")

// Store is what Run needs of the store: the gather, the proposal or the walk
// and placements, the seed of recovery's order, and what was recovered.
type Store interface {
	Gathered() (store.Gather, bool, error)
	Proposed() (initrecord.Proposal, bool, error)
	Walk() (initrecord.Walk, bool, error)
	Place() (initrecord.Place, bool, error)
	RecoverSeed() (uint64, bool, error)
	PutRecoverSeed(uint64, time.Time) error
	PutRecovered([]initrecord.Recovered) error
	Recovered() ([]initrecord.Recovered, error)
}

// Session says how Run runs its sessions.
type Session struct {
	// Agent resolves the agent to recover with, once, and only when nothing
	// refuses the run first.
	Agent func() (adapter.Adapter, error)
	// Only names the capabilities to recover, in the order to run them; empty
	// means all: the walk's in recovery's seeded order, the proposal's in its.
	Only []string
	// Root is the repo's root, where sessions read and `from <path>` anchors
	// resolve. Nothing is written under it.
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

// Report is how one run ended: its source ("walk" or "proposal"), what its
// sessions used together, and one outcome per capability, in run order.
type Report struct {
	From         string         `json:"from"`
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
// the first and each retry, and Tokens is what they used together. Shares
// is how many shares its sources were read in, one session each, when they
// were more than one session holds; 0 is one session.
// SessionID is the id assigned to the session, empty when none started.
// Without its File and Terms, it is the JSON form of initrecord.Recovered's
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
	Shares    int                  `json:"shares,omitempty"`
	SessionID string               `json:"session_id"`
	Model     string               `json:"model"`
	Tokens    adapter.Tokens       `json:"tokens"`
	Error     string               `json:"error,omitempty"`
}

// job is one capability ready to run: its name and promise, its prompt and the
// input the prompt was written from. When the prompt is more than one
// session holds, parts are its sources laid into shares of room estimated
// tokens each, or err says why they could not be.
type job struct {
	capability mapping.Capability
	system     string
	prompt     string
	rec        recovering.Input
	parts      []reading.Input
	room       int
	err        error
}

// Run is init's recover and check phases end to end, from the walk and the
// placements when both are stored (a new gather, new areas or a new walk
// clears the later ones, so both are of the current gather), else from the
// stored proposal. For each capability of s.Only, or of the source, it runs
// one session, read-only in s.Root, with the recovering skill on its
// sources, whole, checks the text it writes, sends problems back up to
// MaxRetries times, then drops what is still at fault; its row replaces the
// capability's earlier one.
//
// Neither a walk with its placements nor a proposal (ErrNoProposal), and a
// name in s.Only the source lacks, are errors that resolve no agent and
// store nothing. A capability that fails, or that a cancelled run never
// reaches, is not an error: the Report says so.
func Run(ctx context.Context, st Store, s Session) (Report, error) {
	var rep Report
	p, proposed, err := st.Proposed()
	w, walked, werr := st.Walk()
	_, placed, perr := st.Place()
	if err = cmp.Or(err, werr, perr); err != nil {
		return rep, err
	}
	if walked = walked && placed; !walked && !proposed {
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
	if proposed {
		if err := json.Unmarshal(p.Body, &stored); err != nil {
			return rep, fmt.Errorf("the stored proposal cannot be read: %w", err)
		}
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}
	date := now().UTC()
	product, at, what := stored.Proposal.Product.Promise, p.At, "the proposal"
	var jobs []job
	if rep.From = "proposal"; walked {
		if !proposed {
			product = noPromise
		}
		at, rep.From, what = w.At, "walk", "the walk and placements"
		jobs, err = walkJobs(st, w, g, s, product, date)
	} else {
		jobs, err = proposalJobs(g, stored.Proposal.Capabilities, s.Only, product, date)
	}
	if err != nil {
		return rep, err
	}
	done, err := recoverinput.Kept(st, s.Only, at) // a continued run keeps what this gather and source recovered
	if err != nil {
		return rep, err
	}
	if len(done) > 0 {
		n := len(jobs)
		jobs = slices.DeleteFunc(jobs, func(j job) bool { return done[j.capability.Name] })
		if s.Progress != nil {
			fmt.Fprintln(s.Progress, recoverinput.KeptLine(n-len(jobs), len(jobs), what))
		}
		if len(jobs) == 0 {
			return rep, nil
		}
	}
	if s.Agent == nil {
		return rep, errors.New("no agent to recover with")
	}
	a, err := s.Agent()
	if err != nil {
		return rep, err
	}
	window := adapter.Window(a)
	for i := range jobs {
		if j := &jobs[i]; !recovering.Fits(j.system, j.prompt, window) { // shares, laid before any session starts
			j.room = window - reading.Tokens(j.system) - recovering.Reserve
			j.parts, j.err = j.rec.Lay(j.rec.Sources, j.room)
			if s.Progress != nil && j.err == nil {
				fmt.Fprintf(s.Progress, "physa init: %s: its sources are more than one session holds, so it is recovered in %d shares, one session after another, each writing the capability again from what the shares before it recovered\n", j.capability.Name, len(j.parts))
			}
		}
	}
	r := &runner{
		agent: a, progress: s.Progress,
		check: checker{product: product, root: s.Root, prExists: recoverinput.Resolver(g, s.PRExists), openRepo: s.OpenRepo},
	}
	if s.Progress != nil {
		fmt.Fprintf(s.Progress, "physa init: recovering %s, at most %d at once, from %s\n", recoverinput.Plural(len(jobs), "capability", "capabilities"), Concurrency, what)
	}
	rep.Capabilities = r.all(ctx, jobs)
	if err := dedupe(r.check, rep.Capabilities); err != nil {
		return rep, err
	}
	rows := make([]initrecord.Recovered, len(rep.Capabilities))
	for i := range rep.Capabilities {
		o := &rep.Capabilities[i]
		if o.Dropped == nil {
			o.Dropped = []recovering.Dropped{} // written [] and never null
		}
		if o.Problems == nil {
			o.Problems = []string{}
		}
		recoverinput.Add(&rep.Used, o.Tokens)
		d := *o
		d.File, d.Terms = "", ""
		details, err := json.Marshal(d)
		if err != nil {
			return rep, err
		}
		rows[i] = initrecord.Recovered{
			Capability: o.Name, ProposalAt: at, At: now().UTC(), Status: o.Status, File: o.File, Terms: o.Terms, KeepEarlier: o.Status != StatusRecovered,
			Details: details, SessionID: o.SessionID, Model: o.Model, Attempts: o.Attempts,
			Input: o.Tokens.Input, CachedInput: o.Tokens.CachedInput, Output: o.Tokens.Output,
		}
	}
	if err := st.PutRecovered(rows); err != nil {
		return rep, err
	}
	return rep, ctx.Err()
}

// newJob is the job of capability c, recovered from rec.
func newJob(c mapping.Capability, rec recovering.Input) job {
	system, prompt := recovering.Split(rec)
	return job{capability: c, system: system, prompt: prompt, rec: rec}
}

// proposalJobs are the jobs of the proposed capabilities only names, or all
// of them, in the proposal's order, each reading what it cites and what
// coverage places with it.
func proposalJobs(g store.Gather, proposed []mapping.Capability, only []string, product string, date time.Time) ([]job, error) {
	if len(proposed) == 0 {
		return nil, errors.New("the proposal holds no capability; run physa init --propose again")
	}
	names := make([]string, len(proposed))
	for i, c := range proposed {
		names[i] = c.Name
	}
	chosen, err := recoverinput.Choose(names, only, "no capability %q in the proposal; it proposes: %s")
	if err != nil {
		return nil, err
	}
	// Placed over every capability proposed, so --only reads what a full run would.
	pl, _ := coverage.Place(g, proposed)
	jobs := make([]job, len(chosen))
	for k, i := range chosen {
		c := proposed[i]
		jobs[k] = newJob(c, recovering.Input{Product: product, Name: c.Name, Promise: c.Promise,
			Questions: c.Questions, Date: date, Sources: reading.Of(recoverinput.Proposed(g, c, pl), recovering.Reads)})
	}
	return jobs, nil
}

// walkJobs are the jobs of the walk's capabilities s.Only names, in its
// order, or of all of them in recovery's order, each reading its area, its
// footprint and what the stored placement places with it. The seed of the
// order is the stored one, or the gather's, stored before any session
// starts, so a later run takes the same order.
func walkJobs(st Store, w initrecord.Walk, g store.Gather, s Session, product string, date time.Time) ([]job, error) {
	seed, ok, err := st.RecoverSeed()
	if !ok {
		seed = recoverinput.Seed(g)
	}
	var caps []recoverinput.Walked
	if err == nil {
		caps, err = recoverinput.Chosen(st, s.Root, w, g, s.Only, seed)
	}
	if err == nil && !ok && len(s.Only) == 0 {
		err = st.PutRecoverSeed(seed, date)
	}
	if err != nil {
		return nil, err
	}
	jobs := make([]job, len(caps))
	for i, c := range caps {
		jobs[i] = newJob(mapping.Capability{Name: c.Name, Promise: c.Promise}, recovering.Input{Product: product, Name: c.Name,
			Promise: c.Promise, Area: c.Area, Footprint: &c.Footprint, Date: date, Sources: reading.Of(c.Sources, recovering.Reads)})
	}
	return jobs, nil
}

// Whole reports whether every capability r holds got its session: none is
// left not yet recovered by a run cut short.
func (r Report) Whole() bool {
	return !slices.ContainsFunc(r.Capabilities, func(o Outcome) bool { return o.Status == StatusNotYet })
}
