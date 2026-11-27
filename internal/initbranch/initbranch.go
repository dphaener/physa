// Package initbranch writes init's map to the branch physa/init (design 02
// section 2.6) in an isolated working copy: the map's files and the AGENTS.md
// pointer, and nothing else. It commits only after the map passes the map
// check, pushes the branch with a lease and opens or refreshes its one PR,
// whose body summarises what init proposed, recovered, raised, dropped and
// spent. It never touches the person's own working copy, index or
// checked-out branch, and never merges: the person edits, deletes or merges
// the PR like any other.
package initbranch

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/initcost"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/vcs"
)

// Branch is the branch init writes its map to.
const Branch = "physa/init"

// Map is everything init writes: the product, its capabilities and terms,
// the sessions Physa started for them, and what the PR body summarises.
type Map struct {
	// ProductName is the title of the product's index.md.
	ProductName string
	// ProductPromise is the product's promise, on one line.
	ProductPromise string
	// ProductQuestions are the product-wide questions, raised by physa.
	ProductQuestions []Question
	// Capabilities are written in this order, one folder each.
	Capabilities []Capability
	// Terms are the glossary's terms, as glossary.md writes them.
	Terms string
	// Sessions are the ids of the sessions Physa started; each is written
	// once as a Session: trailer.
	Sessions []string
	// Date is the day the product's questions are raised on; zero means
	// today.
	Date time.Time
	// Summary is what the PR body says beside the files.
	Summary Summary
}

// Question is an undecided matter the sources suggest but do not settle.
type Question struct {
	// Question is the matter, one line ending with a question mark.
	Question string
	// SuggestedBy names the PRs ("#12") and changes (short ids) that
	// suggested it.
	SuggestedBy []string
}

// Capability is one capability of the map.
type Capability struct {
	// Name is the capability's name, a slug, and its folder's name.
	Name string
	// Promise is what the product promises its users, on one line.
	Promise string
	// File is the recovered capability as one text, split into its folder;
	// empty when not recovered, and the folder then holds the promise only.
	File string
	// Status is the recovery's status, such as "not yet recovered" or
	// "failed"; Reason says why, when there is no File.
	Status, Reason string
	// Dropped are the entries and terms recovery took out of the text.
	Dropped []Drop
}

// Drop is one thing taken out before the map was written, with its reason.
type Drop struct {
	// What names its kind: "rule", "term", "capability", "malformed item".
	What string
	// Value is the thing itself: a heading, a name, what the session wrote.
	Value string
	// Reason says why it was taken out.
	Reason string
}

// Summary is what the PR body reports beside the files.
type Summary struct {
	// Dropped are what proposing took out, with the malformed items of its
	// reply.
	Dropped []Drop
	// Tokens are the tokens spent, one row per phase.
	Tokens []Tokens
	// Hook is what happened to the Session: hook.
	Hook Hook
	// Skill is the line saying the agent init ran with has no current Physa
	// skill; empty when it has one or its state could not be read.
	Skill string
}

// Tokens are one phase's tokens, raw and weighted.
type Tokens = initcost.Row

// The values of Hook.State.
const (
	HookNotAsked  = ""          // the run did not ask for the hook
	HookInstalled = "installed" // the hook was installed
	HookAlready   = "already"   // the hook was already there
	HookRefused   = "refused"   // the hook was refused; Detail says why
)

// Hook is what happened to the Session: hook.
type Hook struct {
	State string
	// Detail is where the hook was installed, or why it was refused.
	Detail string
}

// Options say how far a run goes.
type Options struct {
	// Push pushes the branch to the default branch's remote, with a lease.
	Push bool
	// PR opens or refreshes the branch's PR; it needs Push.
	PR bool
	// PRs opens and refreshes the PR; nil means no forge to do it, and
	// ForgeReason says why.
	PRs         forge.PRs
	ForgeReason string
	// PRExists resolves #N anchors for the map check; nil checks their
	// form only.
	PRExists func(int) (bool, error)
}

