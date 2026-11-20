package main

import (
	"bytes"
	"encoding/json"
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

const sliceUsage = "physa slice <capability>... [--with decisions|questions|all] [--budget <tokens>] [--format md|json]"

func TestUsageNamesSlice(t *testing.T) {
	if !strings.Contains(usage, sliceUsage) {
		t.Errorf("usage = %q, want it to name %q", usage, sliceUsage)
	}
}

func TestSliceGoodFixture(t *testing.T) {
	t.Chdir("../../internal/mapfile/testdata/good")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"slice", "delivery"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "# Slice: delivery\n") {
		t.Errorf("stdout = %q, want a slice header", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Capability: delivery") {
		t.Errorf("stdout = %q, want the anchor trailer", stdout.String())
	}
}

func TestSliceOwnMap(t *testing.T) {
	t.Chdir("../..")
	var stdout, stderr bytes.Buffer
	code := run([]string{"slice", "slices", "map"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "# Slice: map, slices\n") {
		t.Errorf("stdout = %q, want a slice of map and slices", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestSliceJSON(t *testing.T) {
	t.Chdir("../..")
	var stdout, stderr bytes.Buffer
	code := run([]string{"slice", "map", "--format", "json", "--with", "all"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if v["capabilities"] == nil {
		t.Errorf("json = %v, want capabilities", v)
	}
}

func TestSliceFlagForms(t *testing.T) {
	t.Chdir("../..")
	var want bytes.Buffer
	if code := run([]string{"slice", "slices", "map", "--format", "json", "--budget", "3000", "--with", "all"}, &want, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	for _, args := range [][]string{
		{"--format=json", "--budget=3000", "--with=all", "slices", "map"},
		{"slices", "--format", "json", "map", "--with", "all", "--budget=3000"},
		{"map", "slices", "map", "--with=all", "--format=json", "--budget", "3000"},
	} {
		var got, stderr bytes.Buffer
		if code := run(append([]string{"slice"}, args...), &got, &stderr); code != 0 {
			t.Fatalf("run(%v) = %d; stderr %q", args, code, stderr.String())
		}
		if got.String() != want.String() {
			t.Errorf("run(%v) differs from the reference output", args)
		}
	}
}

func TestSliceWithKinds(t *testing.T) {
	t.Chdir("../..")
	for _, w := range []string{"decisions", "questions", "all"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"slice", "slices", "--with", w}, &stdout, &stderr); code != 0 {
			t.Errorf("--with %s: code = %d; stderr %q", w, code, stderr.String())
		}
	}
}

func TestSliceBadUsage(t *testing.T) {
	t.Chdir("../..")
	for _, args := range [][]string{
		{"slice"},
		{"slice", "--format", "json"},
		{"slice", "slices", "--budget=0"},
		{"slice", "slices", "--budget", "-5"},
		{"slice", "slices", "--budget", "many"},
		{"slice", "slices", "--budget"},
		{"slice", "slices", "--with", "bad"},
		{"slice", "slices", "--format", "bad"},
		{"slice", "slices", "--change", "x"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), sliceUsage) {
			t.Errorf("run(%v) stderr = %q, want the usage", args, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%v) stdout = %q, want empty", args, stdout.String())
		}
	}
}

func TestSliceUnknownCapability(t *testing.T) {
	t.Chdir("../..")
	var stdout, stderr bytes.Buffer
	code := run([]string{"slice", "nope"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), "physa slice: unknown capability \"nope\"; run physa index to see them\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestSliceNoMapIsError(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"slice", "slices"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.HasPrefix(stderr.String(), "physa slice: ") {
		t.Errorf("stderr = %q, want a physa slice error", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestSliceMapWithProblem(t *testing.T) {
	t.Chdir(tempMap(t, "r from docs/d.md"))
	var stdout, stderr bytes.Buffer
	code := run([]string{"slice", "a"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "# Slice: a\n") {
		t.Errorf("stdout = %q, want the slice still printed", stdout.String())
	}
	if got, want := stderr.String(), "physa slice: the map has 1 problem; run physa check\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestSliceOverBudget(t *testing.T) {
	t.Chdir("../..")
	var stdout, stderr bytes.Buffer
	code := run([]string{"slice", "slices", "--budget", "10"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Over budget:") {
		t.Errorf("stdout lacks the over-budget line")
	}
	if got, want := stderr.String(), "physa slice: over budget: what is never cut is "; !strings.HasPrefix(got, want) || !strings.HasSuffix(got, " tokens, over the 10-token budget\n") {
		t.Errorf("stderr = %q, want %q... tokens, over the 10-token budget", got, want)
	}
}
