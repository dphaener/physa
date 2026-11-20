package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/github"
	"github.com/dphaener/physa/internal/prcheck"
)

func runPRCheck(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"pr-check"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestPRCheckMarkdown(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runPRCheck(t, "HEAD~4..HEAD")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, errOut)
	}
	for _, want := range []string{"feat: ok", "feat: bare", "feat: odd", "nosuch"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown lacks %q:\n%s", want, out)
		}
	}
}

func TestPRCheckJSON(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runPRCheck(t, "HEAD~4..HEAD", "--pr", "11", "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, errOut)
	}
	var r prcheck.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Version != 2 || r.PR != 11 || r.Range != "HEAD~4..HEAD" || len(r.Commits) != 4 || len(r.Findings) == 0 {
		t.Errorf("result = %+v", r)
	}
	if !strings.Contains(out, `"slice":{"touched":null,"misses":null,"excess":null}`) {
		t.Errorf("slice block not null: %s", out)
	}
	if !strings.Contains(out, `"reviewer":{"status":"skipped","reason":"not requested"}`) {
		t.Errorf("reviewer not skipped as not requested: %s", out)
	}
	if r.Map.PRAnchors != prcheck.PRAnchorsGitHub {
		t.Errorf("PRAnchors = %q", r.Map.PRAnchors)
	}
}

func TestPRCheckCheckRun(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runPRCheck(t, "HEAD~4..HEAD", "--check-run", "--pr=7")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, errOut)
	}
	var o prcheck.Output
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatal(err)
	}
	if o.Title == "" || o.Summary == "" || o.Text == "" {
		t.Fatalf("output = %+v", o)
	}
	r, err := prcheck.Decode(o.Text)
	if err != nil || r.PR != 7 {
		t.Errorf("Decode = %+v, %v", r, err)
	}
}

func TestPRCheckMapProblemsStillExitZero(t *testing.T) {
	root := trailersRepo(t)
	write(t, root, ".physa/capabilities/a.md", "---\npromise: x\n---\n# T\n\n## Rules\n\n### R\n`r` · from docs/d.md · #99\nBecause: y.\n")
	t.Chdir(root)
	code, out, errOut := runPRCheck(t, "HEAD~4..HEAD", "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, errOut)
	}
	if !strings.Contains(out, "names no PR") {
		t.Errorf("no PR problem in %s", out)
	}
}

func TestPRCheckNoGHFallsBack(t *testing.T) {
	t.Chdir(trailersRepo(t))
	noGH := func(dir string) *github.Client {
		return github.NewWithRunner(dir, func(string, ...string) ([]byte, error) { return nil, errNoGH })
	}
	old := newGitHub
	newGitHub = noGH
	t.Cleanup(func() { newGitHub = old })
	write(t, ".", ".physa/capabilities/a.md", "---\npromise: x\n---\n# T\n\n## Rules\n\n### R\n`r` · from docs/d.md · #10\nBecause: y.\n")
	code, out, _ := runPRCheck(t, "HEAD~4..HEAD", "--json")
	if code != 0 || !strings.Contains(out, `"pr_anchors":"form-only"`) || !strings.Contains(out, "gh is not installed") {
		t.Errorf("code %d, out %s", code, out)
	}
}

func TestPRCheckUsageErrors(t *testing.T) {
	t.Chdir(trailersRepo(t))
	for _, args := range [][]string{
		{"--json", "--check-run"}, {"--pr", "x"}, {"--pr"}, {"--bogus"}, {"a..b", "c..d"},
		{"--review", "--skip-review", "x"}, {"--skip-review=x", "--review"}, {"--skip-review"},
		{"--skip-review", ""}, {"--skip-review=", "--json"}, {"--skip-review=a", "--skip-review=b"}, {"--review=1"},
	} {
		code, _, errOut := runPRCheck(t, args...)
		if code != 2 || errOut != usage {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut)
		}
	}
}

