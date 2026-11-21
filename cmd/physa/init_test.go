package main

import (
	"bytes"
	"encoding/json"
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
	if code != 0 || errOut != "" {
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

func TestInitGatherReplaces(t *testing.T) {
	dir := trailersRepo(t)
	useRepoStore(t, dir)
	useForge(t, mergedFake{prs: []forge.PR{{Number: 3, MergedAt: time.Now()}, {Number: 4, MergedAt: time.Now()}}})
	if code, _, errOut := runInit("--gather"); code != 0 {
		t.Fatalf("first run: %d %q", code, errOut)
	}
	if n := len(storedGather(t, dir).PRs); n != 2 {
		t.Fatalf("first run stored %d PRs", n)
	}
	useForge(t, mergedFake{prs: []forge.PR{{Number: 9, MergedAt: time.Now()}}})
	code, out, errOut := runInit("--gather")
	if code != 0 || !strings.Contains(out, "Gathered into") {
		t.Fatalf("second run: %d %q %q", code, out, errOut)
	}
	g := storedGather(t, dir)
	if len(g.PRs) != 1 || g.PRs[0].Number != 9 {
		t.Errorf("second run left %+v", g.PRs)
	}
}

func TestInitBareSaysWhatIsBuilt(t *testing.T) {
	t.Chdir(trailersRepo(t))
	code, out, errOut := runInit()
	if code != 2 || out != "" || !strings.Contains(errOut, "physa init: only --gather is built yet") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
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
	if code != 0 || errOut != "" || !strings.HasPrefix(out, "usage: physa init --gather") {
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
