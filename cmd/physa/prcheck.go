package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/github"
	"github.com/dphaener/physa/internal/prcheck"
)

// newGitHub builds the client that reads GitHub through gh. Tests replace it
// so that none of them reaches GitHub.
var newGitHub = func(dir string) *github.Client { return github.New(dir) }

// prResolver returns the function physa check uses to resolve #N anchors. A
// missing or unauthenticated gh becomes a plain reason, so the check falls
// back to form only.
func prResolver(dir string) func(n int) (bool, error) {
	client := newGitHub(dir)
	return func(n int) (bool, error) {
		ok, err := client.PRExists(n)
		switch {
		case errors.Is(err, github.ErrNoGH):
			return false, errors.New("gh is not installed")
		case errors.Is(err, github.ErrUnauthenticated):
			return false, errors.New("gh is not authenticated")
		}
		return ok, err
	}
}

// prCheckOptions is a parsed physa pr-check command line.
type prCheckOptions struct {
	rng            string
	pr             int
	json, checkRun bool
	review         bool   // run the reviewer agent
	skipReason     string // why the reviewer was skipped, when --skip-review gave it
}

// parsePRCheck reads the arguments after "pr-check": at most one range and the
// flags --pr, --review, --skip-review, --json and --check-run, in any order.
// It reports false for anything else, including --json together with
// --check-run, --review together with --skip-review, and an empty reason.
func parsePRCheck(args []string) (prCheckOptions, bool) {
	o := prCheckOptions{rng: defaultRange}
	seen, skip := false, false
	value := func(i *int, a, name string) (string, bool) {
		if val, ok := strings.CutPrefix(a, name+"="); ok {
			return val, true
		}
		if *i+1 >= len(args) {
			return "", false
		}
		*i++
		return args[*i], true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			o.json = true
		case a == "--check-run":
			o.checkRun = true
		case a == "--review":
			o.review = true
		case a == "--skip-review" || strings.HasPrefix(a, "--skip-review="):
			val, ok := value(&i, a, "--skip-review")
			if !ok || strings.TrimSpace(val) == "" || skip {
				return o, false
			}
			o.skipReason, skip = val, true
		case a == "--pr" || strings.HasPrefix(a, "--pr="):
			val, ok := value(&i, a, "--pr")
			if !ok {
				return o, false
			}
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return o, false
			}
			o.pr = n
		case strings.HasPrefix(a, "--") || seen:
			return o, false
		default:
			o.rng, seen = a, true
		}
	}
	return o, !(o.json && o.checkRun) && !(o.review && skip)
}

// prCheck runs the PR check on the range of o against the map in dir, with
// the reviewer agent when o.review. It never blocks: 0 whatever it or the
// reviewer finds, and 2 when the map, the history or the range cannot be read
// or the output cannot be written.
func prCheck(dir string, o prCheckOptions, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "physa pr-check: %v\n", err)
		return 2
	}
	m, repo, err := openCheck(dir)
	if err != nil {
		return fail(err)
	}
	c, err := deterministic(dir, m, repo, o.rng, o.pr)
	if err != nil {
		return fail(err)
	}
	switch {
	case o.review:
		title, body, ok := "", "", false
		if o.pr > 0 {
			if t, b, err := newGitHub(dir).PRTitleBody(o.pr); err == nil {
				title, body, ok = t, b, true
			}
		}
		if !ok {
			title, body = c.headMessage()
		}
		_ = c.runReviewer(title, body) // a failed reviewer is in the result
	case o.skipReason != "":
		c.r.Reviewer.Reason = o.skipReason
	}
	switch {
	case o.json:
		err = json.NewEncoder(stdout).Encode(c.r)
	case o.checkRun:
		var out prcheck.Output
		if out, err = prcheck.CheckRunOutput(c.r); err == nil {
			err = json.NewEncoder(stdout).Encode(out)
		}
	default:
		err = prcheck.WriteMarkdown(stdout, c.r)
	}
	if err != nil {
		return fail(err)
	}
	return 0
}
