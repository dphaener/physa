package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/github"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
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
}

// parsePRCheck reads the arguments after "pr-check": at most one range and the
// flags --pr, --json and --check-run, in any order. It reports false for
// anything else, including --json together with --check-run.
func parsePRCheck(args []string) (prCheckOptions, bool) {
	o := prCheckOptions{rng: defaultRange}
	seen := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			o.json = true
		case a == "--check-run":
			o.checkRun = true
		case a == "--pr" || strings.HasPrefix(a, "--pr="):
			val, hasVal := strings.CutPrefix(a, "--pr=")
			if !hasVal {
				if i+1 >= len(args) {
					return o, false
				}
				i++
				val = args[i]
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
	return o, !(o.json && o.checkRun)
}

// prCheck runs the deterministic PR check on the range of o against the map in
// dir. It never blocks: 0 whatever it finds, and 2 when the map, the history
// or the range cannot be read or the output cannot be written.
func prCheck(dir string, o prCheckOptions, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "physa pr-check: %v\n", err)
		return 2
	}
	m, _, err := mapfile.Read(dir)
	if err != nil {
		return fail(err)
	}
	slugs := make(map[string]bool, len(m.Capabilities))
	for _, c := range m.Capabilities {
		slugs[c.Name] = true
	}
	repo, err := history.Open(dir)
	if err != nil {
		return fail(err)
	}
	commits, err := repo.Log(o.rng)
	if err != nil {
		return fail(err)
	}
	findings := history.Check(commits, func(slug string) bool { return slugs[slug] })
	res, err := mapcheck.CheckWith(dir, mapcheck.Options{PRExists: prResolver(dir)})
	if err != nil {
		return fail(err)
	}
	r := prcheck.Result{
		Version:  prcheck.Version,
		PR:       o.pr,
		Range:    o.rng,
		Commits:  commits,
		Findings: findings,
		Map: prcheck.MapResult{
			Problems:  prcheck.Problems(res.Problems),
			PRAnchors: res.PRAnchors,
			Note:      res.PRNote,
		},
	}
	switch {
	case o.json:
		err = json.NewEncoder(stdout).Encode(r)
	case o.checkRun:
		var out prcheck.Output
		if out, err = prcheck.CheckRunOutput(r); err == nil {
			err = json.NewEncoder(stdout).Encode(out)
		}
	default:
		err = prcheck.WriteMarkdown(stdout, r)
	}
	if err != nil {
		return fail(err)
	}
	return 0
}
