// Package learnbranch writes one learning run to the physa/learn branch
// (design 08 section 6): it rebuilds the branch on the default branch in an
// isolated working copy, applies the run's kept proposals there and commits
// them with the record of what was learned (the Learned-through watermark
// and the Learned ids). It pushes the branch with a lease, redoing the write
// once without a second agent session when someone else moved it, and opens
// or refreshes its one PR. It never touches the person's own working copy,
// index or checked-out branch, and never merges: stewards accept, edit or
// drop what the PR holds.
package learnbranch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/learnbet"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/propose"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Branch is the branch learning writes to.
const Branch = "physa/learn"

// Options say what one run reads and how far it goes. The map's
// capabilities are read from the working copy the run writes in.
type Options struct {
	// Push pushes the branch to the default branch's remote, with a lease.
	Push bool
	// PR opens or refreshes the branch's PR; it needs Push.
	PR bool
	// Since is the change the run starts after; empty means after the
	// watermark (learn.Options.Since).
	Since string
	// Records are the slice records; RecordsReason, when not empty, says why
	// they could not be read.
	Records       []store.SliceRecord
	RecordsReason string
	// Session says how the run drafts and checks proposals.
	Session propose.Session
	// Sessions reads the session logs that join changes to their sessions;
	// nil means none.
	Sessions learn.Sessions
	// Bets finds the signals bets carry; nil means none are looked for.
	Bets learn.Bets
	// Forge is the forge to read each change's PR from; nil means none, and
	// ForgeReason says why.
	Forge       forge.Forge
	ForgeReason string
	// PRs opens and refreshes the branch's PR; nil means no forge to do it.
	PRs forge.PRs
}

// Commit is the change a run wrote to the branch.
type Commit struct {
	ID      string
	Short   string
	Subject string
}

// Outcome is what one run did.
type Outcome struct {
	// Report is what the run drafted and checked; its Kept are the proposals
	// applied to the branch and its Dropped every other one, with its reason.
	Report propose.Report
	// Result is the run's triage. After a redo its Through and LearnedNow are
	// the ones recorded.
	Result learn.Result
	// Commit is the change written to the branch; nil when nothing was.
	Commit *Commit
	// Pushed reports whether the branch was pushed.
	Pushed bool
	// Redone reports whether the write was redone after the lease was lost,
	// and Superseded how many applied proposals that dropped.
	Redone     bool
	Superseded int
	// PR is what happened to the branch's PR.
	PR PR
}

// The states of PR.
const (
	PRNotAsked  = ""          // the run did not ask for a PR
	PRNone      = "none"      // nothing to open or refresh
	PROpened    = "opened"    // a PR was opened
	PRRefreshed = "refreshed" // the open PR's body was rebuilt
	PRReady     = "ready"     // no forge to open one; the branch is ready
)

// PR is what a run did with the branch's PR.
type PR struct {
	State  string
	Number int
	URL    string
	// Reason says why there is no forge, for PRReady.
	Reason string
}

// leaseTwice is the error of a run whose redo lost the lease as well.
var leaseTwice = errors.New(Branch + " moved on the remote twice while this run wrote it; run physa map learn again")

// state is what a run knows of the branch and the default branch.
type state struct {
	def    string // the default branch's ref
	name   string // the default branch's name, without its remote
	local  string // the local branch's change, "" when it has none
	remote string // the remote-tracking branch's change, "" when none
}

// base is the change the branch is rebuilt from: the remote's when pushing,
// else the local branch's, else the remote's; "" when there is none.
func (s state) base(push bool) string {
	switch {
	case push && s.remote != "":
		return s.remote
	case s.local != "":
		return s.local
	}
	return s.remote
}

