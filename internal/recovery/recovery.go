// Package recovery is init's recover and check phases: one session per
// capability, read-only over the repo, or one per share, one after another,
// when its sources are more than one session holds, at most Concurrency
// capabilities at once. Each capability the walk found reads its area, its
// files and the PRs and changes placed with it, and every session is given
// the product's promise as init's record holds it. Each capability's text, split into its folder, goes through the map check
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
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/productpromise"
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

// ErrNoWalk is returned by Run when nothing is walked and placed.
var ErrNoWalk = errors.New("nothing walked and placed yet; run physa init --areas, --walk and --place first")

// earlier is what Run adds to ErrNoWalk when init's record holds rows
// recovered from a proposal an earlier physa stored.
const earlier = "the capabilities an earlier physa recovered from a proposal can no longer be written; a new physa init replaces them"

// Store is what Run needs of the store: the gather, the areas (for the
// product's promise), the walk and placements, the seed of recovery's order,
// and what was recovered.
type Store interface {
	Gathered(ctx context.Context) (store.Gather, bool, error)
	Areas(ctx context.Context) (initrecord.Areas, bool, error)
	Walk(ctx context.Context) (initrecord.Walk, bool, error)
	Place(ctx context.Context) (initrecord.Place, bool, error)
	RecoverSeed(ctx context.Context) (uint64, bool, error)
	PutRecoverSeed(context.Context, uint64, time.Time) error
	PutRecovered(context.Context, []initrecord.Recovered) error
	Recovered(ctx context.Context) ([]initrecord.Recovered, error)
}

// Session says how Run runs its sessions.
type Session struct {
	// Agent resolves the agent to recover with, once, and only when nothing
	// refuses the run first.
	Agent func() (adapter.Adapter, error)
	// Only names the capabilities to recover, in the order to run them; empty
	// means all of the walk's, in recovery's seeded order.
	Only []string
	// Root is the repo's root, where sessions read and `from <path>` anchors
	// resolve. Nothing is written under it.
	Root string
	// PRExists tells whether a PR exists, asked only for numbers the gather
	// does not hold. Nil checks #N anchors for form only.
	PRExists func(context.Context, int) (bool, error)
	// OpenRepo opens Root's version control to resolve commit anchors. Nil
	// leaves each unresolved, a problem.
	OpenRepo func(context.Context, string) (vcs.Repo, error)
	// Progress receives one line before the sessions start and one as each
	// capability ends; nil drops them.
	Progress io.Writer
	// Now dates questions and stored rows; nil means time.Now.
	Now func() time.Time
}

// Report is how one run ended: its source (always "walk", kept so readers
// of --json keep the key), what its sessions used together, and one outcome
// per capability, in run order.
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
	capability recoverinput.Capability
	system     string
	prompt     string
	rec        recovering.Input
	parts      []reading.Input
	room       int
	err        error
}

// Run is init's recover and check phases end to end, from the walk and the
// placements (a new gather, new areas or a new walk clears the later ones,
// so both are of the current gather). For each capability of s.Only, or of
// the walk, it runs one session, read-only in s.Root, with the recovering
// skill on its sources, whole, and the product's promise the stored areas
// state (productpromise.NoPromise when none is), checks the text it writes,
// sends problems back up to MaxRetries times, then drops what is still at
// fault; its row replaces the capability's earlier one.
//
// No walk with its placements (ErrNoWalk, saying so when the record holds
// rows an earlier physa recovered from a proposal), and a name in s.Only the
// walk lacks, are errors that resolve no agent and store nothing. A
// capability that fails, or that a cancelled run never reaches or cuts
// short, is not an error: the Report says so. A cancelled run still stores
// every capability's row, the recovered ones kept, and then returns ctx's
// error.
func Run(ctx context.Context, st Store, s Session) (Report, error) {
	var rep Report
	w, walked, err := st.Walk(ctx)
	_, placed, perr := st.Place(ctx)
	if err = cmp.Or(err, perr); err != nil {
		return rep, err
	}
	if !walked || !placed {
		return rep, noWalk(ctx, st, walked)
	}
	g, ok, err := st.Gathered(ctx)
	if err != nil {
		return rep, err
	}
	if !ok {
		return rep, store.ErrNoGather
	}
	p, _, err := productpromise.Stored(ctx, st)
	if err != nil {
		return rep, err
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}
	date := now().UTC()
	product, at, what := p.PromiseOr(), w.At, "the walk and placements"
	rep.From = "walk"
	jobs, err := walkJobs(ctx, st, w, g, s, product, date)
	if err != nil {
		return rep, err
	}
	done, err := recoverinput.Kept(ctx, st, s.Only, at) // a continued run keeps what this gather and source recovered
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
	if err := st.PutRecovered(context.WithoutCancel(ctx), rows); err != nil { // a cancel keeps what finished
		return rep, err
	}
	return rep, ctx.Err()
}

// noWalk is ErrNoWalk, with the sentence about an earlier physa's
// recoveries when st holds rows and no walk: those rows can only be of a
// proposal, since every walk clears the rows recovered before it.
func noWalk(ctx context.Context, st Store, walked bool) error {
	if rows, err := st.Recovered(ctx); err != nil {
		return err
	} else if !walked && len(rows) > 0 {
		return fmt.Errorf("%w; %s", ErrNoWalk, earlier)
	}
	return ErrNoWalk
}

// newJob is the job of capability c, recovered from rec.
func newJob(c recoverinput.Capability, rec recovering.Input) job {
	system, prompt := recovering.Split(rec)
	return job{capability: c, system: system, prompt: prompt, rec: rec}
}

// walkJobs are the jobs of the walk's capabilities s.Only names, in its
// order, or of all of them in recovery's order, each reading its area, its
// footprint and what the stored placement places with it. The seed of the
// order is the stored one, or the gather's, stored before any session
// starts, so a later run takes the same order.
func walkJobs(ctx context.Context, st Store, w initrecord.Walk, g store.Gather, s Session, product string, date time.Time) ([]job, error) {
	seed, ok, err := st.RecoverSeed(ctx)
	if !ok {
		seed = recoverinput.Seed(g)
	}
	var caps []recoverinput.Walked
	if err == nil {
		caps, err = recoverinput.Chosen(ctx, st, s.Root, w, g, s.Only, seed)
	}
	if err == nil && !ok && len(s.Only) == 0 {
		err = st.PutRecoverSeed(ctx, seed, date)
	}
	if err != nil {
		return nil, err
	}
	jobs := make([]job, len(caps))
	for i, c := range caps {
		jobs[i] = newJob(recoverinput.Capability{Name: c.Name, Promise: c.Promise}, recovering.Input{Product: product, Name: c.Name,
			Promise: c.Promise, Area: c.Area, Footprint: &c.Footprint, Date: date, Sources: reading.Of(c.Sources, recovering.Reads)})
	}
	return jobs, nil
}

// Recovered is how many capabilities r holds as recovered.
func (r Report) Recovered() int {
	n := 0
	for _, o := range r.Capabilities {
		if o.Status == StatusRecovered {
			n++
		}
	}
	return n
}

// Whole reports whether every capability r holds got its session: none is
// left not yet recovered by a run cut short.
func (r Report) Whole() bool {
	return !slices.ContainsFunc(r.Capabilities, func(o Outcome) bool { return o.Status == StatusNotYet })
}
