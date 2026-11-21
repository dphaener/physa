// Package vcs is what Physa needs from version control, whatever holds the
// history: the changes of a range with their messages and times, the newest
// changes of a ref, whether a change exists, which change a change says it
// reverts, the shared default branch and the refs that hold a named branch,
// whether the history is incomplete, where Physa's store lives, and the files
// a change leaves in a directory. Nothing else in Physa reads version control
// except through a Repo (design 07 section 6); git is the only adapter today,
// in internal/vcs/git.
//
// A revert is a change whose message says which change it reverts. Each
// system writes that its own way and an adapter reads its form: git's
// "This reverts commit <id>." (built) and Mercurial's backout message "Backed
// out changeset <id>" (the form a Mercurial adapter reads).
//
// Not built yet, because nothing uses them: what a change altered (a diff), an
// isolated working copy for a run, and attaching a digest to a change. Each
// joins the interface when the engine needs it.
package vcs

import (
	"errors"
	"time"
)

// ErrNotRepo is returned by an adapter's Open when the directory is not under
// version control.
var ErrNotRepo = errors.New("not a repository under version control")

// Change is one change in the history.
type Change struct {
	ID      string    // full, stable id
	Short   string    // abbreviated id, for people
	Subject string    // first line of the message
	Message string    // the full message
	Time    time.Time // when the change was committed
}

// Repo is a history Physa can read.
type Repo interface {
	// Changes returns the non-merge changes of the range rng, newest first.
	// The adapter's own message is the error when the range is not valid.
	Changes(rng string) ([]Change, error)
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
}
