package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/adapter/claude"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/prcheck"
	"github.com/dphaener/physa/internal/review"
)

// newAdapter builds the adapter of the agent the reviewer runs on. Tests
// replace it so that none of them starts a real agent.
var newAdapter = func() adapter.Adapter { return claude.New() }

// reviewTimeout bounds one reviewer session.
const reviewTimeout = 15 * time.Minute

// checked is a range after the deterministic check: the map and the repo it
// read, and the result so far, with the reviewer skipped.
type checked struct {
	dir  string
	m    *mapfile.Map
	repo *history.Repo
	r    prcheck.Result
}

// openCheck reads the map in dir and opens its repo.
func openCheck(dir string) (*mapfile.Map, *history.Repo, error) {
	m, _, err := mapfile.Read(dir)
	if err != nil {
		return nil, nil, err
	}
	repo, err := history.Open(dir)
	if err != nil {
		return nil, nil, err
	}
	return m, repo, nil
}

// deterministic runs the part of the PR check that needs no agent on rng:
// the commits' trailers against the map, and the map's own check. The
// reviewer is recorded as skipped because it was not requested.
func deterministic(dir string, m *mapfile.Map, repo *history.Repo, rng string, pr int) (checked, error) {
	slugs := make(map[string]bool, len(m.Capabilities))
	for _, c := range m.Capabilities {
		slugs[c.Name] = true
	}
	commits, err := repo.Log(rng)
	if err != nil {
		return checked{}, err
	}
	findings := history.Check(commits, func(slug string) bool { return slugs[slug] })
	res, err := mapcheck.CheckWith(dir, mapcheck.Options{PRExists: prResolver(dir)})
	if err != nil {
		return checked{}, err
	}
	return checked{dir: dir, m: m, repo: repo, r: prcheck.Result{
		Version:  prcheck.Version,
		PR:       pr,
		Range:    rng,
		Commits:  commits,
		Findings: findings,
		Map: prcheck.MapResult{
			Problems:  prcheck.Problems(res.Problems),
			PRAnchors: res.PRAnchors,
			Note:      res.PRNote,
		},
		Reviewer: prcheck.ReviewerResult{Status: prcheck.ReviewerSkipped, Reason: "not requested"},
	}}, nil
}

// headMessage is the subject and body of the newest commit of the range, or
// empty when the range has none.
func (c *checked) headMessage() (title, body string) {
	if len(c.r.Commits) == 0 {
		return "", ""
	}
	title, body, err := c.repo.Message(c.r.Commits[0].Hash)
	if err != nil {
		return c.r.Commits[0].Subject, ""
	}
	return title, body
}

// runReviewer runs the reviewer agent on the change with the given PR title
// and body and records what became of it in c.r: ran, with the review and the
// misses and excess it gives, or failed, with the reason. The error is why it
// failed; it wraps adapter.ErrNotInstalled when the agent is missing.
func (c *checked) runReviewer(title, body string) error {
	a := newAdapter()
	rv := prcheck.ReviewerResult{Status: prcheck.ReviewerFailed, Agent: a.Name()}
	fail := func(err error) error {
		rv.Reason = err.Error()
		c.r.Reviewer = rv
		return err
	}
	diff, err := c.repo.Diff(c.r.Range)
	if err != nil {
		return fail(fmt.Errorf("reading the diff: %w", err))
	}
	var names []string
	for _, cm := range c.r.Commits {
		names = append(names, cm.Capabilities...)
	}
	prompt, err := review.Prompt(c.m, review.Input{Title: title, Body: body, Diff: diff, Capabilities: names})
	if err != nil {
		return fail(err)
	}
	dir, err := filepath.Abs(c.dir)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
	defer cancel()
	s, err := a.Start(ctx, adapter.Spec{Prompt: prompt, Dir: dir, PromptOnly: true})
	if errors.Is(err, adapter.ErrNotInstalled) {
		return fail(notInstalled(a.Name()))
	} else if err != nil {
		return fail(err)
	}
	res, err := s.Wait()
	rv.Model = res.Model
	if res.Tokens != (adapter.Tokens{}) {
		t := res.Tokens
		rv.Tokens = &t
	}
	if err != nil {
		return fail(err)
	}
	out, err := review.Parse(res.Text, c.m)
	if err != nil {
		return fail(err)
	}
	rv.Status, rv.Review = prcheck.ReviewerRan, &out
	c.r.Reviewer = rv
	c.r.Slice = prcheck.Measure(c.r.Commits, out.Touched)
	return nil
}

// notInstalled is the reason when the agent's command is missing. It is
// adapter.ErrNotInstalled under errors.Is.
type notInstalled string

func (n notInstalled) Error() string { return string(n) + " is not installed" }
func (n notInstalled) Unwrap() error { return adapter.ErrNotInstalled }
