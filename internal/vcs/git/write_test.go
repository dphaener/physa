package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/vcs"
)

// isolate returns an environment that gives git an identity and no signing,
// from a global config of the test's own, so a Writer's commits never read
// the machine's config.
func isolate(t *testing.T) []string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[user]\n\tname = W\n\temail = w@example.com\n[commit]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return env("GIT_CONFIG_GLOBAL="+cfg, "GIT_CONFIG_NOSYSTEM=1")
}

// newWriter returns a Writer on a repo whose origin is a local bare remote
// holding main, the repo's dir, the remote's dir and the environment its git
// runs in.
func newWriter(t *testing.T) (vcs.Writer, string, string, []string) {
	t.Helper()
	e := isolate(t)
	bare := t.TempDir()
	run(t, e, bare, "init", "-q", "--bare", "-b", "main")
	dir := t.TempDir()
	run(t, e, dir, "init", "-q", "-b", "main")
	run(t, e, dir, "remote", "add", "origin", bare)
	writeFile(t, dir, "a.txt", "one\n")
	run(t, e, dir, "add", "a.txt")
	commit(t, e, dir, "feat: Base")
	run(t, e, dir, "push", "-q", "origin", "main")
	run(t, e, dir, "fetch", "-q", "origin")
	w, err := OpenWriter(t.Context(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	return w, dir, bare, e
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// unchanged fails the test at its end when the person's working copy, index
// or checked-out branch differ from how they are now.
func unchanged(t *testing.T, e []string, dir string) {
	t.Helper()
	before := state(t, e, dir)
	t.Cleanup(func() {
		if after := state(t, e, dir); after != before {
			t.Errorf("the working copy changed:\nbefore %s\nafter  %s", before, after)
		}
	})
}

func state(t *testing.T, e []string, dir string) string {
	return run(t, e, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=all") + "|" + run(t, e, dir, "rev-parse", "HEAD")
}

// sideChange commits body to name on a new branch off main, and returns to
// main; the id is the side commit.
func sideChange(t *testing.T, e []string, dir, branch, name, body, message string) string {
	t.Helper()
	run(t, e, dir, "checkout", "-q", "-b", branch, "main")
	writeFile(t, dir, name, body)
	run(t, e, dir, "add", name)
	id := commit(t, e, dir, message)
	run(t, e, dir, "checkout", "-q", "main")
	return id
}

func TestResolve(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	id := commit(t, e, dir, "feat: One")
	if got, ok := r.Resolve(t.Context(), "main"); !ok || got != id {
		t.Errorf("Resolve(main) = %q, %v, want %q", got, ok, id)
	}
	if got, ok := r.Resolve(t.Context(), id[:7]); !ok || got != id {
		t.Errorf("Resolve(short) = %q, %v", got, ok)
	}
	for _, ref := range []string{"nope", "", "--all"} {
		if got, ok := r.Resolve(t.Context(), ref); ok {
			t.Errorf("Resolve(%q) = %q, want false", ref, got)
		}
	}
}

func TestTrailer(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "feat: Old\n\nLearned-through: 1111111\nCapability: learning")
	want := commit(t, e, dir, "feat: Newer\n\nbody\n\nlearned-through: 2222222\nCapability: learning")
	commit(t, e, dir, "fix: Body only\n\nLearned-through: is written in this body\n\nand prose follows it.")
	commit(t, e, dir, "feat: Subject only Learned-through: 3333333")
	commit(t, e, dir, "chore: Nothing")
	got, ok, err := r.Trailer(t.Context(), "HEAD", "Learned-through")
	if err != nil || !ok || got.ID != want {
		t.Fatalf("Trailer = %+v, %v, %v, want %s", got, ok, err, want)
	}
	if got.Subject != "feat: Newer" {
		t.Errorf("subject = %q", got.Subject)
	}
	if _, ok, err := r.Trailer(t.Context(), "HEAD", "Learned"); err != nil || ok {
		t.Errorf("Learned = %v, %v, want none", ok, err)
	}
	if _, _, err := r.Trailer(t.Context(), "nope", "Learned-through"); err == nil {
		t.Error("an invalid ref is no error")
	}
	if _, _, err := r.Trailer(t.Context(), "HEAD", "Learned.*"); err == nil {
		t.Error("a key that is not a trailer key is no error")
	}
}

func TestTrailerSkipsMerges(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	base := commit(t, e, dir, "base")
	run(t, e, dir, "checkout", "-q", "-b", "side")
	commit(t, e, dir, "side")
	run(t, e, dir, "checkout", "-q", "main")
	commit(t, e, dir, "main work")
	run(t, e, dir, "merge", "-q", "--no-ff", "-m", "Merge side\n\nLearned-through: "+base, "side")
	if got, ok, err := r.Trailer(t.Context(), "HEAD", "Learned-through"); err != nil || ok {
		t.Errorf("Trailer = %+v, %v, %v, want the merge skipped", got, ok, err)
	}
}

func TestIsAncestor(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	first := commit(t, e, dir, "one")
	second := commit(t, e, dir, "two")
	run(t, e, dir, "checkout", "-q", "-b", "side", first)
	other := commit(t, e, dir, "other")
	cases := []struct {
		a, b string
		want bool
	}{
		{first, second, true}, {second, second, true}, {second, first, false},
		{other, second, false}, {first, other, true}, {"nope", second, false}, {"", second, false},
	}
	for _, c := range cases {
		if got := r.IsAncestor(t.Context(), c.a, c.b); got != c.want {
			t.Errorf("IsAncestor(%.7s, %.7s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestFetch(t *testing.T) {
	t.Parallel()
	w, dir, bare, e := newWriter(t)
	unchanged(t, e, dir)
	if err := w.Fetch(t.Context(), "main", "physa/learn"); err != nil {
		t.Fatalf("fetching an absent branch: %v", err)
	}
	if len(w.Branch(t.Context(), "physa/learn")) != 0 {
		t.Errorf("Branch = %v, want none", w.Branch(t.Context(), "physa/learn"))
	}
	main, _ := w.Resolve(t.Context(), "main")
	run(t, e, bare, "update-ref", "refs/heads/physa/learn", main)
	if err := w.Fetch(t.Context(), "physa/learn"); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Resolve(t.Context(), "refs/remotes/origin/physa/learn"); !ok || got != main {
		t.Errorf("origin/physa/learn = %q, %v, want %s", got, ok, main)
	}
	run(t, e, bare, "update-ref", "-d", "refs/heads/physa/learn")
	if err := w.Fetch(t.Context(), "physa/learn"); err != nil {
		t.Fatal(err)
	}
	if got, ok := w.Resolve(t.Context(), "refs/remotes/origin/physa/learn"); ok {
		t.Errorf("stale origin/physa/learn kept at %s", got)
	}
	if err := w.Fetch(t.Context(), "bad..name"); err == nil {
		t.Error("a bad branch name is no error")
	}
}

func TestCheckedOut(t *testing.T) {
	t.Parallel()
	w, dir, _, e := newWriter(t)
	if ok, err := w.CheckedOut(t.Context(), "main"); err != nil || !ok {
		t.Errorf("CheckedOut(main) = %v, %v, want true", ok, err)
	}
	if ok, err := w.CheckedOut(t.Context(), "physa/learn"); err != nil || ok {
		t.Errorf("CheckedOut(physa/learn) = %v, %v, want false", ok, err)
	}
	other := filepath.Join(t.TempDir(), "other")
	run(t, e, dir, "worktree", "add", "-q", "-b", "physa/learn", other)
	if ok, err := w.CheckedOut(t.Context(), "physa/learn"); err != nil || !ok {
		t.Errorf("CheckedOut in a linked worktree = %v, %v, want true", ok, err)
	}
}

func TestCopyLifecycle(t *testing.T) {
	t.Parallel()
	w, dir, _, e := newWriter(t)
	unchanged(t, e, dir)
	c, err := w.NewCopy(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	store, _ := w.StoreDir(t.Context())
	if rel, err := filepath.Rel(store, c.Dir()); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("copy %s is not under the store %s", c.Dir(), store)
	}
	if !strings.Contains(run(t, e, dir, "worktree", "list", "--porcelain"), c.Dir()) {
		t.Fatal("the copy is not a registered worktree")
	}
	if got := run(t, e, c.Dir(), "rev-parse", "--abbrev-ref", "HEAD"); got != "HEAD" {
		t.Errorf("the copy is on %q, want detached", got)
	}
	if _, err := os.Stat(filepath.Join(c.Dir(), "a.txt")); err != nil {
		t.Errorf("the copy lacks the base's files: %v", err)
	}
	if err := c.Remove(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(run(t, e, dir, "worktree", "list", "--porcelain"), c.Dir()) {
		t.Error("the copy is still registered")
	}
	if _, err := os.Stat(filepath.Dir(c.Dir())); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temp dir is left: %v", err)
	}
	if _, err := w.NewCopy(t.Context(), "nope"); err == nil {
		t.Error("a copy of no change is no error")
	}
}

func TestReplay(t *testing.T) {
	t.Parallel()
	w, dir, _, e := newWriter(t)
	x := sideChange(t, e, dir, "x", "a.txt", "two\n", "docs: Change a\n\nCapability: learning")
	unchanged(t, e, dir)
	c, err := w.NewCopy(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove(t.Context())
	ok, err := c.Replay(t.Context(), x)
	if err != nil || !ok {
		t.Fatalf("Replay = %v, %v, want true", ok, err)
	}
	if got := run(t, e, c.Dir(), "log", "-1", "--format=%B%an"); got != "docs: Change a\n\nCapability: learning\nT" {
		t.Errorf("replayed message and author = %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(c.Dir(), "a.txt")); string(b) != "two\n" {
		t.Errorf("a.txt = %q", b)
	}
	head := run(t, e, c.Dir(), "rev-parse", "HEAD")
	ok, err = c.Replay(t.Context(), x)
	if err != nil || ok {
		t.Fatalf("Replay of an applied change = %v, %v, want false", ok, err)
	}
	if got := run(t, e, c.Dir(), "rev-parse", "HEAD"); got != head {
		t.Error("an applied change recorded a change")
	}
	if got := run(t, e, c.Dir(), "status", "--porcelain"); got != "" {
		t.Errorf("the copy is not clean: %q", got)
	}
}

func TestReplayConflict(t *testing.T) {
	t.Parallel()
	w, dir, _, e := newWriter(t)
	x := sideChange(t, e, dir, "x", "a.txt", "two\n", "docs: Two")
	y := sideChange(t, e, dir, "y", "a.txt", "three\n", "docs: Three")
	unchanged(t, e, dir)
	c, err := w.NewCopy(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove(t.Context())
	if ok, err := c.Replay(t.Context(), x); err != nil || !ok {
		t.Fatalf("Replay(x) = %v, %v", ok, err)
	}
	head := run(t, e, c.Dir(), "rev-parse", "HEAD")
	ok, err := c.Replay(t.Context(), y)
	if !errors.Is(err, vcs.ErrConflict) || ok {
		t.Fatalf("Replay(y) = %v, %v, want ErrConflict", ok, err)
	}
	t.Logf("conflicting replay: %v", err)
	if got := run(t, e, c.Dir(), "rev-parse", "HEAD"); got != head {
		t.Error("the conflict moved the copy")
	}
	if got := run(t, e, c.Dir(), "status", "--porcelain"); got != "" {
		t.Errorf("the copy is not clean after the conflict: %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(c.Dir(), "a.txt")); string(b) != "two\n" {
		t.Errorf("a.txt = %q, want the copy's own", b)
	}
	t.Log("copy unchanged: HEAD " + head[:7] + ", clean")
}

func TestCommit(t *testing.T) {
	t.Parallel()
	w, dir, _, e := newWriter(t)
	unchanged(t, e, dir)
	c, err := w.NewCopy(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove(t.Context())
	base := run(t, e, c.Dir(), "rev-parse", "HEAD")
	msg := "docs(map): Learn from 0 changes\n\nNo proposals; the watermark advances.\n\nLearned-through: " + base + "\nCapability: learning\n"
	got, err := c.Commit(t.Context(), msg)
	if err != nil {
		t.Fatalf("an empty commit: %v", err)
	}
	if got.Subject != "docs(map): Learn from 0 changes" || got.ID != run(t, e, c.Dir(), "rev-parse", "HEAD") || got.ID == base {
		t.Errorf("change = %+v", got)
	}
	if found, ok, err := w.Trailer(t.Context(), got.ID, "Learned-through"); err != nil || !ok || found.ID != got.ID {
		t.Errorf("the trailer is not read back: %+v, %v, %v", found, ok, err)
	}
	writeFile(t, c.Dir(), "new.txt", "new\n")
	got, err = c.Commit(t.Context(), "docs: Add new")
	if err != nil {
		t.Fatal(err)
	}
	if files, _ := w.Files(t.Context(), got.ID, "."); strings.Join(files, ",") != "a.txt,new.txt" {
		t.Errorf("files = %v, want the new file staged", files)
	}
	if _, err := c.Commit(t.Context(), "  \n"); err == nil {
		t.Error("an empty message is no error")
	}
}

func TestSetBranchLease(t *testing.T) {
	t.Parallel()
	w, dir, _, e := newWriter(t)
	unchanged(t, e, dir)
	main, _ := w.Resolve(t.Context(), "main")
	if err := w.SetBranch(t.Context(), "physa/learn", main, ""); err != nil {
		t.Fatal(err)
	}
	c, err := w.NewCopy(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove(t.Context())
	next, err := c.Commit(t.Context(), "docs: Next")
	if err != nil {
		t.Fatal(err)
	}
	err = w.SetBranch(t.Context(), "physa/learn", next.ID, "")
	if !errors.Is(err, vcs.ErrLease) {
		t.Errorf("old \"\" on an existing branch = %v, want ErrLease", err)
	}
	t.Logf("must-not-exist: %v", err)
	err = w.SetBranch(t.Context(), "physa/learn", next.ID, next.ID)
	if !errors.Is(err, vcs.ErrLease) {
		t.Errorf("a wrong old = %v, want ErrLease", err)
	}
	t.Logf("wrong old: %v", err)
	if got, _ := w.Resolve(t.Context(), "physa/learn"); got != main {
		t.Errorf("a lost lease moved the branch to %s", got)
	}
	if err := w.SetBranch(t.Context(), "physa/learn", next.ID, main); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.Resolve(t.Context(), "physa/learn"); got != next.ID {
		t.Errorf("physa/learn = %s, want %s", got, next.ID)
	}
	if err := w.SetBranch(t.Context(), "main", next.ID, main); err == nil || errors.Is(err, vcs.ErrLease) {
		t.Errorf("moving the checked-out branch = %v, want refused", err)
	}
}

func TestPushLease(t *testing.T) {
	t.Parallel()
	w, dir, bare, e := newWriter(t)
	unchanged(t, e, dir)
	main, _ := w.Resolve(t.Context(), "main")
	if err := w.Push(t.Context(), "physa/learn", main, ""); err != nil {
		t.Fatal(err)
	}
	if got := run(t, e, bare, "rev-parse", "refs/heads/physa/learn"); got != main {
		t.Fatalf("remote physa/learn = %s, want %s", got, main)
	}
	c, err := w.NewCopy(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove(t.Context())
	next, err := c.Commit(t.Context(), "docs: Next")
	if err != nil {
		t.Fatal(err)
	}
	err = w.Push(t.Context(), "physa/learn", next.ID, "")
	if !errors.Is(err, vcs.ErrLease) {
		t.Errorf("expect \"\" on an existing remote branch = %v, want ErrLease", err)
	}
	t.Logf("must-not-exist: %v", err)
	err = w.Push(t.Context(), "physa/learn", next.ID, next.ID)
	if !errors.Is(err, vcs.ErrLease) {
		t.Errorf("a stale lease = %v, want ErrLease", err)
	}
	t.Logf("stale lease: %v", err)
	if got := run(t, e, bare, "rev-parse", "refs/heads/physa/learn"); got != main {
		t.Errorf("a stale lease moved the remote to %s", got)
	}
	t.Log("remote unchanged: physa/learn " + main[:7])
	if err := w.Push(t.Context(), "physa/learn", next.ID, main); err != nil {
		t.Fatal(err)
	}
	if got := run(t, e, bare, "rev-parse", "refs/heads/physa/learn"); got != next.ID {
		t.Errorf("remote physa/learn = %s, want %s", got, next.ID)
	}
	if err := w.Push(t.Context(), "physa/learn", next.ID, "--all"); err == nil {
		t.Error("an expect that is no change id is no error")
	}
}

func TestCopySkipsHooks(t *testing.T) {
	t.Parallel()
	_, dir, _, base := newWriter(t)
	e := with(base, "CLAUDE_CODE_SESSION_ID=")
	run(t, e, dir, "config", "--local", "core.hooksPath", shippedHooks(t))
	x := sideChange(t, e, dir, "x", "a.txt", "two\n", "docs: Change a\n\nCapability: learning")
	unchanged(t, e, dir)
	e = with(base, "CLAUDE_CODE_SESSION_ID=s1")
	w, err := OpenWriter(t.Context(), dir, e) // the Writer runs git in the session too
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.NewCopy(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Remove(t.Context())
	run(t, e, c.Dir(), "commit", "-q", "--allow-empty", "-m", "docs: Control\n\nCapability: learning")
	if got := message(t, e, c.Dir(), "HEAD"); !strings.HasSuffix(got, "\nSession: s1") {
		t.Fatalf("the hook is not active in the copy, so the test proves nothing: %q", got)
	}
	if ok, err := c.Replay(t.Context(), x); err != nil || !ok {
		t.Fatalf("Replay = %v, %v", ok, err)
	}
	if got := message(t, e, c.Dir(), "HEAD"); got != "docs: Change a\n\nCapability: learning" {
		t.Errorf("a replayed message = %q, want it as written", got)
	}
	got, err := c.Commit(t.Context(), "docs(map): Learn from 1 change\n\nCapability: learning\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Message != "docs(map): Learn from 1 change\n\nCapability: learning\n" || strings.Contains(message(t, e, c.Dir(), "HEAD"), "Session:") {
		t.Errorf("a committed message = %q, want it as written", got.Message)
	}
}

func TestFetchCancel(t *testing.T) {
	t.Parallel()
	w, _, _, _ := newWriter(t)
	if err := w.Fetch(cancelled(t), "main"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestPushCancel(t *testing.T) {
	t.Parallel()
	w, dir, _, e := newWriter(t)
	head := run(t, e, dir, "rev-parse", "HEAD")
	err := w.Push(cancelled(t), "learn", head, "")
	if !errors.Is(err, context.Canceled) || errors.Is(err, vcs.ErrLease) {
		t.Fatalf("err = %v, want context.Canceled and not ErrLease", err)
	}
}

func TestSetBranchCancel(t *testing.T) {
	t.Parallel()
	w, dir, _, e := newWriter(t)
	head := run(t, e, dir, "rev-parse", "HEAD")
	err := w.SetBranch(cancelled(t), "learn", head, head)
	if !errors.Is(err, context.Canceled) || errors.Is(err, vcs.ErrLease) {
		t.Fatalf("err = %v, want context.Canceled and not ErrLease", err)
	}
}
