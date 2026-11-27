package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/prcheck"
)

// prResolver returns the function physa check map uses to resolve #N anchors
// on the forge of d. A forge that is unavailable or unauthenticated becomes
// its plain reason ("gh is not installed"), so the check falls back to form
// only.
func (d deps) prResolver(dir string) func(ctx context.Context, n int) (bool, error) {
	f := d.newForge(dir)
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
func (d deps) runChanges(ctx context.Context, dir, name string, o changesOptions, stdout, stderr io.Writer) (prcheck.Result, bool) {
	fail := func(err error) (prcheck.Result, bool) {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return prcheck.Result{}, false
	}
	repo, err := d.openRepo(ctx, dir)
	if err != nil {
		return fail(err)
	}
	r, err := prcheck.Run(ctx, dir, repo, o.rng, d.prResolver(dir))
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
func (d deps) checkChanges(ctx context.Context, dir, name string, o changesOptions, stdout, stderr io.Writer) int {
	r, ok := d.runChanges(ctx, dir, name, o, stdout, stderr)
	switch {
	case !ok:
		return 2
	case o.strict && !r.Clean():
		return 1
	}
	return 0
}
