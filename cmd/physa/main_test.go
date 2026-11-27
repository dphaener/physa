package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/forge/github"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/proc"
	"github.com/dphaener/physa/internal/store"
)

// TestMain refuses to run the test binary as an engine, and points the
// process environment every test inherits away from the real ~/.claude,
// the running session and the real person setting. What a test reaches
// beyond the process it reaches through the deps it builds with testDeps.
func TestMain(m *testing.M) {
	// The test binary never acts as an engine.
	if i := slices.Index(os.Args, "engine"); i >= 0 && i+1 < len(os.Args) && os.Args[i+1] == "run" {
		fmt.Fprintln(os.Stderr, "physa test binary: refusing to run as an engine")
		os.Exit(3)
	}
	// Not the real ~/.claude, nor the running session's id.
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
	os.RemoveAll(claudeDir)
	os.RemoveAll(configDir)
	os.Exit(code)
}

// fakeGitHub returns a client whose gh knows exactly the PRs in known (a
// nil map knows none) and no PR for any change; any other call fails with a
// 404.
func fakeGitHub(dir string, known map[int]bool) *github.Client {
	return github.NewWithRunner(dir, func(_ context.Context, _ string, args ...string) ([]byte, error) {
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

// testDeps are the deps of one test, which keep it off GitHub, off the store
// of this repository and off a real agent: GitHub knows no PRs and no change
// came from one, the store is a fresh one in a temp dir, and starting claude
// stops the binary. A test that wants another fake changes its own copy.
// The process state is the test process's, read each time a command asks,
// with testGit added. A test that reaches a directory or a variable passes
// its own state with depsIn or inDir.
func testDeps(t *testing.T) deps {
	t.Helper()
	dir := t.TempDir()
	return deps{
		newForge:  func(dir string) forge.Forge { return fakeGitHub(dir, nil) },
		openStore: func(ctx context.Context, _ string) (*store.Store, error) { return store.Open(ctx, dir) },
		agents:    map[string]func() adapter.Adapter{"claude": func() adapter.Adapter { return unexpectedAgent{} }},
		proc:      testProcess,
	}
}

// testProcess is the state of the test process as it is now, with testGit.
func testProcess() proc.State {
	wd, err := os.Getwd()
	return proc.State{Dir: ".", Wd: wd, WdErr: err, Env: testEnv()}
}

// testGit keeps a test's git off the person's and the system's config, and
// gives its commits a fixed identity.
var testGit = []string{
	"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
	"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
	"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
}

// testEnv is the test process's environment with testGit and then kv
// added; where a variable appears twice, the last one wins.
func testEnv(kv ...string) []string {
	return slices.Concat(os.Environ(), testGit, kv)
}

// depsIn are testDeps whose commands work in dir, with testEnv(kv...) as
// their environment.
func depsIn(t *testing.T, dir string, kv ...string) deps {
	t.Helper()
	return inDir(t, testDeps(t), dir, kv...)
}

// inDir is d with its commands working in dir, with testEnv(kv...) as their
// environment; the fakes and the store of d are kept.
func inDir(t *testing.T, d deps, dir string, kv ...string) deps {
	t.Helper()
	wd, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := proc.State{Dir: dir, Wd: wd, Env: testEnv(kv...)}
	d.proc = func() proc.State { return st }
	return d
}

// ownRoot is the root of this repository, whose map some tests read.
const ownRoot = "../.."

// useGitHub makes the GitHub of d know the PRs in known.
func useGitHub(d *deps, known ...int) {
	set := map[int]bool{}
	for _, n := range known {
		set[n] = true
	}
	d.newForge = func(dir string) forge.Forge { return fakeGitHub(dir, set) }
}

func TestVersion(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), testDeps(t), []string{"version"}, &stdout, &stderr); code != 0 {
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
func rootHelp(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	command.Help(rootCommand(testDeps(t)), &b)
	return b.String()
}

func TestRootHelpListsTheCommands(t *testing.T) {
	t.Parallel()
	help := rootHelp(t)
	for _, name := range []string{"check ", "map ", "init ", "idea ", "pursue ", "pursuit ", "evidence ", "engine ", "api ", "sync ", "version ", "help "} {
		if !strings.Contains(help, "\n  "+name) {
			t.Errorf("root help lacks %q:\n%s", name, help)
		}
	}
}

func TestHelpListsEngineAndAPI(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), testDeps(t), []string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	help, last := stdout.String(), -1
	for _, name := range []string{"check", "map", "init", "idea", "pursue", "pursuit", "evidence", "engine", "api", "sync", "version", "help"} {
		i := strings.Index(help, "\n  "+name+" ")
		if i <= last {
			t.Errorf("physa help lists %q out of the order check, map, init, idea, pursue, pursuit, evidence, engine, api, sync, version, help:\n%s", name, help)
		}
		last = i
	}
}

func TestHelpPrintsRootHelp(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"help"}, {"--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(t.Context(), testDeps(t), args, &stdout, &stderr); code != 0 {
			t.Errorf("run(t.Context(), testDeps(t), %v) = %d, want 0", args, code)
		}
		if stdout.String() != rootHelp(t) || stderr.Len() != 0 {
			t.Errorf("run(t.Context(), testDeps(t), %v) stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestHelpOnGroupsAndLeaves(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"check", "--help"}, {"check", "map", "--help"}, {"check", "changes", "--help"},
		{"map", "--help"}, {"map", "index", "--help"}, {"map", "slice", "--help"},
		{"map", "changes", "--help"}, {"map", "records", "--help"},
		{"version", "--help"}, {"sync", "--help"},
		{"engine", "--help"}, {"engine", "run", "--help"}, {"api", "--help"}, {"api", "call", "--help"},
		{"idea", "--help"}, {"idea", "add", "--help"}, {"pursue", "--help"},
		{"pursuit", "--help"}, {"pursuit", "list", "--help"}, {"pursuit", "stop", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(t.Context(), testDeps(t), args, &stdout, &stderr); code != 0 {
			t.Errorf("run(t.Context(), testDeps(t), %v) = %d, want 0", args, code)
		}
		if !strings.HasPrefix(stdout.String(), "usage: physa "+args[0]) || stderr.Len() != 0 {
			t.Errorf("run(t.Context(), testDeps(t), %v) stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestNoArgsAndUnknownPrintRootHelp(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"bogus"}, {"bet"}, {"bet", "list"}} {
		var stdout, stderr bytes.Buffer
		if code := run(t.Context(), testDeps(t), args, &stdout, &stderr); code != 2 {
			t.Errorf("run(t.Context(), testDeps(t), %v) = %d, want 2", args, code)
		}
		if stderr.String() != rootHelp(t) || stdout.Len() != 0 {
			t.Errorf("run(t.Context(), testDeps(t), %v) stdout %q, stderr %q", args, stdout.String(), stderr.String())
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
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"check", "map", tempMap(t, "`r` · from docs/d.md")}, &stdout, &stderr)
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
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"check", "map", tempMap(t, "r from docs/d.md")}, &stdout, &stderr)
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
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"check", "map", t.TempDir()}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.HasPrefix(stderr.String(), "physa check map: ") {
		t.Errorf("stderr = %q, want a physa check map error", stderr.String())
	}
}

func TestCheckMapTooManyArgsPrintsUsage(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"check", "map", "a", "b"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), helpOf(t, "check", "map"); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestIndexGoodMap(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"map", "index", tempMap(t, "`r` · from docs/d.md")}, &stdout, &stderr)
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
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"map", "index", tempMap(t, "r from docs/d.md")}, &stdout, &stderr)
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
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"map", "index", t.TempDir()}, &stdout, &stderr)
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
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"map", "index", "a", "b"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if got, want := stderr.String(), helpOf(t, "map", "index"); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestBareMapEqualsMapIndex(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	var a, b, errA, errB bytes.Buffer
	codeA := run(t.Context(), d, []string{"map"}, &a, &errA)
	codeB := run(t.Context(), d, []string{"map", "index"}, &b, &errB)
	if codeA != 0 || codeB != 0 || a.Len() == 0 || a.String() != b.String() || errA.String() != errB.String() {
		t.Errorf("map: %d %q %q; map index: %d %q %q", codeA, a.String(), errA.String(), codeB, b.String(), errB.String())
	}
}

func TestMapBadArgumentPrintsMapHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), testDeps(t), []string{"map", "nonsense"}, &stdout, &stderr); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if stderr.String() != helpOf(t, "map") || stdout.Len() != 0 {
		t.Errorf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}

func TestIndexOwnMap(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"map", "index", "../.."}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr %q", code, stderr.String())
	}
	folders, err := os.ReadDir("../../.physa/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, f := range folders {
		if f.IsDir() {
			want++
		}
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != want {
		t.Fatalf("got %d lines, want one for each of the %d capability folders:\n%s", len(lines), want, stdout.String())
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
	if code := run(t.Context(), testDeps(t), append(path, "--help"), &stdout, &stderr); code != 0 {
		t.Fatalf("run(t.Context(), testDeps(t), %v --help) = %d", path, code)
	}
	return stdout.String()
}

func TestHelpNamesSlice(t *testing.T) {
	t.Parallel()
	help := helpOf(t, "map", "slice")
	for _, u := range []string{sliceUsage, sliceEntryUsage} {
		if !strings.Contains(help, u) {
			t.Errorf("help = %q, want it to name %q", help, u)
		}
	}
}

func TestSliceGoodMap(t *testing.T) {
	t.Parallel()
	d := depsIn(t, tempMap(t, "`r` · from docs/d.md"))
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "slice", "a"}, &stdout, &stderr); code != 0 {
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
	t.Parallel()
	d := depsIn(t, ownRoot)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), d, []string{"map", "slice", "slices", "map"}, &stdout, &stderr)
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
	t.Parallel()
	d := depsIn(t, ownRoot)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), d, []string{"map", "slice", "map", "--format", "json", "--with", "all"}, &stdout, &stderr)
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
	t.Parallel()
	d := depsIn(t, ownRoot)
	var want bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "slice", "slices", "map", "--format", "json", "--with", "all"}, &want, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	for _, args := range [][]string{
		{"--format=json", "--with=all", "slices", "map"},
		{"slices", "--format", "json", "map", "--with", "all"},
		{"map", "slices", "map", "--with=all", "--format=json"},
	} {
		var got, stderr bytes.Buffer
		if code := run(t.Context(), d, append([]string{"map", "slice"}, args...), &got, &stderr); code != 0 {
			t.Fatalf("run(t.Context(), d, %v) = %d; stderr %q", args, code, stderr.String())
		}
		if got.String() != want.String() {
			t.Errorf("run(t.Context(), d, %v) differs from the reference output", args)
		}
	}
}

