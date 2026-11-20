package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/github"
	"github.com/dphaener/physa/internal/prcheck"
	"github.com/dphaener/physa/internal/vcs/git"
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
	rng          string
	json, strict bool
}

// parsePRCheck reads the arguments after "pr-check": at most one range and the
// flags --json and --strict, in any order. It reports false for anything else.
func parsePRCheck(args []string) (prCheckOptions, bool) {
	o := prCheckOptions{rng: defaultRange}
	seen := false
	for _, a := range args {
		switch {
		case a == "--json":
			o.json = true
		case a == "--strict":
			o.strict = true
		case strings.HasPrefix(a, "--") || seen:
			return o, false
		default:
			o.rng, seen = a, true
		}
	}
	return o, true
}

// prCheck runs the PR check on the range of o against the map in dir. It
// reports: 0 whatever it finds, 1 when o.strict and it found something, and
// 2 when the map, the history or the range cannot be read or the output
// cannot be written.
func prCheck(dir string, o prCheckOptions, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "physa pr-check: %v\n", err)
		return 2
	}
	repo, err := git.Open(dir)
	if err != nil {
		return fail(err)
	}
	r, err := prcheck.Run(dir, repo, o.rng, prResolver(dir))
	if err != nil {
		return fail(err)
	}
	if o.json {
		err = json.NewEncoder(stdout).Encode(r)
	} else {
		err = prcheck.WriteMarkdown(stdout, r)
	}
	if err != nil {
		return fail(err)
	}
	if o.strict && !r.Clean() {
		return 1
	}
	return 0
}
