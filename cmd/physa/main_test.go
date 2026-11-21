package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/forge/github"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/store"
)

// TestMain keeps every test off GitHub: unless a test installs its own fake,
// GitHub knows no PRs and no change came from one.
func TestMain(m *testing.M) {
	newForge = func(dir string) forge.Forge { return fakeGitHub(dir, nil) }
	// No test starts a real agent: an unexpected start stops the binary.
	agents["claude"] = func() adapter.Adapter { return unexpectedAgent{} }
	// Safety net: no test reaches the store of this repository.
	dir, err := os.MkdirTemp("", "physa-test-store")
	if err != nil {
		panic(err)
	}
	openStore = func(string) (*store.Store, error) { return store.Open(dir) }
	// Nor the real ~/.claude, nor the running session's id.
	claudeDir, err := os.MkdirTemp("", "physa-test-claude")
	if err != nil {
		panic(err)
	}
	os.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	os.Setenv("CLAUDE_CODE_SESSION_ID", "")
	// Nor the real person setting.
	configDir, err := os.MkdirTemp("", "physa-test-config")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", configDir)
	// A test that learns has a person by default; the ones about naming one
	// set their own config dir.
	if err := os.MkdirAll(filepath.Join(configDir, "physa"), 0o700); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "physa", "person"), []byte("tester\n"), 0o600); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.RemoveAll(claudeDir)
	os.RemoveAll(configDir)
	os.Exit(code)
}