func TestPRCheckBadRange(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, _, errOut := runPRCheck(t, "not-a-ref..HEAD")
	if code != 2 || !strings.HasPrefix(errOut, "physa pr-check: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func checkWithAnchor(t *testing.T, anchor string) (int, string, string) {
	t.Helper()
	root := tempMap(t, "`r` · from docs/d.md · "+anchor)
	var stdout, stderr bytes.Buffer
	code := check(root, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestCheckResolvesPRAnchors(t *testing.T) {
	useGitHub(t, 10)
	if code, _, errOut := checkWithAnchor(t, "#10"); code != 0 || errOut != "" {
		t.Errorf("known: code %d, stderr %q", code, errOut)
	}
	code, _, errOut := checkWithAnchor(t, "#11")
	if code != 1 || !strings.Contains(errOut, "#11") {
		t.Errorf("unknown: code %d, stderr %q", code, errOut)
	}
}

func TestCheckFormOnlyWithoutGH(t *testing.T) {
	old := newGitHub
	newGitHub = func(dir string) *github.Client {
		return github.NewWithRunner(dir, func(string, ...string) ([]byte, error) { return nil, errNoGH })
	}
	t.Cleanup(func() { newGitHub = old })
	code, _, errOut := checkWithAnchor(t, "#10")
	if code != 0 || !strings.Contains(errOut, "physa check: PR anchors checked for form only: gh is not installed") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

// errNoGH is what the real runner returns when gh is not on PATH.
var errNoGH = &exec.Error{Name: "gh", Err: exec.ErrNotFound}

// prCheckReview runs pr-check with the JSON output on the review repo and
// returns the exit code and the result.
func prCheckReview(t *testing.T, args ...string) (int, prcheck.Result, string) {
	t.Helper()
	code, out, errOut := runPRCheck(t, append([]string{"main..HEAD", "--json"}, args...)...)
	var r prcheck.Result
	if code == 0 {
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	return code, r, errOut
}

func TestPRCheckSkipReview(t *testing.T) {
	t.Chdir(reviewRepo(t))
	f := &fakeAdapter{}
	useAdapter(t, f)
	for _, args := range [][]string{{"--skip-review", "no secret"}, {"--skip-review=no secret"}} {
		code, r, errOut := prCheckReview(t, args...)
		if code != 0 || r.Reviewer.Status != prcheck.ReviewerSkipped || r.Reviewer.Reason != "no secret" || r.Slice.Touched != nil {
			t.Errorf("%v: code %d, reviewer %+v, slice %+v, stderr %q", args, code, r.Reviewer, r.Slice, errOut)
		}
	}
	code, out, _ := runPRCheck(t, "main..HEAD", "--skip-review", "no secret")
	if code != 0 || !strings.Contains(out, "Reviewer: skipped (no secret).") {
		t.Errorf("markdown: code %d, out %s", code, out)
	}
	if len(f.specs) != 0 {
		t.Errorf("the agent started %d times", len(f.specs))
	}
}

func TestPRCheckReviewRan(t *testing.T) {
	root := reviewRepo(t)
	t.Chdir(root)
	f := &fakeAdapter{result: validResult()}
	useAdapter(t, f)
	code, r, errOut := prCheckReview(t, "--review")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	rv := r.Reviewer
	if rv.Status != prcheck.ReviewerRan || rv.Agent != "fake-agent" || rv.Model != "m-1" || rv.Tokens == nil || rv.Tokens.Input != 100 || rv.Review == nil {
		t.Fatalf("reviewer = %+v", rv)
	}
	want := prcheck.SliceResult{Touched: []string{"a"}, Misses: []string{"a"}, Excess: []string{"b"}}
	if !reflect.DeepEqual(r.Slice, want) {
		t.Errorf("slice = %+v, want %+v", r.Slice, want)
	}
	if len(f.specs) != 1 {
		t.Fatalf("specs = %d, want 1", len(f.specs))
	}
	s := f.specs[0]
	wd, _ := os.Getwd()
	if !s.PromptOnly || s.Dir != wd {
		t.Errorf("spec PromptOnly %v, Dir %q, want true and %q", s.PromptOnly, s.Dir, wd)
	}
	for _, part := range []string{"## PR title\n\nfeat: change d", "Why d changes.", "+changed", "File: .physa/capabilities/a.md"} {
		if !strings.Contains(s.Prompt, part) {
			t.Errorf("prompt lacks %q", part)
		}
	}
	if strings.Contains(s.Prompt, "Loaded:") || strings.Contains(s.Prompt, "File: .physa/capabilities/b.md") {
		t.Errorf("prompt carries what the change loaded:\n%s", s.Prompt)
	}
}

func TestPRCheckReviewerFailureExitsZero(t *testing.T) {
	t.Chdir(reviewRepo(t))
	tokens := adapter.Tokens{Input: 7, Output: 1}
	for name, f := range map[string]*fakeAdapter{
		"invalid":       {result: adapter.Result{Text: "no json here", Tokens: tokens}},
		"agent error":   {result: adapter.Result{Tokens: tokens}, waitErr: fmt.Errorf("%w: rate limited", adapter.ErrFailed)},
		"start error":   {startErr: errors.New("cannot start")},
		"not installed": {startErr: adapter.ErrNotInstalled},
	} {
		useAdapter(t, f)
		code, r, errOut := prCheckReview(t, "--review")
		rv := r.Reviewer
		if code != 0 || rv.Status != prcheck.ReviewerFailed || rv.Reason == "" || rv.Review != nil || r.Slice.Touched != nil {
			t.Errorf("%s: code %d, reviewer %+v, slice %+v, stderr %q", name, code, rv, r.Slice, errOut)
			continue
		}
		switch name {
		case "invalid":
			if !strings.Contains(rv.Reason, "invalid") && !strings.Contains(rv.Reason, "json") {
				t.Errorf("invalid: reason %q", rv.Reason)
			}
		case "agent error":
			if !strings.Contains(rv.Reason, "rate limited") || rv.Tokens == nil || *rv.Tokens != tokens {
				t.Errorf("agent error: reviewer %+v", rv)
			}
		case "not installed":
			if rv.Reason != "fake-agent is not installed" {
				t.Errorf("not installed: reason %q", rv.Reason)
			}
		}
	}
}

func TestPRCheckReviewTitleBodyFromGitHub(t *testing.T) {
	t.Chdir(reviewRepo(t))
	f := &fakeAdapter{result: validResult()}
	useAdapter(t, f)
	old := newGitHub
	newGitHub = func(dir string) *github.Client {
		return github.NewWithRunner(dir, func(_ string, args ...string) ([]byte, error) {
			if len(args) == 4 && args[1] == "repos/{owner}/{repo}/pulls/12" && args[3] == "{title, body}" {
				return []byte(`{"title":"PR title from GitHub","body":"PR body from GitHub"}`), nil
			}
			return nil, &github.Error{Args: args, Stderr: "gh: Not Found (HTTP 404)", ExitCode: 1}
		})
	}
	t.Cleanup(func() { newGitHub = old })
	if code, _, errOut := prCheckReview(t, "--review", "--pr", "12"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if p := f.specs[0].Prompt; !strings.Contains(p, "PR title from GitHub") || !strings.Contains(p, "PR body from GitHub") || strings.Contains(p, "Why d changes.") {
		t.Errorf("prompt does not carry GitHub's title and body")
	}
	// A PR GitHub does not know falls back to the head commit.
	if code, _, _ := prCheckReview(t, "--review", "--pr", "13"); code != 0 || !strings.Contains(f.specs[1].Prompt, "Why d changes.") {
		t.Errorf("fallback: code %d", code)
	}
}

func TestPRCheckReviewCheckRun(t *testing.T) {
	t.Chdir(reviewRepo(t))
	useAdapter(t, &fakeAdapter{result: validResult()})
	code, out, errOut := runPRCheck(t, "main..HEAD", "--review", "--check-run")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var o prcheck.Output
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatal(err)
	}
	r, err := prcheck.Decode(o.Text)
	if err != nil || r.Reviewer.Status != prcheck.ReviewerRan || !strings.Contains(o.Title, "1 touched") {
		t.Errorf("title %q, reviewer %+v, err %v", o.Title, r.Reviewer, err)
	}
}
