package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

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
	if r.Version != 1 || r.PR != 11 || r.Range != "HEAD~4..HEAD" || len(r.Commits) != 4 || len(r.Findings) == 0 {
		t.Errorf("result = %+v", r)
	}
	if !strings.Contains(out, `"slice":{"touched":null,"misses":null,"excess":null}`) {
		t.Errorf("slice block not null: %s", out)
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
	for _, args := range [][]string{{"--json", "--check-run"}, {"--pr", "x"}, {"--pr"}, {"--bogus"}, {"a..b", "c..d"}} {
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
