// Package forge is what Physa needs from the forge that hosts a repo's PRs,
// when there is one: whether a PR exists, and for a change the PR it came
// from with its review, discussion and commits. It also lets Physa open or
// refresh one PR, through PRs, and never merge, approve or auto-merge one.
// A forge that offers Merged also lists the PRs merged since a time, each
// with its review (marked when it requests changes) and its linked issues.
// Version control, never the
// forge, is the source of every fact (design 07 section 7); the forge only
// adds the review around a change. A forge that cannot answer says so with
// ErrUnavailable or ErrUnauthenticated, so a caller can report what needs a
// forge as unavailable rather than absent. GitHub is the first adapter, in
// internal/forge/github.
package forge

import (
	"errors"
	"time"
)

// Forge reads the forge that hosts a repo. It never writes to it.
type Forge interface {
	// Name names the forge, in lower case: "github".
	Name() string
	// PRExists reports whether the repo has a PR numbered n.
	PRExists(n int) (bool, error)
	// ChangePR returns the PR that change came from. Its bool is false when
	// the change came from no PR (it was pushed directly) or the forge does
	// not know the change.
	ChangePR(change string) (PR, bool, error)
}

// PRs opens and refreshes a PR. It is separate from Forge, which only reads.
// It has no way to merge, approve or enable auto-merge on a PR, so Physa
// never merges one itself (the no-self-merge rule): stewards accept, edit or
// drop what a PR holds.
type PRs interface {
	// OpenPR returns the open PR whose head is the branch head. Its bool is
	// false when none is open.
	OpenPR(head string) (n int, url string, ok bool, err error)
	// CreatePR opens a PR from branch head into branch base.
	CreatePR(head, base, title, body string) (n int, url string, err error)
	// EditPR replaces the body of PR n.
	EditPR(n int, body string) error
}

// Merged lists merged PRs. It is separate from Forge because an adapter may
// not offer it.
type Merged interface {
	// MergedPRs returns the PRs merged at or after since, newest merge first,
	// at most max. more is true when the limit stopped the list before the
	// date did.
	MergedPRs(since time.Time, max int) (prs []PR, more bool, err error)
}

// PR is a PR with the review around it.
type PR struct {
	Number int `json:"number"`
	// Author is the PR's author's login; empty when the forge no longer
	// knows them.
	Author string `json:"author"`
	// Comments are the PR's review and discussion by people, oldest first.
	// Bots' comments are left out.
	Comments []Comment `json:"comments"`
	// Commits are the PR's commits, oldest first.
	Commits []Commit `json:"commits"`
	// Title and Body are the PR's own words; MergedAt is when it was merged.
	// MergedPRs fills them, ChangePR does not.
	Title    string    `json:"title,omitempty"`
	Body     string    `json:"body,omitempty"`
	MergedAt time.Time `json:"merged_at,omitzero"`
	// CreatedAt is when the PR was opened. MergedPRs fills it.
	CreatedAt time.Time `json:"created_at,omitzero"`
	// MergeCommit is the id of the commit the merge made on the base branch;
	// empty when the forge names none. MergedPRs fills it.
	MergeCommit string `json:"merge_commit,omitempty"`
	// Issues are the issues the PR closes, as the forge links them.
	Issues []Issue `json:"issues,omitempty"`
}

// Issue is an issue a PR is linked to as closing.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// The kinds of Comment.
const (
	// KindReview is an inline review comment or a non-empty review body.
	KindReview = "review"
	// KindDiscussion is a comment on the PR's conversation.
	KindDiscussion = "discussion"
)

// Comment is one comment on a PR by a person.
type Comment struct {
	Author string    `json:"author"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"` // KindReview or KindDiscussion
	Body   string    `json:"body"`
	// RequestsChanges is true for a review that requests changes and for
	// that review's inline comments.
	RequestsChanges bool `json:"requests_changes,omitempty"`
}

// Commit is one commit of a PR. At is its committer time, so a commit that
// was amended and pushed again counts as later.
type Commit struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
	// Checks is the commit's check rollup state as the forge names it, such
	// as SUCCESS, FAILURE, ERROR or PENDING; empty when the commit has no
	// checks. MergedPRs fills it.
	Checks string `json:"checks,omitempty"`
}

// Sentinel errors, checked with errors.Is.
var (
	// ErrUnavailable means there is no forge to ask: its tool is not
	// installed, or the repo has no remote on a forge.
	ErrUnavailable = errors.New("no forge")
	// ErrUnauthenticated means the forge's tool has no usable login.
	ErrUnauthenticated = errors.New("not authenticated with the forge")
	// ErrNotFound means the forge answered that what was asked for does not
	// exist.
	ErrNotFound = errors.New("not found on the forge")
)

// Error is a forge that could not answer, of one of the sentinel classes.
// Reason says why in a few words a person can read, naming the tool;
// Detail is what the forge's tool said.
type Error struct {
	Class  error // ErrUnavailable, ErrUnauthenticated or ErrNotFound
	Reason string
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Reason
	}
	return e.Reason + ": " + e.Detail
}

// Unwrap returns the class, so errors.Is matches the sentinel.
func (e *Error) Unwrap() error { return e.Class }

// Reason returns the short reason of err when it is a forge Error, and
// err's own text otherwise.
func Reason(err error) string {
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Reason
	}
	return err.Error()
}
