package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/forge/github"
	"github.com/dphaener/physa/internal/prcheck"
	"github.com/dphaener/physa/internal/vcs/git"
)

// newForge builds the forge that hosts the repo in dir: GitHub, read through
// gh. Tests replace it so that none of them reaches GitHub.
var newForge = func(dir string) forge.Forge { return github.New(dir) }

// prResolver returns the function physa check map uses to resolve #N anchors.
// A forge that is unavailable or unauthenticated becomes its plain reason
// ("gh is not installed"), so the check falls back to form only.
func prResolver(dir string) func(ctx context.Context, n int) (bool, error) {
	f := newForge(dir)
	return func(ctx context.Context, n int) (bool, error) {
		ok, err := f.PRExists(ctx, n)
		if errors.Is(err, forge.ErrUnavailable) || errors.Is(err, forge.ErrUnauthenticated) {
			return false, errors.New(forge.Reason(err))
		}
		return ok, err
	}
}

// runChanges runs the PR check on the range of o against the map in dir and
// writes its report. It reports false after printing an error that starts
// with name: the map, the history or the range cannot be read, or the output
// cannot be written.
func runChanges(ctx context.Context, dir, name string, o changesOptions, stdout, stderr io.Writer) (prcheck.Result, bool) {
	fail := func(err error) (prcheck.Result, bool) {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return prcheck.Result{}, false
	}
	repo, err := git.Open(ctx, dir)
	if err != nil {
		return fail(err)
	}
	r, err := prcheck.Run(ctx, dir, repo, o.rng, prResolver(dir))
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
func checkChanges(ctx context.Context, dir, name string, o changesOptions, stdout, stderr io.Writer) int {
	r, ok := runChanges(ctx, dir, name, o, stdout, stderr)
	switch {
	case !ok:
		return 2
	case o.strict && !r.Clean():
		return 1
	}
	return 0
}
