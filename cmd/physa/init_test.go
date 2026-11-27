package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/adapter/claude"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/skillstub"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// mergedFake is a forge that lists the merged PRs it was given.
type mergedFake struct{ prs []forge.PR }

func (mergedFake) Name() string                                { return "fake" }
func (mergedFake) PRExists(context.Context, int) (bool, error) { return false, nil }
func (mergedFake) ChangePR(context.Context, string) (forge.PR, bool, error) {
	return forge.PR{}, false, nil
}
func (m mergedFake) MergedPRs(context.Context, time.Time, int) ([]forge.PR, bool, error) {
	return m.prs, false, nil
}

// useForge makes f the forge of d, whatever the repo.
func useForge(d *deps, f forge.Forge) {
	d.newForge = func(string) forge.Forge { return f }
}

// useRepoStore makes init write to the store of the repo in dir, as it does
// outside the tests, which keep off the real store, and moves the test there.
func useRepoStore(t *testing.T, d *deps, dir string) {
	t.Helper()
	d.openStore = gitStore
	t.Chdir(dir)
}

func runInit(d deps, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), d, append([]string{"init"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func storedGather(t *testing.T, dir string) store.Gather {
	t.Helper()
	repo, err := git.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := repo.StoreDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), sd)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g, ok, err := st.Gathered(context.Background())
	if err != nil || !ok {
		t.Fatalf("gathered: ok %v, err %v", ok, err)
	}
	return g
}

func TestInitGatherJSON(t *testing.T) {
	d := testDeps(t)
	dir := trailersRepo(t)
	write(t, dir, "AGENTS.md", "rules\n")
	write(t, dir, "README.md", "hello\n")
	useRepoStore(t, &d, dir)
	useForge(&d, mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}})
	code, out, errOut := runInit(d, "--gather", "--json")
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
	d := testDeps(t)
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
	useRepoStore(t, &d, dir)
	useForge(&d, mergedFake{})
	f := &fakeAgent{}
	useAgent(&d, "claude", f)
	code, out, errOut := runInit(d, "--answers", answers)
	if code != 0 || f.starts != 1 || !strings.Contains(out, "A new codebase") || // discovery's session alone
		!strings.Contains(out, "Lathe, promising: Lathe turns sketches into cut lists.") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
	// The spend cache is kept machine-wide, in the settings directory.
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "physa", "spend.json")); err != nil {
		t.Errorf("no spend cache in the settings directory: %v", err)
	}
}

func TestInitPushWithoutWriteIsBadUsage(t *testing.T) {
	d := testDeps(t)
	t.Chdir(trailersRepo(t))
	code, _, errOut := runInit(d, "--push")
	if code != 2 || !strings.HasPrefix(errOut, "usage: physa init") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitBadSinceIsBadUsage(t *testing.T) {
	d := testDeps(t)
	t.Chdir(trailersRepo(t))
	code, _, errOut := runInit(d, "--gather", "--since", "yesterday")
	if code != 2 || !strings.HasPrefix(errOut, "usage: physa init") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitHelp(t *testing.T) {
	t.Parallel()
	d := testDeps(t)
	code, out, errOut := runInit(d, "--help")
	if code != 0 || errOut != "" || !strings.HasPrefix(out, "usage: physa init [--write") || strings.Contains(out, "--appetite") || !strings.Contains(out, "physa init --gather") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestHelpListsInit(t *testing.T) {
	t.Parallel()
	if help := rootHelp(t); !strings.Contains(help, "\n  init ") {
		t.Errorf("root help lacks init:\n%s", help)
	}
}

func TestInitOutsideARepo(t *testing.T) {
	d := testDeps(t)
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", "/")
	code, _, errOut := runInit(d, "--gather")
	if code != 2 || !strings.HasPrefix(errOut, "physa init: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitProposeWithGatherIsBadUsage(t *testing.T) {
	d := testDeps(t)
	t.Chdir(trailersRepo(t))
	code, _, errOut := runInit(d, "--propose", "--gather")
	if code != 2 || !strings.HasPrefix(errOut, "usage: physa init") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitProposeOnAMappedRepoIsRefused(t *testing.T) {
	d := testDeps(t)
	dir := trailersRepo(t)
	useRepoStore(t, &d, dir)
	f := &fakeAgent{}
	useAgent(&d, "claude", f)
	code, out, errOut := runInit(d, "--propose")
	if code != 2 || out != "" || f.starts != 0 ||
		!strings.Contains(errOut, "physa init: the repo already has a map") || !strings.Contains(errOut, "physa map learn") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

// claudeFake is a fake agent that answers to Claude Code's name, so init
// looks for Claude Code's skill.
type claudeFake struct{ *fakeAgent }

func (claudeFake) Name() string { return claude.Name }

// skillInit runs a new codebase's init with --write in a temp repo, with
// HOME and CLAUDE_CONFIG_DIR in temp directories, after setup has prepared
// the home, and returns the exit code, stdout, stderr and the home.
func skillInit(t *testing.T, setup func(home string)) (int, string, string) {
	t.Helper()
	d := testDeps(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
	setup(home)
	dir := filepath.Join(t.TempDir(), "lathe")
	write(t, dir, "README.md", "# Lathe\n")
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "more")
	answers := filepath.Join(t.TempDir(), "answers.txt")
	write(t, filepath.Dir(answers), "answers.txt", "2: Lathe turns sketches into cut lists.\n")
	useRepoStore(t, &d, dir)
	useForge(&d, mergedFake{})
	d.agents["claude"] = func() adapter.Adapter { return claudeFake{&fakeAgent{}} }
	return runInit(d, "--answers", answers, "--write")
}

func TestInitSaysWhenTheSkillIsMissing(t *testing.T) {
	code, out, errOut := skillInit(t, func(string) {})
	line := "Claude Code has no Physa skill on this account: run `physa skill setup`"
	if code != 0 || strings.Count(errOut, "physa skill setup") != 1 || !strings.Contains(errOut, "physa init: "+line) {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "## Physa's skill\n\n"+line) {
		t.Errorf("stdout has no skill section: %q", out)
	}
}

func TestInitSaysNothingWhenTheSkillIsCurrent(t *testing.T) {
	code, out, errOut := skillInit(t, func(home string) {
		if err := skillstub.Write(claude.SkillFile(home, "physa")); err != nil {
			t.Fatal(err)
		}
	})
	if code != 0 || strings.Contains(out+errOut, "physa skill") || strings.Contains(out, "Physa's skill") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestInitSaysNothingWhenTheSkillCannotBeRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	code, out, errOut := skillInit(t, func(home string) {
		path := claude.SkillFile(home, "physa")
		if err := skillstub.Write(path); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Dir(path)
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o755) })
	})
	if code != 0 || strings.Contains(out+errOut, "physa skill") || strings.Contains(out, "Physa's skill") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}