func TestSliceWithKinds(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	for _, w := range []string{"decisions", "questions", "all"} {
		var stdout, stderr bytes.Buffer
		if code := run(t.Context(), d, []string{"map", "slice", "slices", "--with", w}, &stdout, &stderr); code != 0 {
			t.Errorf("--with %s: code = %d; stderr %q", w, code, stderr.String())
		}
	}
}

func TestSliceBadUsage(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
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
		if code := run(t.Context(), d, args, &stdout, &stderr); code != 2 {
			t.Errorf("run(t.Context(), d, %v) = %d, want 2", args, code)
		}
		if !strings.Contains(stderr.String(), sliceUsage) || !strings.Contains(stderr.String(), sliceEntryUsage) {
			t.Errorf("run(t.Context(), d, %v) stderr = %q, want both usages", args, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("run(t.Context(), d, %v) stdout = %q, want empty", args, stdout.String())
		}
	}
}

func TestSliceUnknownCapability(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), d, []string{"map", "slice", "nope"}, &stdout, &stderr)
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
	t.Parallel()
	d := depsIn(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), d, []string{"map", "slice", "slices"}, &stdout, &stderr)
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
	t.Parallel()
	d := depsIn(t, tempMap(t, "r from docs/d.md"))
	for _, args := range [][]string{{"map", "slice", "a"}, {"map", "slice", "--entry", "r"}} {
		var stdout, stderr bytes.Buffer
		if code := run(t.Context(), d, args, &stdout, &stderr); code != 2 {
			t.Fatalf("run(t.Context(), d, %v) = %d, want 2; stderr %q", args, code, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("run(t.Context(), d, %v) stdout = %q, want empty", args, stdout.String())
		}
		if got := stderr.String(); !strings.HasPrefix(got, "physa map slice: the map has ") ||
			!strings.Contains(got, ".physa/capabilities/a/rules.md:") {
			t.Errorf("run(t.Context(), d, %v) stderr = %q, want the problem named by file and line", args, got)
		}
	}
	if recs := listed(t, d); len(recs) != 0 {
		t.Errorf("records = %+v, want none", recs)
	}
}