// Commit is the change a run wrote to the branch.
type Commit struct {
	ID      string
	Short   string
	Subject string
}

// The states of PR.
const (
	PRNotAsked  = ""          // the run did not ask for a PR
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

// Outcome is what one run did.
type Outcome struct {
	// Commit is the change written to the branch; nil when nothing was.
	Commit *Commit
	// Files are the files written, relative to the repo root, sorted.
	Files []string
	// Body is the PR body for what was written.
	Body string
	// Pushed reports whether the branch was pushed.
	Pushed bool
	// PR is what happened to the branch's PR.
	PR PR
}

// state is what a run knows of the branch and the default branch.
type state struct {
	def    string // the default branch's ref
	name   string // the default branch's name, without its remote
	local  string // the local branch's change, "" when it has none
	remote string // the remote-tracking branch's change, "" when none
}

// Run writes m to the branch with w: it refuses a branch checked out in any
// working copy and a default branch that already holds a map, writes the
// map's files and the AGENTS.md pointer in a working copy at the default
// branch, checks the map there and commits only when it passes, then points
// the branch at that one change, replacing what it held. With o.Push it
// pushes first, with the remote's branch as the lease, and moves the local
// branch only after the remote took the change. With o.PR it opens the PR,
// or refreshes the open one. A lost lease is an error, never retried. The
// Outcome holds what was done even when it returns an error.
func Run(w vcs.Writer, m Map, o Options) (Outcome, error) {
	var out Outcome
	if o.PR && !o.Push {
		return out, errors.New("a PR needs the branch pushed")
	}
	if err := m.valid(); err != nil {
		return out, err
	}
	st, err := read(w, o.Push)
	if err != nil {
		return out, err
	}
	if err := inCopy(w, st.def, func(c vcs.Copy) error {
		return commit(w, c, m, o, &out)
	}); err != nil {
		return out, err
	}
	if o.Push {
		if err := w.Push(Branch, out.Commit.ID, st.remote); err != nil {
			return out, leaseError(err, "on the remote")
		}
		out.Pushed = true
	}
	if err := w.SetBranch(Branch, out.Commit.ID, st.local); err != nil {
		return out, leaseError(err, "here")
	}
	if o.PR {
		err = openPR(&out, st, o)
		if errors.Is(err, forge.ErrUnavailable) || errors.Is(err, forge.ErrUnauthenticated) {
			out.PR, err = PR{State: PRReady, Reason: forge.Reason(err)}, nil
		}
	}
	return out, err
}

// leaseError names the branch in a lost lease, where says where it moved.
func leaseError(err error, where string) error {
	if errors.Is(err, vcs.ErrLease) {
		return fmt.Errorf("%s moved %s while this run wrote it; nothing was replaced, run physa init --write again: %w", Branch, where, err)
	}
	return err
}

// commit writes m into the copy, checks it and commits it, filling out's
// Commit, Files and Body. A map that fails the check commits nothing.
func commit(w vcs.Writer, c vcs.Copy, m Map, o Options, out *Outcome) error {
	files, m, err := writeAll(c.Dir(), m)
	if err != nil {
		return err
	}
	res, err := mapcheck.CheckWith(c.Dir(), mapcheck.Options{PRExists: o.PRExists,
		OpenRepo: func(string) (vcs.Repo, error) { return w, nil }})
	if err != nil {
		return fmt.Errorf("the map init wrote cannot be read: %w", err)
	}
	if len(res.Problems) > 0 {
		lines := make([]string, len(res.Problems))
		for i, p := range res.Problems {
			lines[i] = "  " + p.String()
		}
		return fmt.Errorf("the map init wrote fails the map check, so nothing was committed:\n%s", strings.Join(lines, "\n"))
	}
	read, _, err := mapfile.Read(c.Dir())
	if err != nil {
		return err
	}
	ch, err := c.Commit(message(m))
	if err != nil {
		return err
	}
	out.Commit = &Commit{ID: ch.ID, Short: ch.Short, Subject: ch.Subject}
	out.Files, out.Body = files, body(m, read)
	return nil
}

// valid refuses a map that cannot be written: a product with no name, or a
// capability whose name is not a slug or is used twice.
func (m Map) valid() error {
	if strings.TrimSpace(m.ProductName) == "" {
		return errors.New("the product needs a name to title its index.md")
	}
	seen := map[string]bool{}
	for _, c := range m.Capabilities {
		if !mapfile.ValidSlug(c.Name) {
			return fmt.Errorf("capability %q: a name must be lowercase words joined by single hyphens", c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("capability %q is named twice", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

// read fetches the default branch and the init branch when pushing, refuses
// an init branch checked out anywhere and a default branch holding a map,
// and reads where each stands.
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
	if err := HasMap(w); err != nil {
		return st, err
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

// HasMap returns an error naming the file found and saying how to proceed
// when the default branch holds either of mapfolder.Products, else nil. Files
// lists only blobs directly under a directory, so each one's is listed.
func HasMap(w vcs.Writer) error {
	def, err := w.DefaultBranch()
	if err != nil {
		return err
	}
	for _, p := range mapfolder.Products {
		files, err := w.Files(def, path.Dir(p))
		if err != nil {
			return err
		}
		if slices.ContainsFunc(files, func(f string) bool { return path.Base(f) == path.Base(p) }) {
			return fmt.Errorf("the repo already has a map (%s on %s); init only starts a map, and learning keeps an existing one current: run physa map learn", p, def)
		}
	}
	return nil
}

// inCopy makes a working copy at def and calls fn with it. The copy is
// always removed.
func inCopy(w vcs.Writer, def string, fn func(vcs.Copy) error) (err error) {
	c, err := w.NewCopy(def)
	if err != nil {
		return err
	}
	defer func() {
		if rmErr := c.Remove(); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	return fn(c)
}

// message is the commit's message: its subject, a short body counting the
// capabilities by what recovery did, and one Session: trailer per session
// Physa started. It carries no Capability: trailer, as the change that adds
// the map serves no capability yet.
func message(m Map) string {
	var b strings.Builder
	b.WriteString(subject(m) + "\n\n")
	if len(m.Capabilities) == 0 {
		b.WriteString("A new codebase: the product's promise and first terms come from a person's\n" +
			"answers, and its capabilities come as the product grows.\n")
	} else {
		rec, notYet, failed := counts(m)
		fmt.Fprintf(&b, "Init proposed %d %s from the repo's docs, history and PRs:\n", len(m.Capabilities), plural(len(m.Capabilities), "capability", "capabilities"))
		fmt.Fprintf(&b, "%d recovered, %d not yet recovered and %d failed.", rec, notYet, failed)
		if notYet+failed > 0 {
			b.WriteString("\nThose not recovered hold their promise only, to be filled from future PRs.")
		}
		b.WriteString("\n")
	}
	seen := map[string]bool{}
	var trailers []string
	for _, s := range m.Sessions {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			trailers = append(trailers, "Session: "+s)
		}
	}
	if len(trailers) > 0 {
		b.WriteString("\n" + strings.Join(trailers, "\n") + "\n")
	}
	return b.String()
}

// subject is the commit's subject and the PR's title.
func subject(m Map) string {
	if len(m.Capabilities) == 0 {
		return "docs(map): Start the map for a new codebase"
	}
	return "docs(map): Add the map recovered by physa init"
}

// counts says how many capabilities were recovered, not yet recovered and
// failed. A capability with a recovered text is recovered.
func counts(m Map) (recovered, notYet, failed int) {
	for _, c := range m.Capabilities {
		switch {
		case c.File != "":
			recovered++
		case c.Status == statusFailed:
			failed++
		default:
			notYet++
		}
	}
	return recovered, notYet, failed
}

// plural is one when n is 1, else many.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
