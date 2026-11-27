package gather

import (
	"context"
	"fmt"
	"time"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// readHistory returns the changes committed after since on the default
// branch, falling back to HEAD, with the merges of PRs on its first-parent
// line, each with its trailers, the change it reverts and the files it
// altered, a merge's against its first parent.
func readHistory(ctx context.Context, repo vcs.Repo, since time.Time) ([]store.GatheredChange, error) {
	ref, err := repo.DefaultBranch(ctx)
	if err != nil {
		ref = "HEAD"
	}
	changes, err := vcs.SinceWithPRMerges(ctx, repo, ref, since)
	if err != nil {
		return nil, fmt.Errorf("cannot read the history: %w", err)
	}
	out := make([]store.GatheredChange, 0, len(changes))
	for _, c := range changes {
		files, err := repo.Altered(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("cannot list the files of %s: %w", c.Short, err)
		}
		g := store.GatheredChange{
			ID: c.ID, Short: c.Short, Subject: c.Subject, Message: c.Message,
			Author: c.Author, At: c.Time, Files: files,
		}
		for _, kv := range history.TrailerBlock(c.Message) {
			g.Trailers = append(g.Trailers, store.Trailer{Key: kv[0], Value: kv[1]})
		}
		if id, ok := repo.Reverts(c); ok {
			g.Reverts = id
		}
		out = append(out, g)
	}
	return out, nil
}
