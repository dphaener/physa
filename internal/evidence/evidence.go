// Package evidence turns what version control and the forge show into
// evidence facts: a change landing, a change reverted, a merged PR's lead time
// and its CI result. Facts are observed, never reported and never judged: they
// carry no weight, and the package holds no store, so what to keep and how to
// count it is for the caller. Rework is not observed here: reading it from
// history is a guess, and a fact is never a guess (design 05 section 6.5).
package evidence

import (
	"time"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/progress"
	"github.com/dphaener/physa/internal/vcs"
)

// The kinds of Fact.
const (
	KindLanded             = "landed"
	KindReverted           = "reverted"
	KindLeadTime           = "lead_time"
	KindCIPassed           = "ci_passed"
	KindCIFailed           = "ci_failed"
	KindCIPassedAfterFails = "ci_passed_after_failing"
)

// The sources of a Fact.
const (
	SourceVCS   = "vcs"
	SourceForge = "forge"
)

// window is how far back a first look reaches: evidence is kept a year.
const window = 365 * 24 * time.Hour

// rewindSlack is how far before the watermark's time a look restarts when
// the watermark's change is no longer part of the default branch.
const rewindSlack = 24 * time.Hour

// Fact is one observation. The idea is never set here: it needs the store.
type Fact struct {
	Kind       string    `json:"kind"`
	Commit     string    `json:"commit,omitempty"`
	PR         int       `json:"pr,omitempty"`
	Capability string    `json:"capability,omitempty"`
	Bet        string    `json:"bet,omitempty"`
	Change     string    `json:"change,omitempty"` // the plan change's name
	Idea       string    `json:"idea,omitempty"`
	At         time.Time `json:"at"`
	Source     string    `json:"source"`
	Seconds    *int64    `json:"seconds,omitempty"` // lead time only
	ByCommit   string    `json:"by_commit,omitempty"`
}

// Watermark is how far observing has got: the default branch's head and its
// time, and the newest merged-at the forge showed. The zero Watermark is a
// first look.
type Watermark struct {
	Commit string    `json:"commit,omitempty"`
	Time   time.Time `json:"time,omitzero"`
	Forge  time.Time `json:"forge,omitzero"`
}

// Repo is the smallest part of vcs.Repo that observing reads.
type Repo interface {
	DefaultBranch() (string, error)
	Changes(rng string) ([]vcs.Change, error)
	Since(ref string, t time.Time) ([]vcs.Change, error)
	Reverts(c vcs.Change) (string, bool)
	Resolve(ref string) (string, bool)
	IsAncestor(a, b string) bool
	Latest(ref string, n int) ([]vcs.Change, error)
}

// Observe reads the changes landed since w and, when forge is not nil, the PRs
// merged since it, and returns their facts oldest first with the watermark to
// start the next look from. A forge that fails gives forgeErr, with the
// version-control facts still returned and the forge's watermark unmoved; a
// failure of version control is err. A repo with no default branch has no
// changes and is no error.
func Observe(repo Repo, forge Merged, w Watermark, now time.Time) (facts []Fact, next Watermark, forgeErr error, err error) {
	next = w
	facts, next, err = observeChanges(repo, w, now)
	if err != nil {
		return nil, w, nil, err
	}
	if forge != nil {
		var ff []Fact
		ff, next.Forge, forgeErr = observeForge(repo, forge, w.Forge, now)
		facts = append(facts, ff...)
	}
	return facts, next, forgeErr, nil
}

func observeChanges(repo Repo, w Watermark, now time.Time) ([]Fact, Watermark, error) {
	ref, err := repo.DefaultBranch()
	if err != nil {
		return nil, w, nil
	}
	var changes []vcs.Change
	switch {
	case w.Commit == "":
		changes, err = repo.Since(ref, now.Add(-window))
	case !repo.IsAncestor(w.Commit, ref):
		changes, err = repo.Since(ref, w.Time.Add(-rewindSlack))
	default:
		changes, err = repo.Changes(w.Commit + ".." + ref)
	}
	if err != nil {
		return nil, w, err
	}
	var facts []Fact
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		f := fromMessage(Fact{Kind: KindLanded, Commit: c.ID, At: c.Time, Source: SourceVCS}, c.Message)
		facts = append(facts, f)
		if rf, ok := reverted(repo, c); ok {
			facts = append(facts, rf)
		}
	}
	head, err := repo.Latest(ref, 1)
	if err != nil {
		return nil, w, err
	}
	next := w
	if len(head) == 1 {
		next.Commit, next.Time = head[0].ID, head[0].Time
	}
	return facts, next, nil
}

// reverted returns the fact for the change that c reverts, when c reverts one
// the repo has.
func reverted(repo Repo, c vcs.Change) (Fact, bool) {
	short, ok := repo.Reverts(c)
	if !ok {
		return Fact{}, false
	}
	id, ok := repo.Resolve(short)
	if !ok {
		return Fact{}, false
	}
	f := Fact{Kind: KindReverted, Commit: id, At: c.Time, Source: SourceVCS, ByCommit: c.ID}
	if old, err := repo.Latest(id, 1); err == nil && len(old) == 1 {
		f = fromMessage(f, old[0].Message)
	}
	return f, true
}

// fromMessage fills a fact's capability, bet and change from the trailers of
// a change's message.
func fromMessage(f Fact, message string) Fact {
	if v := history.Trailers(message, "Capability"); len(v) > 0 {
		f.Capability = v[0]
	}
	for _, v := range history.Trailers(message, progress.Trailer) {
		if bet, change, ok := progress.Parse(v); ok {
			f.Bet, f.Change = bet, change
			break
		}
	}
	return f
}
