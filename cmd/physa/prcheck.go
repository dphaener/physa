package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/github"
	"github.com/dphaener/physa/internal/prcheck"
	"github.com/dphaener/physa/internal/vcs/git"
)

// newGitHub builds the client that reads GitHub through gh. Tests replace it
// so that none of them reaches GitHub.
var newGitHub = func(dir string) *github.Client { return github.New(dir) }

// prResolver returns the function physa check map uses to resolve #N anchors. A
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

// runChanges runs the PR check on the range of o against the map in dir and
// writes its report. It reports false after printing an error that starts
// with name: the map, the history or the range cannot be read, or the output
// cannot be written.
func runChanges(dir, name string, o changesOptions, stdout, stderr io.Writer) (prcheck.Result, bool) {
	fail := func(err error) (prcheck.Result, bool) {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return prcheck.Result{}, false
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
	return r, true
}

// checkChanges is physa check changes. It returns 0 whatever it finds, 1 when
// o.strict and it found something, and 2 when the map, the history or the
// range cannot be read or the output cannot be written.
func checkChanges(dir, name string, o changesOptions, stdout, stderr io.Writer) int {
	r, ok := runChanges(dir, name, o, stdout, stderr)
	switch {
	case !ok:
		return 2
	case o.strict && !r.Clean():
		return 1
	}
	return 0
}
