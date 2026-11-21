package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/vcs"
)

// isolate gives git an identity and no signing, from a global config of the
// test's own, so a Writer's commits never read the machine's config.
func isolate(t *testing.T) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[user]\n\tname = W\n\temail = w@example.com\n[commit]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// newWriter returns a Writer on a repo whose origin is a local bare remote
// holding main, the repo's dir and the remote's dir.
func newWriter(t *testing.T) (vcs.Writer, string, string) {
	t.Helper()
	isolate(t)
	bare := t.TempDir()
	run(t, bare, "init", "-q", "--bare", "-b", "main")
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "remote", "add", "origin", bare)
	writeFile(t, dir, "a.txt", "one\n")
	run(t, dir, "add", "a.txt")
	commit(t, dir, "feat: Base")
	run(t, dir, "push", "-q", "origin", "main")
	run(t, dir, "fetch", "-q", "origin")
	w, err := OpenWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	return w, dir, bare
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// unchanged fails the test at its end when the person's working copy, index
// or checked-out branch differ from how they are now.
func unchanged(t *testing.T, dir string) {
	t.Helper()
	before := state(t, dir)
	t.Cleanup(func() {
		if after := state(t, dir); after != before {
			t.Errorf("the working copy changed:\nbefore %s\nafter  %s", before, after)
		}
	})
}

func state(t *testing.T, dir string) string {
	return run(t, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=all") + "|" + run(t, dir, "rev-parse", "HEAD")
}

// sideChange commits body to name on a new branch off main, and returns to
// main; the id is the side commit.
func sideChange(t *testing.T, dir, branch, name, body, message string) string {
	t.Helper()
	run(t, dir, "checkout", "-q", "-b", branch, "main")
	writeFile(t, dir, name, body)
	run(t, dir, "add", name)
	id := commit(t, dir, message)
	run(t, dir, "checkout", "-q", "main")
	return id
}

func TestResolve(t *testing.T) {
	r, dir := newRepo(t)
	id := commit(t, dir, "feat: One")
	if got, ok := r.Resolve("main"); !ok || got != id {
		t.Errorf("Resolve(main) = %q, %v, want %q", got, ok, id)
	}
	if got, ok := r.Resolve(id[:7]); !ok || got != id {
		t.Errorf("Resolve(short) = %q, %v", got, ok)
	}
	for _, ref := range []string{"nope", "", "--all"} {
		if got, ok := r.Resolve(ref); ok {
			t.Errorf("Resolve(%q) = %q, want false", ref, got)
		}
	}
}

func TestTrailer(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "feat: Old\n\nLearned-through: 1111111\nCapability: learning")
	want := commit(t, dir, "feat: Newer\n\nbody\n\nlearned-through: 2222222\nCapability: learning")
	commit(t, dir, "fix: Body only\n\nLearned-through: is written in this body\n\nand prose follows it.")
	commit(t, dir, "feat: Subject only Learned-through: 3333333")
	commit(t, dir, "chore: Nothing")
	got, ok, err := r.Trailer("HEAD", "Learned-through")
	if err != nil || !ok || got.ID != want {
		t.Fatalf("Trailer = %+v, %v, %v, want %s", got, ok, err, want)
	}
	if got.Subject != "feat: Newer" {
		t.Errorf("subject = %q", got.Subject)
	}
	if _, ok, err := r.Trailer("HEAD", "Learned"); err != nil || ok {
		t.Errorf("Learned = %v, %v, want none", ok, err)
	}
	if _, _, err := r.Trailer("nope", "Learned-through"); err == nil {
		t.Error("an invalid ref is no error")
	}
	if _, _, err := r.Trailer("HEAD", "Learned.*"); err == nil {
		t.Error("a key that is not a trailer key is no error")
	}
}

func TestTrailerSkipsMerges(t *testing.T) {
	r, dir := newRepo(t)
	base := commit(t, dir, "base")
	run(t, dir, "checkout", "-q", "-b", "side")
	commit(t, dir, "side")
	run(t, dir, "checkout", "-q", "main")
	commit(t, dir, "main work")
	run(t, dir, "merge", "-q", "--no-ff", "-m", "Merge side\n\nLearned-through: "+base, "side")
	if got, ok, err := r.Trailer("HEAD", "Learned-through"); err != nil || ok {
		t.Errorf("Trailer = %+v, %v, %v, want the merge skipped", got, ok, err)
	}
}

