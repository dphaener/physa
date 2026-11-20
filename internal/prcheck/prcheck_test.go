package prcheck

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

func clean() Result {
	return Result{
		Version: Version, Range: "a..b",
		Commits:  []history.Commit{{Hash: "abc1234567", Short: "abc1234", Subject: "feat: x", Capabilities: []string{"learning"}}},
		Findings: []history.Finding{},
		Map:      MapResult{Problems: []Problem{}, PRAnchors: PRAnchorsGitHub},
	}
}

func messy() Result {
	r := clean()
	r.Commits = append(r.Commits, history.Commit{Hash: "def", Short: "def5678", Subject: "fix: y", Capabilities: []string{}})
	r.Findings = []history.Finding{{Commit: "def5678", Kind: history.KindMissing, Message: "no Capability trailer"}}
	r.Map = MapResult{Problems: []Problem{{Path: ".physa/product.md", Line: 4, Message: "bad anchor"}, {Path: "x.md", Message: "whole file"}}, PRAnchors: PRAnchorsFormOnly, Note: "gh is not installed"}
	return r
}

func TestJSONShape(t *testing.T) {
	data, err := json.Marshal(Result{Version: Version, Range: "a..b"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["version"] != float64(3) {
		t.Errorf("version = %v", m["version"])
	}
	for _, k := range []string{"pr", "slice", "reviewer"} {
		if _, ok := m[k]; ok {
			t.Errorf("key %q should be gone: %s", k, data)
		}
	}
	if c, ok := m["commits"].([]any); !ok || len(c) != 0 {
		t.Errorf("commits = %v", m["commits"])
	}
	data, err = json.Marshal(Result{Version: Version, Commits: []history.Commit{}})
	if err != nil || !strings.Contains(string(data), `"commits":[]`) {
		t.Errorf("empty non-nil commits: %s, %v", data, err)
	}
	if f, ok := m["findings"].([]any); !ok || len(f) != 0 {
		t.Errorf("findings = %v", m["findings"])
	}
	if p, ok := m["map"].(map[string]any)["problems"].([]any); !ok || len(p) != 0 {
		t.Errorf("problems = %v", m["map"])
	}
}

func TestTitle(t *testing.T) {
	cases := []struct {
		r    Result
		want string
	}{
		{clean(), "1 commit · 0 trailer findings · no map problems"},
		{messy(), "2 commits · 1 trailer finding · 2 map problems"},
		{Result{Map: MapResult{Problems: []Problem{{}}}}, "0 commits · 0 trailer findings · 1 map problem"},
	}
	for _, c := range cases {
		if got := c.r.Title(); got != c.want {
			t.Errorf("Title() = %q, want %q", got, c.want)
		}
	}
}

func md(t *testing.T, r Result) string {
	t.Helper()
	var b strings.Builder
	if err := WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestMarkdownClean(t *testing.T) {
	got := md(t, clean())
	for _, want := range []string{"## 1 commit", "| Commit | Subject | Capability |\n", "| abc1234 | feat: x | learning |", "No trailer findings.", "The map passes physa check map.", "checked against GitHub"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownMessy(t *testing.T) {
	got := md(t, messy())
	for _, want := range []string{"| def5678 | fix: y | - |", "- def5678: no Capability trailer", "- .physa/product.md:4: bad anchor", "- x.md: whole file", "checked for form only (gh is not installed)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestClean(t *testing.T) {
	if !clean().Clean() {
		t.Error("clean() should be clean")
	}
	r := clean()
	r.Findings = messy().Findings
	if r.Clean() {
		t.Error("a trailer finding is not clean")
	}
	r = clean()
	r.Map.Problems = messy().Map.Problems
	if r.Clean() {
		t.Error("a map problem is not clean")
	}
	r = clean()
	r.Map.PRAnchors, r.Map.Note = PRAnchorsFormOnly, "gh is not installed"
	if !r.Clean() {
		t.Error("PR anchors checked for form only is still clean")
	}
}

const front = "---\npromise: x\n---\n# T\n\n"

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// mapRepo makes a repo whose first commit holds a sound map with capability a.
func mapRepo(t *testing.T) (vcs.Repo, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	write(t, root, ".physa/product.md", front)
	write(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: a thing.\n")
	write(t, root, ".physa/README.md", "hi\n")
	write(t, root, ".physa/capabilities/a.md", front)
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "base")
	repo, err := git.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo, root
}

func TestRunClean(t *testing.T) {
	repo, root := mapRepo(t)
	gitIn(t, root, "commit", "-q", "--allow-empty", "-m", "feat: a\n\nCapability: a")
	write(t, root, ".physa/capabilities/new.md", front)
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "feat: new\n\nCapability: new")
	r, err := Run(root, repo, "HEAD~2..HEAD", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Clean() || len(r.Commits) != 2 || r.Version != Version || r.Range != "HEAD~2..HEAD" {
		t.Errorf("want a clean result of 2 commits, got %+v", r)
	}
	if r.Map.PRAnchors != PRAnchorsFormOnly {
		t.Errorf("PRAnchors = %q, want form-only without a resolver", r.Map.PRAnchors)
	}
}

func TestRunFindings(t *testing.T) {
	repo, root := mapRepo(t)
	gitIn(t, root, "commit", "-q", "--allow-empty", "-m", "feat: early\n\nCapability: later")
	write(t, root, ".physa/capabilities/later.md", front)
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "feat: later\n\nCapability: later")
	full := gitIn(t, root, "rev-parse", "HEAD")
	write(t, root, ".physa/capabilities/a.md", front+"## Rules\n\n### R\n`r` · from docs/nope.md · "+full[:7]+"\nBecause: y.\n")
	r, err := Run(root, repo, "HEAD~2..HEAD", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Clean() || len(r.Findings) != 1 || r.Findings[0].Message != `Capability "later" is not in the map` {
		t.Errorf("findings = %+v", r.Findings)
	}
	if len(r.Map.Problems) != 1 || !strings.Contains(r.Map.Problems[0].Message, "docs/nope.md") {
		t.Errorf("want only the doc anchor problem (the commit anchor resolves), got %+v", r.Map.Problems)
	}
}

func TestRunErrors(t *testing.T) {
	repo, root := mapRepo(t)
	if _, err := Run(root, repo, "nope..HEAD", nil); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("bad range: want git's message, got %v", err)
	}
	if _, err := Run(t.TempDir(), repo, "HEAD", nil); err == nil {
		t.Error("no map: want an error")
	}
	prErr := func(int) (bool, error) { return false, errors.New("gh is not authenticated") }
	write(t, root, ".physa/capabilities/a.md", front+"## Rules\n\n### R\n`r` · from .physa/README.md · #3\nBecause: y.\n")
	r, err := Run(root, repo, "HEAD..HEAD", prErr)
	if err != nil || r.Map.Note != "gh is not authenticated" || !r.Clean() {
		t.Errorf("resolver error: got %+v %v", r, err)
	}
}
