// Package progress reads which of a pursuit's planned changes have landed: merged
// changes on the default branch carrying the pursuit's Change: trailers, read
// through the version-control interface.
package progress

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/vcs"
)

// Trailer is the key of the commit trailer that names a planned change as
// <pursuit>/<change>.
const Trailer = "Change"

// slack widens the window a little, so a commit made just before the pursuit is
// not missed for a clock's drift.
const slack = time.Minute

var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Repo is the smallest part of vcs.Repo that progress reads.
type Repo interface {
	DefaultBranch(ctx context.Context) (string, error)
	Since(ctx context.Context, ref string, t time.Time) ([]vcs.Change, error)
}

// Landing is the commit on the default branch where a change first landed.
type Landing struct {
	Commit string    `json:"commit"`
	Short  string    `json:"short"`
	At     time.Time `json:"at"`
}

// Parse splits a Change: trailer value, <pursuit>/<change>, into its two slugs.
// A value that is not exactly two slugs around one "/" is not ok.
func Parse(value string) (pursuit, change string, ok bool) {
	pursuit, change, found := strings.Cut(strings.TrimSpace(value), "/")
	if !found || !slug.MatchString(pursuit) || !slug.MatchString(change) {
		return "", "", false
	}
	return pursuit, change, true
}

// Landed returns, by change name, the first commit on the default branch
// since the given time that carries a Change: trailer naming the pursuit. A repo
// with no default branch gives an empty map and no error: what cannot be
// known is not guessed, so the caller shows none landed.
func Landed(repo Repo, pursuit string, since time.Time) (map[string]Landing, error) {
	ctx := context.TODO()
	landed := map[string]Landing{}
	ref, err := repo.DefaultBranch(ctx)
	if err != nil {
		return landed, nil
	}
	if !since.IsZero() {
		since = since.Add(-slack)
	}
	changes, err := repo.Since(ctx, ref, since)
	if err != nil {
		return nil, err
	}
	for _, c := range changes {
		for _, v := range history.Trailers(c.Message, Trailer) {
			b, name, ok := Parse(v)
			if !ok || b != pursuit {
				continue
			}
			if old, seen := landed[name]; seen && old.At.Before(c.Time) {
				continue
			}
			landed[name] = Landing{Commit: c.ID, Short: c.Short, At: c.Time}
		}
	}
	return landed, nil
}
