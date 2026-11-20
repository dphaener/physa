package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/github"
)

// TestMain keeps every test off GitHub and off any real agent: unless a test
// installs its own fake, GitHub knows no PRs, and the agent refuses to start.
func TestMain(m *testing.M) {
	newGitHub = func(dir string) *github.Client { return fakeGitHub(dir, nil) }
	newAdapter = func() adapter.Adapter { return refusingAdapter{} }
	os.Exit(m.Run())
}

// errNoAgentInTests is what an agent no test installed answers.
var errNoAgentInTests = errors.New("no agent may start in tests")

// refusingAdapter is the agent when a test installs none: it never starts.
type refusingAdapter struct{}

func (refusingAdapter) Name() string { return "no-agent" }
func (refusingAdapter) Start(context.Context, adapter.Spec) (adapter.Session, error) {
	return nil, errNoAgentInTests
}

// fakeAdapter records each session's Spec and ends every session with result
// and waitErr, or refuses to start with startErr.
type fakeAdapter struct {
	specs    []adapter.Spec
	result   adapter.Result
	startErr error
	waitErr  error
}

func (f *fakeAdapter) Name() string { return "fake-agent" }
func (f *fakeAdapter) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	f.specs = append(f.specs, s)
	if f.startErr != nil {
		return nil, f.startErr
	}
	return fakeSession{f}, nil
}

type fakeSession struct{ f *fakeAdapter }

func (s fakeSession) Wait() (adapter.Result, error) { return s.f.result, s.f.waitErr }

// useAdapter installs f as the agent for one test.
func useAdapter(t *testing.T, f *fakeAdapter) {
	t.Helper()
	old := newAdapter
	newAdapter = func() adapter.Adapter { return f }
	t.Cleanup(func() { newAdapter = old })
}

// fakeGitHub returns a client whose gh knows exactly the PRs in known (a
// nil map knows none); any other call fails with a 404.
func fakeGitHub(dir string, known map[int]bool) *github.Client {
	return github.NewWithRunner(dir, func(_ string, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "api" {
			var n int
			if _, err := fmt.Sscanf(args[1], "repos/{owner}/{repo}/pulls/%d", &n); err == nil && known[n] {
				return []byte(strconv.Itoa(n) + "\n"), nil
			}
		}
		return nil, &github.Error{Args: args, Stderr: "gh: Not Found (HTTP 404)", ExitCode: 1}
	})
}

// useGitHub installs a fake GitHub that knows the PRs in known for one test.
func useGitHub(t *testing.T, known ...int) {
	t.Helper()
	set := map[int]bool{}
	for _, n := range known {
		set[n] = true
	}
	old := newGitHub
	newGitHub = func(dir string) *github.Client { return fakeGitHub(dir, set) }
	t.Cleanup(func() { newGitHub = old })
}

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

const trailersUsage = "physa trailers [<range>] [--json] [--strict]"

func TestUsageNamesTrailers(t *testing.T) {
	if !strings.Contains(usage, trailersUsage) {
		t.Errorf("usage = %q, want it to name %q", usage, trailersUsage)
	}
}

// gitIn runs git in dir with the user's git config ignored and returns its output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// trailersRepo is a repo with a small map and four commits on top of a base:
// one sound, one without a trailer, one naming two capabilities and one
// naming a slug the map lacks.
func trailersRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	root := tempMap(t, "r from docs/d.md")
	write(t, root, ".physa/capabilities/b.md", "---\npromise: y\n---\n# B\n\n")
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "base")
	for _, msg := range []string{
		"feat: ok\n\nCapability: a\nLoaded: a, b",
		"feat: bare",
		"feat: two\n\nCapability: a, b",
		"feat: odd\n\nCapability: nosuch",
	} {
		gitIn(t, root, "commit", "-q", "--allow-empty", "-m", msg)
	}
	return root
}

