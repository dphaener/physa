package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/prcheck"
)

// parseReview reads the arguments after "review": only the flag --json. It
// reports false for anything else.
func parseReview(args []string) (asJSON bool, ok bool) {
	for _, a := range args {
		if a != "--json" {
			return false, false
		}
		asJSON = true
	}
	return asJSON, true
}

// reviewBranch runs the open-PR check on the current branch against the
// default branch, with the reviewer agent always, and prints the summary and
// the tokens the reviewer used. It returns 0 when the reviewer ran, 1 when it
// failed, and 2 when the check cannot run: the map or the repo cannot be read,
// there is no default branch or no commit on the branch, or the agent is not
// installed.
func reviewBranch(dir string, asJSON bool, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "physa review: %v\n", err)
		return 2
	}
	m, repo, err := openCheck(dir)
	if err != nil {
		return fail(err)
	}
	base, err := repo.DefaultBranch()
	if err != nil {
		return fail(err)
	}
	c, err := deterministic(dir, m, repo, base+"..HEAD", 0)
	if err != nil {
		return fail(err)
	}
	if len(c.r.Commits) == 0 {
		return fail(fmt.Errorf("no commits on this branch against %s", base))
	}
	title, body := c.headMessage()
	if err := c.runReviewer(title, body); errors.Is(err, adapter.ErrNotInstalled) {
		return fail(err)
	}
	if asJSON {
		err = json.NewEncoder(stdout).Encode(c.r)
	} else if err = prcheck.WriteMarkdown(stdout, c.r); err == nil {
		tokens := "not reported"
		if t := c.r.Reviewer.Tokens; t != nil {
			tokens = t.String()
		}
		_, err = fmt.Fprintf(stdout, "\nTokens: %s\n", tokens)
	}
	if err != nil {
		return fail(err)
	}
	if c.r.Reviewer.Status != prcheck.ReviewerRan {
		fmt.Fprintf(stderr, "physa review: the reviewer failed: %s\n", c.r.Reviewer.Reason)
		return 1
	}
	return 0
}
