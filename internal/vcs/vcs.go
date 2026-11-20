// Package vcs is what Physa needs from version control, whatever holds the
// history: the changes of a range with their messages, whether a change
// exists, whether the history is incomplete, where Physa's store lives, and
// the files a change leaves in a directory. Nothing else in Physa reads
// version control except through a Repo (design 07 section 6); git is the
// only adapter today, in internal/vcs/git.
//
// Not built yet, because nothing uses them: what a change altered (a diff),
// detecting reverts, an isolated working copy for a run, and attaching a
// digest to a change. Each joins the interface when the engine needs it.
package vcs

import "errors"

// ErrNotRepo is returned by an adapter's Open when the directory is not under
// version control.
var ErrNotRepo = errors.New("not a git repository")

// Change is one change in the history.
type Change struct {
	ID      string // full, stable id
	Short   string // abbreviated id, for people
	Subject string // first line of the message
	Message string // the full message
}

// Repo is a history Physa can read.
type Repo interface {
	// Changes returns the non-merge changes of the range rng, newest first.
	// The adapter's own message is the error when the range is not valid.
	Changes(rng string) ([]Change, error)
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