func runTrailers(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"trailers"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTrailersText(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runTrailers(t, "HEAD~4..HEAD")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, errOut)
	}
	for _, want := range []string{
		"feat: odd\n  capability: nosuch \u00b7 loaded: \u2014\n",
		"feat: bare\n  capability: \u2014 \u00b7 loaded: \u2014\n",
		"feat: ok\n  capability: a \u00b7 loaded: a, b\n",
		"\n3 findings:\n",
		": no Capability trailer\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want it to contain %q", out, want)
		}
	}
	if strings.Index(out, "feat: odd") > strings.Index(out, "feat: ok") {
		t.Errorf("stdout = %q, want newest first", out)
	}
}

func TestTrailersOk(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, _ := runTrailers(t, "HEAD~4..HEAD~3", "--strict")
	if code != 0 || !strings.HasSuffix(out, "\nphysa trailers: ok (1 commits)\n") {
		t.Errorf("code = %d, stdout = %q, want 0 and ok", code, out)
	}
}

func TestTrailersJSON(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, _ := runTrailers(t, "--json", "HEAD~4..HEAD")
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("stdout = %q, want one line", out)
	}
	var v struct {
		Range    string
		Commits  []map[string]any
		Findings []map[string]any
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if v.Range != "HEAD~4..HEAD" || len(v.Commits) != 4 || len(v.Findings) != 3 {
		t.Errorf("json = %+v, want the range, 4 commits and 3 findings", v)
	}
	code, out, _ = runTrailers(t, "HEAD~4..HEAD~3", "--json")
	if code != 0 || !strings.Contains(out, `"findings":[]`) || strings.Contains(out, "null") {
		t.Errorf("code = %d, stdout = %q, want empty findings and no null", code, out)
	}
	code, out, _ = runTrailers(t, "HEAD..HEAD", "--json")
	if code != 0 || !strings.Contains(out, `"commits":[]`) {
		t.Errorf("code = %d, stdout = %q, want empty commits", code, out)
	}
}

func TestTrailersStrict(t *testing.T) {
	t.Chdir(trailersRepo(t))
	if code, _, _ := runTrailers(t, "HEAD~4..HEAD", "--strict"); code != 1 {
		t.Errorf("strict with findings: code = %d, want 1", code)
	}
	if code, _, _ := runTrailers(t, "HEAD~4..HEAD"); code != 0 {
		t.Errorf("without strict: code = %d, want 0", code)
	}
}

func TestTrailersDefaultRange(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, _, errOut := runTrailers(t)
	if code != 2 || !strings.HasPrefix(errOut, "physa trailers: ") {
		t.Errorf("code = %d, stderr = %q, want 2 for a missing origin/main", code, errOut)
	}
}

func TestTrailersBadRange(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runTrailers(t, "nosuch..HEAD")
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "physa trailers: ") {
		t.Errorf("code = %d, stdout = %q, stderr = %q, want 2 and a git error", code, out, errOut)
	}
}

func TestTrailersNotARepo(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	t.Chdir(tempMap(t, "r from docs/d.md"))
	code, _, errOut := runTrailers(t, "HEAD~1..HEAD")
	if code != 2 || !strings.Contains(errOut, "not a git repository") {
		t.Errorf("code = %d, stderr = %q, want 2 and not a git repository", code, errOut)
	}
}

func TestTrailersNoMapIsError(t *testing.T) {
	t.Chdir(t.TempDir())
	code, _, errOut := runTrailers(t, "HEAD~1..HEAD")
	if code != 2 || !strings.HasPrefix(errOut, "physa trailers: ") {
		t.Errorf("code = %d, stderr = %q, want 2", code, errOut)
	}
}

func TestTrailersBadUsage(t *testing.T) {
	for _, args := range [][]string{{"a", "b"}, {"--bogus"}, {"--json=1"}} {
		var stdout, stderr bytes.Buffer
		code := run(append([]string{"trailers"}, args...), &stdout, &stderr)
		if code != 2 || stderr.String() != usage || stdout.Len() != 0 {
			t.Errorf("%v: code = %d, stderr = %q, want 2 and usage", args, code, stderr.String())
		}
	}
}
