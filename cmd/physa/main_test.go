package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestRunVersionForms(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-version"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{arg}, &stdout, &stderr)
		if code != 0 {
			t.Errorf("run(%q) = %d, want 0", arg, code)
		}
		if got, want := stdout.String(), "physa dev\n"; got != want {
			t.Errorf("run(%q) stdout = %q, want %q", arg, got, want)
		}
		if stderr.Len() != 0 {
			t.Errorf("run(%q) stderr = %q, want empty", arg, stderr.String())
		}
	}
}

func TestRunNoArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("run(nil) = %d, want 2", code)
	}
	if got, want := stderr.String(), usage; got != want {
		t.Errorf("run(nil) stderr = %q, want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("run(nil) stdout = %q, want empty", stdout.String())
	}
}

func TestRunUnknownArgPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("run(bogus) = %d, want 2", code)
	}
	if got, want := stderr.String(), usage; got != want {
		t.Errorf("run(bogus) stderr = %q, want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("run(bogus) stdout = %q, want empty", stdout.String())
	}
}

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

// tempMap builds a repo root with a small map; rule is the capability's rule entry.
func tempMap(t *testing.T, rule string) string {
	t.Helper()
	root := t.TempDir()
	front := "---\npromise: x\n---\n# T\n\n"
	write(t, root, ".physa/product.md", front)
	write(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: a thing.\n")
	write(t, root, ".physa/README.md", "hi\n")
	write(t, root, ".physa/capabilities/a.md", front+"## Rules\n\n### R\n"+rule+"\nBecause: y.\n")
	write(t, root, "docs/d.md", "doc\n")
	return root
}

func TestCheckGoodMap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", tempMap(t, "`r` · from docs/d.md")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	if got, want := stdout.String(), "physa check: ok (1 capabilities, 1 entries)\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestCheckBrokenMap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", tempMap(t, "r from docs/d.md")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), ".physa/capabilities/a.md:") {
		t.Errorf("stderr = %q, want a path:line problem", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestCheckNoMapIsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", t.TempDir()}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.HasPrefix(stderr.String(), "physa check: ") {
		t.Errorf("stderr = %q, want a physa check error", stderr.String())
	}
}

func TestCheckTooManyArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "a", "b"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), usage; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestIndexGoodMap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"index", tempMap(t, "`r` · from docs/d.md")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	if got, want := stdout.String(), "a  x  1 rule\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestIndexMapWithProblem(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"index", tempMap(t, "r from docs/d.md")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.HasPrefix(stdout.String(), "a  x") {
		t.Errorf("stdout = %q, want the index still printed", stdout.String())
	}
	if got, want := stderr.String(), "physa index: the map has 1 problem; run physa check\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestIndexNoMapIsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"index", t.TempDir()}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.HasPrefix(stderr.String(), "physa index: ") {
		t.Errorf("stderr = %q, want a physa index error", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestIndexTooManyArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"index", "a", "b"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), usage; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestIndexOwnMap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"index", "../.."}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 13 {
		t.Fatalf("got %d lines, want 13:\n%s", len(lines), stdout.String())
	}
	var names []string
	for _, l := range lines {
		names = append(names, strings.Fields(l)[0])
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("names not sorted: %v", names)
	}
}