func TestIsAncestor(t *testing.T) {
	r, dir := newRepo(t)
	first := commit(t, dir, "one")
	second := commit(t, dir, "two")
	run(t, dir, "checkout", "-q", "-b", "side", first)
	other := commit(t, dir, "other")
	cases := []struct {
		a, b string
		want bool
	}{
		{first, second, true}, {second, second, true}, {second, first, false},
		{other, second, false}, {first, other, true}, {"nope", second, false}, {"", second, false},
	}
	for _, c := range cases {
		if got := r.IsAncestor(c.a, c.b); got != c.want {
			t.Errorf("IsAncestor(%.7s, %.7s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestFetch(t *testing.T) {
	w, dir, bare := newWriter(t)
	unchanged(t, dir)
	if err := w.Fetch("main", "physa/learn"); err != nil {
		t.Fatalf("fetching an absent branch: %v", err)
	}
	if len(w.Branch("physa/learn")) != 0 {
		t.Errorf("Branch = %v, want none", w.Branch("physa/learn"))
	}
	main, _ := w.Resolve("main")
	run(t, bare, "update-ref", "refs/heads/physa/learn", main)
	if err := w.Fetch("physa/learn"); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Resolve("refs/remotes/origin/physa/learn"); !ok || got != main {
		t.Errorf("origin/physa/learn = %q, %v, want %s", got, ok, main)
	}
	run(t, bare, "update-ref", "-d", "refs/heads/physa/learn")
	if err := w.Fetch("physa/learn"); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Resolve("refs/remotes/origin/physa/learn"); ok {
		t.Errorf("stale origin/physa/learn kept at %s", got)
	}
	if err := w.Fetch("bad..name"); err == nil {
		t.Error("a bad branch name is no error")
	}
}

func TestCheckedOut(t *testing.T) {
	w, dir, _ := newWriter(t)
	if ok, err := w.CheckedOut("main"); err != nil || !ok {
		t.Errorf("CheckedOut(main) = %v, %v, want true", ok, err)
	}
	if ok, err := w.CheckedOut("physa/learn"); err != nil || ok {
		t.Errorf("CheckedOut(physa/learn) = %v, %v, want false", ok, err)
	}
	other := filepath.Join(t.TempDir(), "other")
	run(t, dir, "worktree", "add", "-q", "-b", "physa/learn", other)
	if ok, err := w.CheckedOut("physa/learn"); err != nil || !ok {
		t.Errorf("CheckedOut in a linked worktree = %v, %v, want true", ok, err)
	}
}

func TestCopyLifecycle(t *testing.T) {
	w, dir, _ := newWriter(t)
	unchanged(t, dir)
	c, err := w.NewCopy("main")
	if err != nil {
		t.Fatal(err)
	}
	store, _ := w.StoreDir()
	if rel, err := filepath.Rel(store, c.Dir()); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("copy %s is not under the store %s", c.Dir(), store)
	}
	if !strings.Contains(run(t, dir, "worktree", "list", "--porcelain"), c.Dir()) {
		t.Fatal("the copy is not a registered worktree")
	}
	if got := run(t, c.Dir(), "rev-parse", "--abbrev-ref", "HEAD"); got != "HEAD" {
		t.Errorf("the copy is on %q, want detached", got)
	}
	if _, err := os.Stat(filepath.Join(c.Dir(), "a.txt")); err != nil {
		t.Errorf("the copy lacks the base's files: %v", err)
	}
	if err := c.Remove(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(run(t, dir, "worktree", "list", "--porcelain"), c.Dir()) {
		t.Error("the copy is still registered")
	}
	if _, err := os.Stat(filepath.Dir(c.Dir())); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temp dir is left: %v", err)
	}
	if _, err := w.NewCopy("nope"); err == nil {
		t.Error("a copy of no change is no error")
	}
}

func TestReplay(t *testing.T) {
	w, dir, _ := newWriter(t)
	x := sideChange(t, dir, "x", "a.txt", "two\n", "docs: Change a\n\nCapability: learning")
	unchanged(t, dir)
	c, err := w.NewCopy("main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove()
	ok, err := c.Replay(x)
	if err != nil || !ok {
		t.Fatalf("Replay = %v, %v, want true", ok, err)
	}
	if got := run(t, c.Dir(), "log", "-1", "--format=%B%an"); got != "docs: Change a\n\nCapability: learning\nT" {
		t.Errorf("replayed message and author = %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(c.Dir(), "a.txt")); string(b) != "two\n" {
		t.Errorf("a.txt = %q", b)
	}
	head := run(t, c.Dir(), "rev-parse", "HEAD")
	ok, err = c.Replay(x)
	if err != nil || ok {
		t.Fatalf("Replay of an applied change = %v, %v, want false", ok, err)
	}
	if got := run(t, c.Dir(), "rev-parse", "HEAD"); got != head {
		t.Error("an applied change recorded a change")
	}
	if got := run(t, c.Dir(), "status", "--porcelain"); got != "" {
		t.Errorf("the copy is not clean: %q", got)
	}
}

func TestReplayConflict(t *testing.T) {
	w, dir, _ := newWriter(t)
	x := sideChange(t, dir, "x", "a.txt", "two\n", "docs: Two")
	y := sideChange(t, dir, "y", "a.txt", "three\n", "docs: Three")
	unchanged(t, dir)
	c, err := w.NewCopy("main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove()
	if ok, err := c.Replay(x); err != nil || !ok {
		t.Fatalf("Replay(x) = %v, %v", ok, err)
	}
	head := run(t, c.Dir(), "rev-parse", "HEAD")
	ok, err := c.Replay(y)
	if !errors.Is(err, vcs.ErrConflict) || ok {
		t.Fatalf("Replay(y) = %v, %v, want ErrConflict", ok, err)
	}
	t.Logf("conflicting replay: %v", err)
	if got := run(t, c.Dir(), "rev-parse", "HEAD"); got != head {
		t.Error("the conflict moved the copy")
	}
	if got := run(t, c.Dir(), "status", "--porcelain"); got != "" {
		t.Errorf("the copy is not clean after the conflict: %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(c.Dir(), "a.txt")); string(b) != "two\n" {
		t.Errorf("a.txt = %q, want the copy's own", b)
	}
	t.Log("copy unchanged: HEAD " + head[:7] + ", clean")
}

func TestCommit(t *testing.T) {
	w, dir, _ := newWriter(t)
	unchanged(t, dir)
	c, err := w.NewCopy("main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove()
	base := run(t, c.Dir(), "rev-parse", "HEAD")
	msg := "docs(map): Learn from 0 changes\n\nNo proposals; the watermark advances.\n\nLearned-through: " + base + "\nCapability: learning\n"
	got, err := c.Commit(msg)
	if err != nil {
		t.Fatalf("an empty commit: %v", err)
	}
	if got.Subject != "docs(map): Learn from 0 changes" || got.ID != run(t, c.Dir(), "rev-parse", "HEAD") || got.ID == base {
		t.Errorf("change = %+v", got)
	}
	if found, ok, err := w.Trailer(got.ID, "Learned-through"); err != nil || !ok || found.ID != got.ID {
		t.Errorf("the trailer is not read back: %+v, %v, %v", found, ok, err)
	}
	writeFile(t, c.Dir(), "new.txt", "new\n")
	got, err = c.Commit("docs: Add new")
	if err != nil {
		t.Fatal(err)
	}
	if files, _ := w.Files(got.ID, "."); strings.Join(files, ",") != "a.txt,new.txt" {
		t.Errorf("files = %v, want the new file staged", files)
	}
	if _, err := c.Commit("  \n"); err == nil {
		t.Error("an empty message is no error")
	}
}

func TestSetBranchLease(t *testing.T) {
	w, dir, _ := newWriter(t)
	unchanged(t, dir)
	main, _ := w.Resolve("main")
	if err := w.SetBranch("physa/learn", main, ""); err != nil {
		t.Fatal(err)
	}
	c, err := w.NewCopy("main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove()
	next, err := c.Commit("docs: Next")
	if err != nil {
		t.Fatal(err)
	}
	err = w.SetBranch("physa/learn", next.ID, "")
	if !errors.Is(err, vcs.ErrLease) {
		t.Errorf("old \"\" on an existing branch = %v, want ErrLease", err)
	}
	t.Logf("must-not-exist: %v", err)
	err = w.SetBranch("physa/learn", next.ID, next.ID)
	if !errors.Is(err, vcs.ErrLease) {
		t.Errorf("a wrong old = %v, want ErrLease", err)
	}
	t.Logf("wrong old: %v", err)
	if got, _ := w.Resolve("physa/learn"); got != main {
		t.Errorf("a lost lease moved the branch to %s", got)
	}
	if err := w.SetBranch("physa/learn", next.ID, main); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.Resolve("physa/learn"); got != next.ID {
		t.Errorf("physa/learn = %s, want %s", got, next.ID)
	}
	if err := w.SetBranch("main", next.ID, main); err == nil || errors.Is(err, vcs.ErrLease) {
		t.Errorf("moving the checked-out branch = %v, want refused", err)
	}
}

func TestPushLease(t *testing.T) {
	w, dir, bare := newWriter(t)
	unchanged(t, dir)
	main, _ := w.Resolve("main")
	if err := w.Push("physa/learn", main, ""); err != nil {
		t.Fatal(err)
	}
	if got := run(t, bare, "rev-parse", "refs/heads/physa/learn"); got != main {
		t.Fatalf("remote physa/learn = %s, want %s", got, main)
	}
	c, err := w.NewCopy("main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove()
	next, err := c.Commit("docs: Next")
	if err != nil {
		t.Fatal(err)
	}
	err = w.Push("physa/learn", next.ID, "")
	if !errors.Is(err, vcs.ErrLease) {
		t.Errorf("expect \"\" on an existing remote branch = %v, want ErrLease", err)
	}
	t.Logf("must-not-exist: %v", err)
	err = w.Push("physa/learn", next.ID, next.ID)
	if !errors.Is(err, vcs.ErrLease) {
		t.Errorf("a stale lease = %v, want ErrLease", err)
	}
	t.Logf("stale lease: %v", err)
	if got := run(t, bare, "rev-parse", "refs/heads/physa/learn"); got != main {
		t.Errorf("a stale lease moved the remote to %s", got)
	}
	t.Log("remote unchanged: physa/learn " + main[:7])
	if err := w.Push("physa/learn", next.ID, main); err != nil {
		t.Fatal(err)
	}
	if got := run(t, bare, "rev-parse", "refs/heads/physa/learn"); got != next.ID {
		t.Errorf("remote physa/learn = %s, want %s", got, next.ID)
	}
	if err := w.Push("physa/learn", next.ID, "--all"); err == nil {
		t.Error("an expect that is no change id is no error")
	}
}