// Run writes one learning run to the branch with w: it refuses a branch
// checked out in any working copy, rebuilds the branch on the default
// branch in a working copy under the store, drafts from the copy's map,
// applies what is kept, commits when there is anything to record, and then
// pushes and opens or refreshes the PR as o asks. A session the circuit
// breaker stopped writes nothing and is no error (the Report's Session is
// SessionStopped). The Outcome holds what was done even when it returns an
// error.
func Run(ctx context.Context, w vcs.Writer, o Options) (Outcome, error) {
	var out Outcome
	if o.PR && !o.Push {
		return out, errors.New("a PR needs the branch pushed")
	}
	st, err := read(w, o.Push)
	if err != nil {
		return out, err
	}
	lo := learn.Options{Since: o.Since, Forge: o.Forge, ForgeReason: o.ForgeReason,
		Records: o.Records, RecordsReason: o.RecordsReason, Sessions: o.Sessions, Bets: o.Bets}
	var batch draft.Batch
	err = inCopy(w, st.def, st.base(o.Push), func(c vcs.Copy, carried []string) error {
		m, res, err := Read(c.Dir(), w, lo)
		if err != nil {
			return err
		}
		out.Result = res
		if out.Report, err = propose.Run(ctx, c.Dir(), m, out.Result, o.Session); err != nil {
			return err
		}
		if out.Report.Session == propose.SessionStopped {
			return nil
		}
		batch = draft.NewBatch(m, out.Result)
		batch.Person = o.Session.Person
		rec := record{changes: out.Result.Changes, watermark: out.Result.Watermark,
			through: out.Result.Through, learned: out.Result.LearnedNow, carried: carried,
			session: out.Report.DraftSession}
		out.Commit, err = apply(w, c, batch, &out.Report, out.Report.Kept, rec, o.Session.Check)
		return err
	})
	if err != nil || out.Report.Session == propose.SessionStopped {
		return out, err
	}
	if out.Commit != nil {
		if err := write(w, &out, st, o, lo, batch); err != nil {
			return out, err
		}
	}
	if o.PR {
		err = openPR(w, &out, st, o)
		if errors.Is(err, forge.ErrUnavailable) || errors.Is(err, forge.ErrUnauthenticated) {
			out.PR, err = PR{State: PRReady, Reason: forge.Reason(err)}, nil
		}
	}
	return out, err
}

// Read reads the map in dir and what the history r holds teaches it, with lo
// and its Capabilities taken from the map. It writes nothing.
func Read(dir string, r vcs.Repo, lo learn.Options) (*mapfile.Map, learn.Result, error) {
	m, _, err := mapfile.Read(dir)
	if err != nil {
		return m, learn.Result{}, err
	}
	lo.Capabilities = nil
	for _, c := range m.Capabilities {
		lo.Capabilities = append(lo.Capabilities, c.Name)
	}
	res, err := learn.Run(r, lo)
	return m, res, err
}

// WriteRun prints a run for people or, with asJSON, as JSON: the Report on
// stdout and, when branch is true, what the run did to the branch after it
// (on stderr with asJSON, so stdout stays valid JSON).
func WriteRun(stdout, stderr io.Writer, o Outcome, asJSON, branch bool) error {
	write, text := propose.WriteMarkdown, stdout
	if asJSON {
		write, text = propose.WriteJSON, stderr
	}
	if err := write(stdout, o.Report); err != nil || !branch {
		return err
	}
	return WriteText(text, o)
}

// write pushes the run's commit when o asks, redoing it once when the lease
// is lost, and then points the local branch at what was written. The local
// branch moves only after the remote took the commit, so it never records
// what the remote refused.
func write(w vcs.Writer, out *Outcome, st state, o Options, lo learn.Options, b draft.Batch) error {
	if o.Push {
		err := w.Push(Branch, out.Commit.ID, st.remote)
		if errors.Is(err, vcs.ErrLease) {
			out.Redone = true
			err = redo(w, out, lo, b, o.Session.Check)
		}
		if err != nil || out.Commit == nil {
			return err
		}
		out.Pushed = true
	}
	return w.SetBranch(Branch, out.Commit.ID, st.local)
}

// read fetches the default branch and the learn branch when pushing,
// refuses a learn branch checked out anywhere, and reads where each stands.
func read(w vcs.Writer, push bool) (state, error) {
	var st state
	def, err := w.DefaultBranch()
	if err != nil {
		return st, err
	}
	st.def, st.name = def, def
	if remote, name, ok := strings.Cut(def, "/"); ok && remote != "" {
		st.name = name
	}
	if push {
		if err := w.Fetch(st.name, Branch); err != nil {
			return st, err
		}
	}
	out, err := w.CheckedOut(Branch)
	if err != nil {
		return st, err
	}
	if out {
		return st, fmt.Errorf("%s is checked out in a working copy; switch that copy to another branch first, as Physa never writes a checked-out branch", Branch)
	}
	for _, ref := range w.Branch(Branch) {
		id, _ := w.Resolve(ref)
		if ref == Branch {
			st.local = id
		} else {
			st.remote = id
		}
	}
	return st, nil
}