// fakeGitHub returns a client whose gh knows exactly the PRs in known (a
// nil map knows none) and no PR for any change; any other call fails with a
// 404.
func fakeGitHub(dir string, known map[int]bool) *github.Client {
	return github.NewWithRunner(dir, func(_ string, args ...string) ([]byte, error) {
		switch {
		case len(args) >= 2 && args[0] == "repo" && args[1] == "view":
			return []byte(`{"name":"r","owner":{"login":"o"}}`), nil
		case len(args) >= 2 && args[0] == "api" && args[1] == "graphql":
			return []byte(`{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[]}}}}}`), nil
		case len(args) >= 2 && args[0] == "api":
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
	old := newForge
	newForge = func(dir string) forge.Forge { return fakeGitHub(dir, set) }
	t.Cleanup(func() { newForge = old })
}

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if got, want := stdout.String(), "physa dev\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// rootHelp is the root help text.
func rootHelp() string {
	var b bytes.Buffer
	cli.Help(rootCommand(), &b)
	return b.String()
}

func TestRootHelpListsTheCommands(t *testing.T) {
	help := rootHelp()
	for _, name := range []string{"check ", "map ", "sync ", "version ", "help "} {
		if !strings.Contains(help, "\n  "+name) {
			t.Errorf("root help lacks %q:\n%s", name, help)
		}
	}
}

func TestHelpPrintsRootHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
		if stdout.String() != rootHelp() || stderr.Len() != 0 {
			t.Errorf("run(%v) stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestHelpOnGroupsAndLeaves(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--help"}, {"check", "map", "--help"}, {"check", "changes", "--help"},
		{"map", "--help"}, {"map", "index", "--help"}, {"map", "slice", "--help"},
		{"map", "changes", "--help"}, {"map", "records", "--help"},
		{"version", "--help"}, {"sync", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
		if !strings.HasPrefix(stdout.String(), "usage: physa "+args[0]) || stderr.Len() != 0 {
			t.Errorf("run(%v) stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestNoArgsAndUnknownPrintRootHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if stderr.String() != rootHelp() || stdout.Len() != 0 {
			t.Errorf("run(%v) stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
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
// folder writes the single text of a capability (or the product) as a folder
// at dir under root, as init does.
func folder(t *testing.T, root, dir, text string) {
	t.Helper()
	f, err := mapfolder.Split(text)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range f.Files {
		write(t, root, dir+"/"+name, body)
	}
}

func tempMap(t *testing.T, rule string) string {
	t.Helper()
	root := t.TempDir()
	front := "---\npromise: x\n---\n# T\n\n"
	folder(t, root, mapfolder.ProductDir, front)
	write(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: a thing.\n")
	write(t, root, ".physa/README.md", "hi\n")
	folder(t, root, mapfolder.CapabilityDir("a"), front+"## Rules\n\n### R\n"+rule+"\nBecause: y.\n")
	write(t, root, "docs/d.md", "doc\n")
	return root
}

func TestCheckGoodMap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "map", tempMap(t, "`r` · from docs/d.md")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	if got, want := stdout.String(), "physa check map: ok (1 capability, 1 entry)\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestCheckBrokenMap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "map", tempMap(t, "r from docs/d.md")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), ".physa/capabilities/a/rules.md:") {
		t.Errorf("stderr = %q, want a path:line problem", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestCheckNoMapIsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "map", t.TempDir()}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.HasPrefix(stderr.String(), "physa check map: ") {
		t.Errorf("stderr = %q, want a physa check map error", stderr.String())
	}
}

func TestCheckMapTooManyArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "map", "a", "b"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), helpOf(t, "check", "map"); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestIndexGoodMap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "index", tempMap(t, "`r` · from docs/d.md")}, &stdout, &stderr)
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
	code := run([]string{"map", "index", tempMap(t, "r from docs/d.md")}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.HasPrefix(stdout.String(), "a  x") {
		t.Errorf("stdout = %q, want the index still printed", stdout.String())
	}
	if got, want := stderr.String(), "physa map index: the map has 1 problem; run physa check map\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestIndexNoMapIsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "index", t.TempDir()}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.HasPrefix(stderr.String(), "physa map index: ") {
		t.Errorf("stderr = %q, want a physa map index error", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestIndexTooManyArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "index", "a", "b"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), helpOf(t, "map", "index"); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestBareMapEqualsMapIndex(t *testing.T) {
	t.Chdir("../..")
	var a, b, errA, errB bytes.Buffer
	codeA := run([]string{"map"}, &a, &errA)
	codeB := run([]string{"map", "index"}, &b, &errB)
	if codeA != 0 || codeB != 0 || a.Len() == 0 || a.String() != b.String() || errA.String() != errB.String() {
		t.Errorf("map: %d %q %q; map index: %d %q %q", codeA, a.String(), errA.String(), codeB, b.String(), errB.String())
	}
}

func TestMapBadArgumentPrintsMapHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"map", "nonsense"}, &stdout, &stderr); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if stderr.String() != helpOf(t, "map") || stdout.Len() != 0 {
		t.Errorf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}

func TestIndexOwnMap(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "index", "../.."}, &stdout, &stderr)
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

const (
	sliceUsage      = "physa map slice <capability>... [--with decisions|questions|all] [--format md|json]"
	sliceEntryUsage = "physa map slice --entry <slug>... [--format md|json]"
)

// helpOf returns the help text of the command at path, as --help prints it.
func helpOf(t *testing.T, path ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(append(path, "--help"), &stdout, &stderr); code != 0 {
		t.Fatalf("run(%v --help) = %d", path, code)
	}
	return stdout.String()
}

func TestHelpNamesSlice(t *testing.T) {
	help := helpOf(t, "map", "slice")
	for _, u := range []string{sliceUsage, sliceEntryUsage} {
		if !strings.Contains(help, u) {
			t.Errorf("help = %q, want it to name %q", help, u)
		}
	}
}

func TestSliceGoodMap(t *testing.T) {
	t.Chdir(tempMap(t, "`r` · from docs/d.md"))
	useStore(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"map", "slice", "a"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "# Slice: a\n") {
		t.Errorf("stdout = %q, want a slice header", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Capability: a") {
		t.Errorf("stdout = %q, want the anchor trailer", stdout.String())
	}
}

func TestSliceOwnMap(t *testing.T) {
	t.Chdir("../..")
	useStore(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "slice", "slices", "map"}, &stdout, &stderr)
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
	useStore(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "slice", "map", "--format", "json", "--with", "all"}, &stdout, &stderr)
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
	useStore(t)
	var want bytes.Buffer
	if code := run([]string{"map", "slice", "slices", "map", "--format", "json", "--with", "all"}, &want, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	for _, args := range [][]string{
		{"--format=json", "--with=all", "slices", "map"},
		{"slices", "--format", "json", "map", "--with", "all"},
		{"map", "slices", "map", "--with=all", "--format=json"},
	} {
		var got, stderr bytes.Buffer
		if code := run(append([]string{"map", "slice"}, args...), &got, &stderr); code != 0 {
			t.Fatalf("run(%v) = %d; stderr %q", args, code, stderr.String())
		}
		if got.String() != want.String() {
			t.Errorf("run(%v) differs from the reference output", args)
		}
	}
}

func TestSliceWithKinds(t *testing.T) {
	t.Chdir("../..")
	useStore(t)
	for _, w := range []string{"decisions", "questions", "all"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"map", "slice", "slices", "--with", w}, &stdout, &stderr); code != 0 {
			t.Errorf("--with %s: code = %d; stderr %q", w, code, stderr.String())
		}
	}
}

func TestSliceBadUsage(t *testing.T) {
	t.Chdir("../..")
	useStore(t)
	for _, args := range [][]string{
		{"map", "slice"},
		{"map", "slice", "--format", "json"},
		{"map", "slice", "slices", "--budget", "3000"},
		{"map", "slice", "--entry"},
		{"map", "slice", "--entry", "--format", "json"},
		{"map", "slice", "init", "--entry", "x"},
		{"map", "slice", "--entry", "x", "--with", "all"},
		{"map", "slice", "slices", "--with", "bad"},
		{"map", "slice", "slices", "--format", "bad"},
		{"map", "slice", "slices", "--change", "x"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), sliceUsage) || !strings.Contains(stderr.String(), sliceEntryUsage) {
			t.Errorf("run(%v) stderr = %q, want both usages", args, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%v) stdout = %q, want empty", args, stdout.String())
		}
	}
}

