package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/forge/github"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/prcheck"
)

func runPRCheck(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"check", "changes"}, args...), &stdout, &stderr)
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
	code, out, errOut := runPRCheck(t, "HEAD~4..HEAD", "--json")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, errOut)
	}
	var r prcheck.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Version != 3 || r.Range != "HEAD~4..HEAD" || len(r.Commits) != 4 || len(r.Findings) == 0 {
		t.Errorf("result = %+v", r)
	}
	for _, k := range []string{`"reviewer"`, `"slice"`, `"pr"`} {
		if strings.Contains(out, k) {
			t.Errorf("output still has %s: %s", k, out)
		}
	}
	if r.Map.PRAnchors != prcheck.PRAnchorsGitHub {
		t.Errorf("PRAnchors = %q", r.Map.PRAnchors)
	}
}

func TestPRCheckMapProblemsStillExitZero(t *testing.T) {
	root := trailersRepo(t)
	folder(t, root, mapfolder.CapabilityDir("a"), "---\npromise: x\n---\n# T\n\n## Rules\n\n### R\n`r` · from docs/d.md · #99\nBecause: y.\n")
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
	useNoGH(t)
	folder(t, ".", mapfolder.CapabilityDir("a"), "---\npromise: x\n---\n# T\n\n## Rules\n\n### R\n`r` · from docs/d.md · #10\nBecause: y.\n")
	code, out, _ := runPRCheck(t, "HEAD~4..HEAD", "--json")
	if code != 0 || !strings.Contains(out, `"pr_anchors":"form-only"`) || !strings.Contains(out, "gh is not installed") {
		t.Errorf("code %d, out %s", code, out)
	}
}

func TestCheckHelpNamesChanges(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if want := "physa check changes [<range>] [--json] [--strict]"; !strings.Contains(stdout.String(), want) {
		t.Errorf("help = %q, want it to name %q", stdout.String(), want)
	}
}

func TestPRCheckStrict(t *testing.T) {
	t.Chdir(trailersRepo(t))
	if code, out, errOut := runPRCheck(t, "HEAD~4..HEAD", "--strict"); code != 1 || !strings.Contains(out, "nosuch") {
		t.Errorf("strict with findings: code %d, stdout %q, stderr %q, want 1", code, out, errOut)
	}
	folder(t, ".", mapfolder.CapabilityDir("a"), "---\npromise: x\n---\n# T\n\n## Rules\n\n### R\n`r` · from docs/d.md\nBecause: y.\n")
	code, out, errOut := runPRCheck(t, "--strict", "HEAD~4..HEAD~3", "--json")
	if code != 0 {
		t.Errorf("strict and clean: code %d, stdout %q, stderr %q, want 0", code, out, errOut)
	}
	folder(t, ".", mapfolder.CapabilityDir("a"), "---\npromise: x\n---\n# T\n\n## Rules\n\n### R\n`r` · from docs/nope.md\nBecause: y.\n")
	if code, out, _ := runPRCheck(t, "HEAD~4..HEAD~3", "--strict"); code != 1 || !strings.Contains(out, "docs/nope.md") {
		t.Errorf("strict with a map problem: code %d, stdout %q, want 1", code, out)
	}
}

// A change that adds a capability may name it; a change naming a slug that
// only a later change adds may not.
func TestPRCheckMapAsEachChangeLeavesIt(t *testing.T) {
	root := trailersRepo(t)
	gitIn(t, root, "commit", "-q", "--allow-empty", "-m", "feat: early\n\nCapability: c")
	folder(t, root, mapfolder.CapabilityDir("c"), "---\npromise: z\n---\n# C\n\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "feat: add c\n\nCapability: c")
	t.Chdir(root)
	code, out, errOut := runPRCheck(t, "HEAD~2..HEAD", "--json")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var r prcheck.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Findings) != 1 || r.Findings[0].Slug != "c" || r.Findings[0].Commit != r.Commits[1].Short {
		t.Errorf("want one unknown c on the early commit, got %+v", r.Findings)
	}
}