// entryText returns the stdout of physa map slice --entry with args, failing the test on a non-zero code.
func entryText(t *testing.T, d deps, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), d, append([]string{"map", "slice", "--entry"}, args...), &stdout, &stderr); code != 0 {
		t.Fatalf("run(t.Context(), testDeps(t), %v) = %d; stderr %q", args, code, stderr.String())
	}
	return stdout.String()
}

func TestSliceEntriesInOrderFromTwoCapabilities(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	out := entryText(t, d, "no-adr", "github-first")
	i, j := strings.Index(out, "`no-adr`"), strings.Index(out, "`github-first`")
	if i < 0 || j < i {
		t.Errorf("stdout = %q, want no-adr then github-first", out)
	}
	if strings.Contains(out, "# Slice") {
		t.Errorf("stdout = %q, want entries only", out)
	}
	if rev := entryText(t, d, "github-first", "no-adr"); strings.Index(rev, "`github-first`") > strings.Index(rev, "`no-adr`") {
		t.Errorf("reversed ask comes back in the old order: %q", rev)
	}
}

func TestSliceEntryProductSlug(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	if out := entryText(t, d, "no-adr"); !strings.HasPrefix(out, "### Never write \"ADR\"\n`no-adr`") {
		t.Errorf("stdout = %q, want the product entry", out)
	}
}

func TestSliceEntryUnknownSlugsNamed(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), d, []string{"map", "slice", "--entry", "no-adr", "nope-one", "nope-two"}, &stdout, &stderr)
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
	if recs := listed(t, d); len(recs) != 0 {
		t.Errorf("records = %+v, want none", recs)
	}
}

func TestSliceEntryJSON(t *testing.T) {
	t.Parallel()
	d := depsIn(t, ownRoot)
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "slice", "--entry", "no-adr", "github-first", "--format", "json"}, &stdout, &stderr); code != 0 {
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
	t.Parallel()
	if help := helpOf(t, "map", "changes"); !strings.Contains(help, changesUsage) {
		t.Errorf("help = %q, want it to name %q", help, changesUsage)
	}
}

// gitIn runs git in dir with testEnv, so the person's git config is ignored,
// and returns its output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return gitWith(t, nil, dir, args...)
}

