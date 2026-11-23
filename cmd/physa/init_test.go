package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// mergedFake is a forge that lists the merged PRs it was given.
type mergedFake struct{ prs []forge.PR }

func (mergedFake) Name() string                            { return "fake" }
func (mergedFake) PRExists(int) (bool, error)              { return false, nil }
func (mergedFake) ChangePR(string) (forge.PR, bool, error) { return forge.PR{}, false, nil }
func (m mergedFake) MergedPRs(time.Time, int) ([]forge.PR, bool, error) {
	return m.prs, false, nil
}

func useForge(t *testing.T, f forge.Forge) {
	t.Helper()
	old := newForge
	newForge = func(string) forge.Forge { return f }
	t.Cleanup(func() { newForge = old })
}

// useRepoStore makes init write to the store of the repo in dir, as it does
// outside the tests, which keep off the real store.
func useRepoStore(t *testing.T, dir string) {
	t.Helper()
	old := openStore
	openStore = gitStore
	t.Cleanup(func() { openStore = old })
	t.Chdir(dir)
}

func runInit(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"init"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func storedGather(t *testing.T, dir string) store.Gather {
	t.Helper()
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := repo.StoreDir()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(sd)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g, ok, err := st.Gathered()
	if err != nil || !ok {
		t.Fatalf("gathered: ok %v, err %v", ok, err)
	}
	return g
}

func TestInitGatherJSON(t *testing.T) {
	dir := trailersRepo(t)
	write(t, dir, "AGENTS.md", "rules\n")
	write(t, dir, "README.md", "hello\n")
	useRepoStore(t, dir)
	useForge(t, mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}})
	code, out, errOut := runInit("--gather", "--json")
	if code != 0 || errOut != "physa init: gather starts no agent (discovery not started: the repo has a map)\n" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var sum gather.Summary
	if err := json.Unmarshal([]byte(out), &sum); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if sum.Docs.Count < 2 || sum.History.Changes < 5 || sum.PRs.Count != 1 || !sum.PRs.Available {
		t.Errorf("summary %+v", sum)
	}
	g := storedGather(t, dir)
	if len(g.Docs) != sum.Docs.Count || len(g.Changes) != sum.History.Changes || len(g.PRs) != 1 {
		t.Errorf("store holds %d docs, %d changes, %d PRs; summary %+v", len(g.Docs), len(g.Changes), len(g.PRs), sum)
	}
}

func TestInitBareNewCodebaseReadsAnswers(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	dir := filepath.Join(t.TempDir(), "lathe")
	write(t, dir, "README.md", "# Lathe\n")
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "more")
	answers := filepath.Join(t.TempDir(), "answers.txt")
	write(t, filepath.Dir(answers), "answers.txt", "2: Lathe turns sketches into cut lists.\n")
	useRepoStore(t, dir)
	useForge(t, mergedFake{})
	f := &fakeAgent{}
	useAgent(t, "claude", f)
	code, out, errOut := runInit("--answers", answers)
	if code != 0 || f.starts != 1 || !strings.Contains(out, "A new codebase") || // discovery's session alone
		!strings.Contains(out, "Lathe, promising: Lathe turns sketches into cut lists.") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestInitPushWithoutWriteIsBadUsage(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, _, errOut := runInit("--push")
	if code != 2 || !strings.HasPrefix(errOut, "usage: physa init") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitBadSinceIsBadUsage(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, _, errOut := runInit("--gather", "--since", "yesterday")
	if code != 2 || !strings.HasPrefix(errOut, "usage: physa init") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitHelp(t *testing.T) {
	code, out, errOut := runInit("--help")
	if code != 0 || errOut != "" || !strings.HasPrefix(out, "usage: physa init [--write") || !strings.Contains(out, "physa init --gather") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestHelpListsInit(t *testing.T) {
	if help := rootHelp(); !strings.Contains(help, "\n  init ") {
		t.Errorf("root help lacks init:\n%s", help)
	}
}

func TestInitOutsideARepo(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", "/")
	code, _, errOut := runInit("--gather")
	if code != 2 || !strings.HasPrefix(errOut, "physa init: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitProposeWithGatherIsBadUsage(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, _, errOut := runInit("--propose", "--gather")
	if code != 2 || !strings.HasPrefix(errOut, "usage: physa init") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitProposeOnAMappedRepoIsRefused(t *testing.T) {
	dir := trailersRepo(t)
	useRepoStore(t, dir)
	f := &fakeAgent{}
	useAgent(t, "claude", f)
	code, out, errOut := runInit("--propose")
	if code != 2 || out != "" || f.starts != 0 ||
		!strings.Contains(errOut, "physa init: the repo already has a map") || !strings.Contains(errOut, "physa map learn") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}