// inCopy makes a working copy at def, replays onto it every change of
// def..base oldest first, and calls fn with it and the Learned ids of the
// replayed changes that added nothing. The copy is always removed.
func inCopy(w vcs.Writer, def, base string, fn func(vcs.Copy, []string) error) (err error) {
	c, err := w.NewCopy(def)
	if err != nil {
		return err
	}
	defer func() {
		if rmErr := c.Remove(); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	var carried []string
	if base != "" {
		changes, err := w.Changes(def + ".." + base)
		if err != nil {
			return err
		}
		for i := len(changes) - 1; i >= 0; i-- {
			ch := changes[i]
			added, err := c.Replay(ch.ID)
			if errors.Is(err, vcs.ErrConflict) {
				return fmt.Errorf("the unmerged change %s %q on %s conflicts with %s; drop or rebase it by hand, then run again", ch.Short, ch.Subject, Branch, def)
			}
			if err != nil {
				return err
			}
			if !added {
				carried = append(carried, history.Trailers(ch.Message, learnedKey)...)
			}
		}
	}
	return fn(c, carried)
}

// apply applies kept to the copy, records in report what was applied and
// dropped, and commits when there is anything to record: a proposal applied,
// a watermark that moves, or a bet learned from, which no watermark records.
// It returns nil when it commits nothing.
func apply(w vcs.Writer, c vcs.Copy, b draft.Batch, report *propose.Report, kept []propose.Kept, rec record, o propose.Options) (*Commit, error) {
	applied, dropped := propose.ApplyAll(c.Dir(), b, kept, o)
	report.Kept = append([]propose.Kept{}, applied...)
	report.Dropped = append(report.Dropped, dropped...)
	if len(applied) == 0 && rec.through == rec.watermark && rec.bets() == 0 {
		return nil, nil
	}
	rec.applied = applied
	ch, err := c.Commit(rec.message(w))
	if err != nil {
		return nil, err
	}
	return &Commit{ID: ch.ID, Short: ch.Short, Subject: ch.Subject}, nil
}

// redo writes the run again after the remote moved: it fetches, re-reads
// what is learned, drops the applied proposals whose changes, or whose bet,
// another run learned meanwhile, takes the later watermark, rebuilds on the
// remote's branch and pushes once more. No agent session is spent.
func redo(w vcs.Writer, out *Outcome, lo learn.Options, b draft.Batch, o propose.Options) error {
	first := out.Result
	st, err := read(w, true)
	if err != nil {
		return err
	}
	now, err := learn.Run(w, lo)
	if err != nil {
		return err
	}
	var meanwhile, learned []string
	for _, id := range first.LearnedNow {
		if contains(now.LearnedNow, id) {
			learned = append(learned, id)
		} else {
			meanwhile = append(meanwhile, id)
		}
	}
	var keep []propose.Kept
	for _, k := range out.Report.Kept {
		from, what := k.Proposal.Changes, "change "
		if k.Proposal.Signal.Kind == learn.KindBetPitch {
			from, what = betsOf(first, k.Proposal.Signal.Changes), ""
		}
		if gone := overlap(from, meanwhile); gone != "" {
			out.Report.Dropped = append(out.Report.Dropped, propose.Dropped{Proposal: k.Proposal,
				Reason: fmt.Sprintf("another run on %s learned from %s`%s` meanwhile", Branch, what, gone)})
			out.Superseded++
			continue
		}
		keep = append(keep, propose.Kept{Proposal: k.Proposal})
	}
	through := first.Through
	if now.Through != "" && (through == "" || w.IsAncestor(through, now.Through)) {
		through = now.Through
	}
	out.Result.Through, out.Result.LearnedNow = through, append([]string{}, learned...)
	out.Commit = nil
	err = inCopy(w, st.def, st.base(true), func(c vcs.Copy, carried []string) error {
		rec := record{changes: now.Changes, watermark: now.Watermark, through: through,
			learned: learned, carried: carried, session: out.Report.DraftSession}
		var err error
		out.Commit, err = apply(w, c, b, &out.Report, keep, rec, o)
		return err
	})
	if err != nil || out.Commit == nil {
		return err
	}
	err = w.Push(Branch, out.Commit.ID, st.remote)
	if errors.Is(err, vcs.ErrLease) {
		return leaseTwice
	}
	return err
}

// betsOf returns the ids of the run's bets whose signal is about changes: a
// proposal from a bet's pitch names the change the bet was made on, which
// says nothing of whether the bet was learned from.
func betsOf(r learn.Result, changes []string) []string {
	var ids []string
	for _, c := range r.Learn {
		for _, s := range c.Signals {
			if s.Kind == learn.KindBetPitch && overlap(s.Changes, changes) != "" {
				ids = append(ids, c.Short)
			}
		}
	}
	return ids
}

// contains reports whether ids holds a short id naming the same change as id.
func contains(ids []string, id string) bool {
	return overlap([]string{id}, ids) != ""
}

// overlap returns the first of changes that names the same change as one of
// ids, or "".
func overlap(changes, ids []string) string {
	for _, c := range changes {
		for _, id := range ids {
			if sameChange(c, id) {
				return c
			}
		}
	}
	return ""
}

// sameChange reports whether two ids, full or abbreviated, name one change:
// one is a prefix of the other, at least seven characters long. A bet's id
// is never abbreviated, so it names only itself.
func sameChange(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if learnbet.IsID(a) || learnbet.IsID(b) {
		return a == b
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return len(a) >= 7 && strings.HasPrefix(b, a)
}