func TestPRCheckNotARepo(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	t.Chdir(tempMap(t, "r from docs/d.md"))
	code, _, errOut := runPRCheck(t, "HEAD~1..HEAD")
	if code != 2 || errOut != "physa check changes: not a repository under version control\n" {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestPRCheckUsageErrors(t *testing.T) {
	t.Chdir(trailersRepo(t))
	for _, args := range [][]string{
		{"--check-run"}, {"--pr", "7"}, {"--review"}, {"--skip-review", "x"}, {"--bogus"}, {"a..b", "c..d"}, {"--json=1"},
	} {
		code, _, errOut := runPRCheck(t, args...)
		if code != 2 || !strings.HasPrefix(errOut, "usage: physa check changes") {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut)
		}
	}
}

func TestPRCheckBadRange(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, _, errOut := runPRCheck(t, "not-a-ref..HEAD")
	if code != 2 || !strings.HasPrefix(errOut, "physa check changes: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func checkWithAnchor(t *testing.T, anchor string) (int, string, string) {
	t.Helper()
	root := tempMap(t, "`r` · from docs/d.md · "+anchor)
	var stdout, stderr bytes.Buffer
	code := checkMap(root, "physa check map", &stdout, &stderr)
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
	useNoGH(t)
	code, _, errOut := checkWithAnchor(t, "#10")
	if code != 0 || !strings.Contains(errOut, "physa check map: PR anchors checked for form only: gh is not installed") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

// errNoGH is what the real runner returns when gh is not on PATH.
var errNoGH = &exec.Error{Name: "gh", Err: exec.ErrNotFound}

// useNoGH installs, for one test, a GitHub whose gh is not installed.
func useNoGH(t *testing.T) {
	t.Helper()
	old := newForge
	newForge = func(dir string) forge.Forge {
		return github.NewWithRunner(dir, func(string, ...string) ([]byte, error) { return nil, errNoGH })
	}
	t.Cleanup(func() { newForge = old })
}

func TestCheckFormOnlyUnauthenticated(t *testing.T) {
	old := newForge
	newForge = func(dir string) forge.Forge {
		return github.NewWithRunner(dir, func(_ string, args ...string) ([]byte, error) {
			return nil, &github.Error{Args: args, Stderr: "HTTP 401: Bad credentials", ExitCode: 1}
		})
	}
	t.Cleanup(func() { newForge = old })
	code, _, errOut := checkWithAnchor(t, "#10")
	if code != 0 || !strings.Contains(errOut, "physa check map: PR anchors checked for form only: gh is not authenticated\n") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func runCheck(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"check"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestBareCheckWithoutRepositoryChecksTheMap(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	t.Chdir(tempMap(t, "`r` · from docs/d.md"))
	code, out, errOut := runCheck(t)
	if code != 0 || out != "physa check: ok (1 capability, 1 entry)\n" || errOut != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestBareCheckWithoutOriginMainChecksTheMap(t *testing.T) {
	t.Chdir(trailersRepo(t))
	folder(t, ".", mapfolder.CapabilityDir("a"), "---\npromise: x\n---\n# T\n\n## Rules\n\n### R\n`r` \u00b7 from docs/d.md\nBecause: y.\n")
	code, out, _ := runCheck(t)
	if code != 0 || !strings.HasPrefix(out, "physa check: ok") {
		t.Errorf("code %d, stdout %q", code, out)
	}
}

func TestBareCheckRangeRunsTheChangeCheck(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runCheck(t, "HEAD~4..HEAD")
	if code != 1 {
		t.Fatalf("a map with problems: code %d, want 1; stderr %q", code, errOut)
	}
	for _, want := range []string{"feat: ok", "feat: odd", "nosuch"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown lacks %q:\n%s", want, out)
		}
	}
	folder(t, ".", mapfolder.CapabilityDir("a"), "---\npromise: x\n---\n# T\n\n## Rules\n\n### R\n`r` \u00b7 from docs/d.md\nBecause: y.\n")
	if code, _, errOut := runCheck(t, "HEAD~4..HEAD"); code != 0 {
		t.Errorf("sound map with findings: code %d, want 0; stderr %q", code, errOut)
	}
	if code, _, _ := runCheck(t, "HEAD~4..HEAD", "--strict"); code != 1 {
		t.Errorf("strict with findings: code %d, want 1", code)
	}
}

func TestBareCheckBadRangeAndUsage(t *testing.T) {
	t.Chdir(trailersRepo(t))
	if code, _, errOut := runCheck(t, "not-a-ref..HEAD"); code != 2 || !strings.HasPrefix(errOut, "physa check: ") {
		t.Errorf("bad range: code %d, stderr %q", code, errOut)
	}
	for _, args := range [][]string{{"--json"}, {"a..b", "c..d"}, {"--bogus"}} {
		if code, _, errOut := runCheck(t, args...); code != 2 || !strings.HasPrefix(errOut, "usage: physa check") {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut)
		}
	}
}

func TestDefaultFakeForgeKnowsNoPR(t *testing.T) {
	f := newForge(t.TempDir())
	if ok, err := f.PRExists(1); ok || err != nil {
		t.Errorf("PRExists: %v, %v", ok, err)
	}
	if _, ok, err := f.ChangePR("c0ffee"); ok || err != nil {
		t.Errorf("ChangePR: %v, %v", ok, err)
	}
}