// gitWith runs git in dir as gitIn does, with kv added to its environment.
func gitWith(t *testing.T, kv []string, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir, cmd.Env = dir, testEnv(kv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// trailersRepo is a repo with a small map and four commits on top of a base:
// one sound, one without a trailer, one naming two capabilities and one
// naming a slug the map lacks. Its commits are made with kv added to git's
// environment.
func trailersRepo(t *testing.T, kv ...string) string {
	t.Helper()
	root := tempMap(t, "r from docs/d.md")
	folder(t, root, mapfolder.CapabilityDir("b"), "---\npromise: y\n---\n# B\n\n")
	gitWith(t, kv, root, "init", "-q")
	gitWith(t, kv, root, "add", ".")
	gitWith(t, kv, root, "commit", "-q", "-m", "base")
	for _, msg := range []string{
		"feat: ok\n\nCapability: a",
		"feat: bare",
		"feat: two\n\nCapability: a, b",
		"feat: odd\n\nCapability: nosuch",
	} {
		gitWith(t, kv, root, "commit", "-q", "--allow-empty", "-m", msg)
	}
	return root
}

func runTrailers(t *testing.T, d deps, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), d, append([]string{"map", "changes"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTrailersText(t *testing.T) {
	t.Parallel()
	code, out, errOut := runTrailers(t, depsIn(t, trailersRepo(t)), "HEAD~4..HEAD")
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
	t.Parallel()
	code, out, _ := runTrailers(t, depsIn(t, trailersRepo(t)), "HEAD~4..HEAD~3", "--strict")
	if code != 0 || !strings.HasSuffix(out, "\nphysa map changes: ok (1 commit)\n") {
		t.Errorf("code = %d, stdout = %q, want 0 and ok", code, out)
	}
}

func TestTrailersJSON(t *testing.T) {
	t.Parallel()
	d := depsIn(t, trailersRepo(t))
	code, out, _ := runTrailers(t, d, "--json", "HEAD~4..HEAD")
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
	code, out, _ = runTrailers(t, d, "HEAD~4..HEAD~3", "--json")
	if code != 0 || !strings.Contains(out, `"findings":[]`) || strings.Contains(out, "null") {
		t.Errorf("code = %d, stdout = %q, want empty findings and no null", code, out)
	}
	code, out, _ = runTrailers(t, d, "HEAD..HEAD", "--json")
	if code != 0 || !strings.Contains(out, `"commits":[]`) {
		t.Errorf("code = %d, stdout = %q, want empty commits", code, out)
	}
}

func TestTrailersStrict(t *testing.T) {
	t.Parallel()
	d := depsIn(t, trailersRepo(t))
	if code, _, _ := runTrailers(t, d, "HEAD~4..HEAD", "--strict"); code != 1 {
		t.Errorf("strict with findings: code = %d, want 1", code)
	}
	if code, _, _ := runTrailers(t, d, "HEAD~4..HEAD"); code != 0 {
		t.Errorf("without strict: code = %d, want 0", code)
	}
}

func TestTrailersDefaultRange(t *testing.T) {
	t.Parallel()
	code, _, errOut := runTrailers(t, depsIn(t, trailersRepo(t)))
	if code != 2 || !strings.HasPrefix(errOut, "physa map changes: ") {
		t.Errorf("code = %d, stderr = %q, want 2 for a missing origin/main", code, errOut)
	}
}

func TestTrailersBadRange(t *testing.T) {
	t.Parallel()
	code, out, errOut := runTrailers(t, depsIn(t, trailersRepo(t)), "nosuch..HEAD")
	if code != 2 || out != "" || !strings.HasPrefix(errOut, "physa map changes: ") {
		t.Errorf("code = %d, stdout = %q, stderr = %q, want 2 and a git error", code, out, errOut)
	}
}

// ceiling is the variable that stops git looking for a repo above a test's
// temporary directories.
func ceiling(t *testing.T) string {
	t.Helper()
	return "GIT_CEILING_DIRECTORIES=" + filepath.Dir(t.TempDir())
}

func TestTrailersNotARepo(t *testing.T) {
	t.Parallel()
	code, _, errOut := runTrailers(t, depsIn(t, tempMap(t, "r from docs/d.md"), ceiling(t)), "HEAD~1..HEAD")
	if code != 2 || !strings.Contains(errOut, "not a repository under version control") {
		t.Errorf("code = %d, stderr = %q, want 2 and not a repository", code, errOut)
	}
}

func TestTrailersNoMapIsError(t *testing.T) {
	t.Parallel()
	code, _, errOut := runTrailers(t, depsIn(t, t.TempDir()), "HEAD~1..HEAD")
	if code != 2 || !strings.HasPrefix(errOut, "physa map changes: ") {
		t.Errorf("code = %d, stderr = %q, want 2", code, errOut)
	}
}

func TestTrailersBadUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"a", "b"}, {"--bogus"}, {"--json=maybe"}} {
		var stdout, stderr bytes.Buffer
		code := run(t.Context(), testDeps(t), append([]string{"map", "changes"}, args...), &stdout, &stderr)
		if code != 2 || stderr.String() != helpOf(t, "map", "changes") || stdout.Len() != 0 {
			t.Errorf("%v: code = %d, stderr = %q, want 2 and usage", args, code, stderr.String())
		}
	}
}

func TestOldCommandsPrintRootHelp(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"index"}, {"slice", "map"}, {"trailers"}, {"pr-check"}, {"records"},
		{"review"}, {"review", "--json"}, {"--version"}, {"-version"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(t.Context(), testDeps(t), args, &stdout, &stderr); code != 2 {
			t.Errorf("run(t.Context(), testDeps(t), %v) = %d, want 2", args, code)
		}
		if stderr.String() != rootHelp(t) || stdout.Len() != 0 {
			t.Errorf("run(t.Context(), testDeps(t), %v) stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestSyncIsNotBuilt(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), []string{"sync"}, &stdout, &stderr)
	want := "physa sync: not yet; it returns with digests, after the engine (design 07 \u00a77)\n"
	if code != 2 || stderr.String() != want || stdout.Len() != 0 {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestMapHooksWritesEmbeddedHook(t *testing.T) {
	t.Parallel()
	dir := trailersRepo(t)
	var stdout, stderr bytes.Buffer
	file := filepath.Join(dir, ".git", "hooks", "prepare-commit-msg")
	if code := run(t.Context(), depsIn(t, dir), []string{"map", "hooks"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 ||
		!strings.HasPrefix(stdout.String(), "prepare-commit-msg written to ") ||
		!strings.HasSuffix(stdout.String(), "/.git/hooks/prepare-commit-msg: commits made in Claude Code now carry Session:.\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("hook not written executable: %v, %v", info, err)
	}
	cmd := exec.Command("git", "config", "--local", "--get", "core.hooksPath")
	cmd.Dir, cmd.Env = dir, testEnv()
	if out, _ := cmd.Output(); len(out) != 0 {
		t.Errorf("core.hooksPath = %q, want unset", out)
	}
}

func TestNoteProblemsSingularAndPlural(t *testing.T) {
	t.Parallel()
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
func fmtRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), testDeps(t), append([]string{"map", "fmt"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestFmtCheckThenFix(t *testing.T) {
	t.Parallel()
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
	if code, out, _ := fmtRun(t, "--check", root); code != 1 || out != want {
		t.Errorf("--check = %d, %q; want 1, %q", code, out, want)
	}
	if got, _ := os.ReadFile(rules); string(got) != stale {
		t.Errorf("--check changed the file: %q", got)
	}
	if code, out, _ := fmtRun(t, root); code != 0 || out != "formatted .physa/capabilities/a/rules.md\n" {
		t.Errorf("fmt = %d, %q", code, out)
	}
	if got, _ := os.ReadFile(rules); string(got) != string(b) {
		t.Errorf("fmt wrote %q, want %q", got, b)
	}
	if code, out, errOut := fmtRun(t, "--check", root); code != 0 || out != "" || errOut != "" {
		t.Errorf("--check after fmt = %d, %q, %q", code, out, errOut)
	}
}

func TestFmtOldLayoutExitsTwo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, ".physa/product.md", "---\npromise: x\n---\n# T\n")
	for _, args := range [][]string{{root}, {"--check", root}} {
		code, out, errOut := fmtRun(t, args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "old single-file layout; convert") {
			t.Errorf("fmt %v = %d, %q, %q; want 2 and the convert message", args, code, out, errOut)
		}
	}
}

func TestFmtBadUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"a", "b"}, {"--fix"}, {"a", "--check"}} {
		if code, _, _ := fmtRun(t, args...); code != 2 {
			t.Errorf("fmt %v = %d, want 2", args, code)
		}
	}
}
