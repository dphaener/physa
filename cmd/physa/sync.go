package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/github"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/prcheck"
	"github.com/dphaener/physa/internal/store"
)

// syncCheckName is the name of the check run the GitHub Action writes.
const syncCheckName = "physa"

// syncCounts is what one physa sync did.
type syncCounts struct {
	read, added, updated, unchanged, without, unreadable int
}

// sync reads the physa check runs of the open and merged PRs through gh and
// upserts them into the store of the repo in dir. Running it again changes
// nothing. It returns 0 when it synced and 2 when it could not.
func sync(dir string, stdout, stderr io.Writer) int {
	fail := func(msg string) int {
		fmt.Fprintf(stderr, "physa sync: %s\n", msg)
		return 2
	}
	repo, err := history.Open(dir)
	if err != nil {
		return fail(err.Error())
	}
	common, err := repo.CommonDir()
	if err != nil {
		return fail(err.Error())
	}
	st, err := store.Open(common)
	if err != nil {
		return fail(err.Error())
	}
	defer st.Close()

	client := newGitHub(dir)
	prs, err := client.PullRequests()
	if err != nil {
		switch {
		case errors.Is(err, github.ErrNoGH):
			return fail("gh is not installed")
		case errors.Is(err, github.ErrUnauthenticated):
			return fail("gh is not authenticated; run gh auth login")
		}
		return fail(err.Error())
	}
	var n syncCounts
	for _, pr := range prs {
		n.read++
		had, err := st.HasPRCheck(pr.Number, pr.Head)
		if err != nil {
			return fail(err.Error())
		}
		if had && pr.State == "merged" {
			n.unchanged++
			continue
		}
		runs, err := client.CheckRuns(pr.Head, syncCheckName)
		if err != nil {
			return fail(err.Error())
		}
		var best *github.CheckRun
		var result prcheck.Result
		unreadable := false
		for i := range runs {
			r := runs[i]
			if r.Status != "completed" || (best != nil && r.ID < best.ID) {
				continue
			}
			res, err := prcheck.Decode(r.Text)
			if err != nil {
				unreadable = true
				continue
			}
			best, result = &runs[i], res
		}
		if best == nil {
			if unreadable {
				n.unreadable++
			} else {
				n.without++
			}
			continue
		}
		data, err := json.Marshal(result)
		if err != nil {
			return fail(err.Error())
		}
		changed, err := st.PutPRCheck(store.PRCheck{
			PR: pr.Number, HeadSHA: pr.Head, CheckRunID: best.ID,
			CompletedAt: best.CompletedAt, Result: string(data),
		})
		if err != nil {
			return fail(err.Error())
		}
		switch {
		case !changed:
			n.unchanged++
		case had:
			n.updated++
		default:
			n.added++
		}
	}
	line := fmt.Sprintf("physa sync: %d PRs read, %d added, %d updated, %d unchanged, %d without a physa check run",
		n.read, n.added, n.updated, n.unchanged, n.without)
	if n.unreadable > 0 {
		line += fmt.Sprintf(", %d unreadable", n.unreadable)
	}
	fmt.Fprintln(stdout, line)
	return 0
}
