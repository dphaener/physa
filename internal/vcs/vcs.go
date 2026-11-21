// Package vcs is what Physa needs from version control, whatever holds the
// history: the changes of a range with their messages and times, the newest
// changes of a ref, whether a change exists, which change a change says it
// reverts, the shared default branch and the refs that hold a named branch,
// whether the history is incomplete, where Physa's store lives, the files a
// change leaves in a directory, the change a ref names, the newest change
// carrying a trailer, whether one change is part of another's history, the
// changes committed after a time, and the paths a change altered.
// Nothing else in Physa reads version control except through a Repo (design
// 07 section 6); git is the only adapter today, in internal/vcs/git.
//
// Writing goes only through a Writer, which learning uses for its branch
// (design 08 section 6): it fetches branches from the default branch's
// remote, makes an isolated working copy for a run, moves a branch only when
// it still holds the change the caller expects, and pushes with a lease. A
// Writer never touches the person's own working copy, index or checked-out
// branch.
//
// A repo's hooks path can be read and set through Hooks (in hooks.go).
//
// A revert is a change whose message says which change it reverts. Each
// system writes that its own way and an adapter reads its form: git's
// "This reverts commit <id>." (built) and Mercurial's backout message "Backed
// out changeset <id>" (the form a Mercurial adapter reads).
//
// Since lists the changes committed after a time, and Altered the paths a
// change altered (hints only; no diff). Not built yet, because nothing uses
// it: attaching a digest to a change. It joins the interface when the engine
// needs it.
package vcs

import (
	"errors"
	"time"
)

// ErrNotRepo is returned by an adapter's Open when the directory is not under
// version control.
var ErrNotRepo = errors.New("not a repository under version control")

// ErrLease is returned by a Writer when a branch no longer holds the change
// the caller expected, here or on the remote: someone else moved it first.
var ErrLease = errors.New("the branch moved since it was read")

// ErrConflict is returned by Copy.Replay when a change's edits conflict with
// the working copy. The copy is left as it was before the replay.
var ErrConflict = errors.New("the change conflicts with the working copy")

// Change is one change in the history.
type Change struct {
	ID      string    // full, stable id
	Short   string    // abbreviated id, for people
	Subject string    // first line of the message
	Message string    // the full message
	Author  string    // the author's name, as version control records it
	Time    time.Time // when the change was committed
}

// Repo is a history Physa can read.
type Repo interface {
	// Changes returns the non-merge changes of the range rng, newest first.
	// The adapter's own message is the error when the range is not valid.
	Changes(rng string) ([]Change, error)
	// Since returns the non-merge changes reachable from ref committed after
	// t, newest first. A zero t means all of them. The adapter's own message
	// is the error when ref is not valid.
	Since(ref string, t time.Time) ([]Change, error)
	// Altered returns the paths change id altered, relative to the repo root
	// and sorted. A change with no paths, such as an empty one, gives an
	// empty list. The adapter's own message is the error when id is not valid.
	Altered(id string) ([]string, error)
	// Latest returns the newest n non-merge changes reachable from ref, newest
	// first. The adapter's own message is the error when ref is not valid.
	Latest(ref string, n int) ([]Change, error)
	// Reverts returns the id, as written in c's message, of the change c says
	// it reverts. The id may be abbreviated.
	Reverts(c Change) (id string, ok bool)
	// DefaultBranch returns the ref of the shared default branch, preferring
	// the remote's. It is an error when none can be found.
	DefaultBranch() (string, error)
	// Branch returns the refs that hold the branch name here and on the
	// default branch's remote, only those that exist. It is empty when
	// neither does.
	Branch(name string) []string
	// HasChange reports whether id, full or abbreviated, names exactly one
	// change. A missing or ambiguous id is false.
	HasChange(id string) bool
	// Partial reports whether the history is incomplete, as in a shallow
	// clone.
	Partial() bool
	// StoreDir returns the absolute directory where Physa's store lives,
	// shared by every working copy of the repo.
	StoreDir() (string, error)
	// Files returns the names of the files directly under dir, relative to
	// the repo's working directory, as change id leaves them. It is empty
	// when dir is absent.
	Files(id, dir string) ([]string, error)
	// Resolve returns the full id of the change ref names, false when ref
	// names none.
	Resolve(ref string) (id string, ok bool)
	// Trailer returns the newest non-merge change reachable from ref whose
	// trailers, never its body, carry key, matched case-insensitively. It is
	// false when no change does. The adapter's own message is the error when
	// ref is not valid.
	Trailer(ref, key string) (Change, bool, error)
	// IsAncestor reports whether change a is change b or part of b's history.
	// It is false when either does not exist.
	IsAncestor(a, b string) bool
}

// Writer is a history Physa can read and write, for learning's branch. It
// writes only refs it is asked to and isolated working copies it makes;
// never the person's working copy, index or checked-out branch.
type Writer interface {
	Repo
	// Fetch brings each branch from the default branch's remote into its
	// remote-tracking ref. A branch the remote lacks is no error, and a stale
	// remote-tracking ref for it is deleted.
	Fetch(branches ...string) error
	// CheckedOut reports whether any working copy of the repo has branch
	// checked out.
	CheckedOut(branch string) (bool, error)
	// NewCopy makes an isolated working copy, not on any branch, at the change
	// base names, in a fresh directory under StoreDir. The caller removes it.
	NewCopy(base string) (Copy, error)
	// SetBranch points branch name at change id, only if it still holds old;
	// old "" means it must not exist yet. A mismatch is ErrLease. A branch
	// checked out in any working copy is refused.
	SetBranch(name, id, old string) error
	// Push sets the remote's branch to change id, only if the remote still
	// holds expect; expect "" means the remote must not have the branch. A
	// stale lease is ErrLease.
	Push(branch, id, expect string) error
}

// Copy is an isolated working copy made by a Writer.
type Copy interface {
	// Dir returns the copy's absolute working directory.
	Dir() string
	// Replay applies change id's edits to the copy and records them as a new
	// change with id's message and author. It is false, recording nothing,
	// when the edits change nothing. A conflict is ErrConflict and leaves the
	// copy as it was.
	Replay(id string) (bool, error)
	// Commit records everything in the copy as a new change with message,
	// even when nothing changed, and returns it.
	Commit(message string) (Change, error)
	// Remove deletes the copy and forgets it.
	Remove() error
}