func TestSliceUnknownCapability(t *testing.T) {
	t.Chdir("../..")
	useStore(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "slice", "nope"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), "physa map slice: unknown capability \"nope\"; run physa map index to see them\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestSliceNoMapIsError(t *testing.T) {
	t.Chdir(t.TempDir())
	useStore(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "slice", "slices"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.HasPrefix(stderr.String(), "physa map slice: ") {
		t.Errorf("stderr = %q, want a physa map slice error", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestSliceMapWithProblemFails(t *testing.T) {
	t.Chdir(tempMap(t, "r from docs/d.md"))
	useStore(t)
	for _, args := range [][]string{{"map", "slice", "a"}, {"map", "slice", "--entry", "r"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(%v) = %d, want 2; stderr %q", args, code, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%v) stdout = %q, want empty", args, stdout.String())
		}
		if got := stderr.String(); !strings.HasPrefix(got, "physa map slice: the map has ") ||
			!strings.Contains(got, ".physa/capabilities/a/rules.md:") {
			t.Errorf("run(%v) stderr = %q, want the problem named by file and line", args, got)
		}
	}
	if recs := listed(t); len(recs) != 0 {
		t.Errorf("records = %+v, want none", recs)
	}
}

// entryText returns the stdout of physa map slice --entry with args, failing the test on a non-zero code.
func entryText(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(append([]string{"map", "slice", "--entry"}, args...), &stdout, &stderr); code != 0 {
		t.Fatalf("run(%v) = %d; stderr %q", args, code, stderr.String())
	}
	return stdout.String()
}

func TestSliceEntriesInOrderFromTwoCapabilities(t *testing.T) {
	t.Chdir("../..")
	useStore(t)
	out := entryText(t, "no-adr", "github-first")
	i, j := strings.Index(out, "`no-adr`"), strings.Index(out, "`github-first`")
	if i < 0 || j < i {
		t.Errorf("stdout = %q, want no-adr then github-first", out)
	}
	if strings.Contains(out, "# Slice") {
		t.Errorf("stdout = %q, want entries only", out)
	}
	if rev := entryText(t, "github-first", "no-adr"); strings.Index(rev, "`github-first`") > strings.Index(rev, "`no-adr`") {
		t.Errorf("reversed ask comes back in the old order: %q", rev)
	}
}

func TestSliceEntryProductSlug(t *testing.T) {
	t.Chdir("../..")
	useStore(t)
	if out := entryText(t, "no-adr"); !strings.HasPrefix(out, "### Never write \"ADR\"\n`no-adr`") {
		t.Errorf("stdout = %q, want the product entry", out)
	}
}

func TestSliceEntryUnknownSlugsNamed(t *testing.T) {
	t.Chdir("../..")
	useStore(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"map", "slice", "--entry", "no-adr", "nope-one", "nope-two"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	for _, slug := range []string{"nope-one", "nope-two"} {
		if !strings.Contains(stderr.String(), slug) {
			t.Errorf("stderr = %q, want it to name %s", stderr.String(), slug)
		}
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if recs := listed(t); len(recs) != 0 {
		t.Errorf("records = %+v, want none", recs)
	}
}

func TestSliceEntryJSON(t *testing.T) {
	t.Chdir("../..")
	useStore(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"map", "slice", "--entry", "no-adr", "github-first", "--format", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d; stderr %q", code, stderr.String())
	}
	var v struct {
		Entries []struct {
			Slug string `json:"slug"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if len(v.Entries) != 2 || v.Entries[0].Slug != "no-adr" || v.Entries[1].Slug != "github-first" {
		t.Errorf("entries = %+v, want no-adr then github-first", v.Entries)
	}
}

const changesUsage = "physa map changes [<range>] [--json] [--strict]"

func TestHelpNamesMapChanges(t *testing.T) {
	if help := helpOf(t, "map", "changes"); !strings.Contains(help, changesUsage) {
		t.Errorf("help = %q, want it to name %q", help, changesUsage)
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
	folder(t, root, mapfolder.CapabilityDir("b"), "---\npromise: y\n---\n# B\n\n")
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "base")
	for _, msg := range []string{
		"feat: ok\n\nCapability: a",
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
	code := run(append([]string{"map", "changes"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTrailersText(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runTrailers(t, "HEAD~4..HEAD")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, errOut)
	}
	for _, want := range []string{
		"feat: odd\n  capability: nosuch\n",
		"feat: bare\n  capability: \u2014\n",
		"feat: two\n  capability: a, b\n",
		"feat: ok\n  capability: a\n",
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
	if code != 0 || !strings.HasSuffix(out, "\nphysa map changes: ok (1 commit)\n") {
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
	if code != 2 || !strings.HasPrefix(errOut, "physa map changes: ") {
		t.Errorf("code = %d, stderr = %q, want 2 for a missing origin/main", code, errOut)
	}
}

func TestTrailersBadRange(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runTrailers(t, "nosuch..HEAD")
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "physa map changes: ") {
		t.Errorf("code = %d, stdout = %q, stderr = %q, want 2 and a git error", code, out, errOut)
	}
}

func TestTrailersNotARepo(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	t.Chdir(tempMap(t, "r from docs/d.md"))
	code, _, errOut := runTrailers(t, "HEAD~1..HEAD")
	if code != 2 || !strings.Contains(errOut, "not a repository under version control") {
		t.Errorf("code = %d, stderr = %q, want 2 and not a repository", code, errOut)
	}
}

func TestTrailersNoMapIsError(t *testing.T) {
	t.Chdir(t.TempDir())
	code, _, errOut := runTrailers(t, "HEAD~1..HEAD")
	if code != 2 || !strings.HasPrefix(errOut, "physa map changes: ") {
		t.Errorf("code = %d, stderr = %q, want 2", code, errOut)
	}
}

func TestTrailersBadUsage(t *testing.T) {
	for _, args := range [][]string{{"a", "b"}, {"--bogus"}, {"--json=1"}} {
		var stdout, stderr bytes.Buffer
		code := run(append([]string{"map", "changes"}, args...), &stdout, &stderr)
		if code != 2 || stderr.String() != helpOf(t, "map", "changes") || stdout.Len() != 0 {
			t.Errorf("%v: code = %d, stderr = %q, want 2 and usage", args, code, stderr.String())
		}
	}
}

func TestOldCommandsPrintRootHelp(t *testing.T) {
	for _, args := range [][]string{
		{"index"}, {"slice", "map"}, {"trailers"}, {"pr-check"}, {"records"},
		{"review"}, {"review", "--json"}, {"--version"}, {"-version"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if stderr.String() != rootHelp() || stdout.Len() != 0 {
			t.Errorf("run(%v) stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestSyncIsNotBuilt(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"sync"}, &stdout, &stderr)
	want := "physa sync: not yet; it returns with digests, after the engine (design 07 \u00a77)\n"
	if code != 2 || stderr.String() != want || stdout.Len() != 0 {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestMapHooksWritesEmbeddedHook(t *testing.T) {
	dir := trailersRepo(t)
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	file := filepath.Join(dir, ".git", "hooks", "prepare-commit-msg")
	if code := run([]string{"map", "hooks"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 ||
		!strings.HasPrefix(stdout.String(), "prepare-commit-msg written to ") ||
		!strings.HasSuffix(stdout.String(), "/.git/hooks/prepare-commit-msg: commits made in Claude Code now carry Session:.\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("hook not written executable: %v, %v", info, err)
	}
	if out, _ := exec.Command("git", "-C", dir, "config", "--local", "--get", "core.hooksPath").Output(); len(out) != 0 {
		t.Errorf("core.hooksPath = %q, want unset", out)
	}
}

func TestNoteProblemsSingularAndPlural(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{
		{0, ""},
		{1, "physa map slice: the map has 1 problem; run physa check map\n"},
		{2, "physa map slice: the map has 2 problems; run physa check map\n"},
	} {
		var b bytes.Buffer
		noteProblems(&b, "slice", c.n)
		if b.String() != c.want {
			t.Errorf("noteProblems(%d) = %q, want %q", c.n, b.String(), c.want)
		}
	}
}

// fmtRun runs physa map fmt with args and returns the code and both outputs.
func fmtRun(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"map", "fmt"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestFmtCheckThenFix(t *testing.T) {
	root := tempMap(t, "`r` · from docs/d.md")
	rules := filepath.Join(root, ".physa/capabilities/a/rules.md")
	b, err := os.ReadFile(rules)
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(b), "- [R](#r)\n", "", 1)
	if stale == string(b) {
		t.Fatalf("no contents line in %q", b)
	}
	if err := os.WriteFile(rules, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	want := ".physa/capabilities/a/rules.md: contents or spacing differ; run physa map fmt\n"
	if code, out, _ := fmtRun("--check", root); code != 1 || out != want {
		t.Errorf("--check = %d, %q; want 1, %q", code, out, want)
	}
	if got, _ := os.ReadFile(rules); string(got) != stale {
		t.Errorf("--check changed the file: %q", got)
	}
	if code, out, _ := fmtRun(root); code != 0 || out != "formatted .physa/capabilities/a/rules.md\n" {
		t.Errorf("fmt = %d, %q", code, out)
	}
	if got, _ := os.ReadFile(rules); string(got) != string(b) {
		t.Errorf("fmt wrote %q, want %q", got, b)
	}
	if code, out, errOut := fmtRun("--check", root); code != 0 || out != "" || errOut != "" {
		t.Errorf("--check after fmt = %d, %q, %q", code, out, errOut)
	}
}

func TestFmtOldLayoutExitsTwo(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".physa/product.md", "---\npromise: x\n---\n# T\n")
	for _, args := range [][]string{{root}, {"--check", root}} {
		code, out, errOut := fmtRun(args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "old single-file layout; convert") {
			t.Errorf("fmt %v = %d, %q, %q; want 2 and the convert message", args, code, out, errOut)
		}
	}
}

func TestFmtBadUsage(t *testing.T) {
	for _, args := range [][]string{{"a", "b"}, {"--fix"}, {"a", "--check"}} {
		if code, _, _ := fmtRun(args...); code != 2 {
			t.Errorf("fmt %v = %d, want 2", args, code)
		}
	}
}
