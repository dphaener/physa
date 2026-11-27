package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/vcs"
)

// env is the test process's environment with kv after it, so the last of a
// name wins, as for any child started with it. It is a new slice each call.
func env(kv ...string) []string { return slices.Concat(os.Environ(), kv) }

// with is e with kv after it, in a new slice.
func with(e []string, kv ...string) []string { return slices.Concat(e, kv) }

// run runs git in dir with the environment e.
func run(t *testing.T, e []string, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = e
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo returns a new repo, its dir and the environment its git runs in.
func newRepo(t *testing.T) (vcs.Repo, string, []string) {
	t.Helper()
	dir, e := t.TempDir(), env()
	run(t, e, dir, "init", "-q", "-b", "main")
	r, err := Open(t.Context(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir, e
}

func commit(t *testing.T, e []string, dir, message string) string {
	t.Helper()
	run(t, e, dir, "commit", "-q", "--allow-empty", "-m", message)
	return run(t, e, dir, "rev-parse", "HEAD")
}

func TestOpenNotRepo(t *testing.T) {
	t.Parallel()
	_, err := Open(t.Context(), t.TempDir(), env())
	if !errors.Is(err, vcs.ErrNotRepo) {
		t.Fatalf("err = %v, want ErrNotRepo", err)
	}
}

func TestChanges(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	first := commit(t, e, dir, "feat: First\n\nbody one\n\nCapability: map")
	second := commit(t, e, dir, "fix: Second")
	got, err := r.Changes(t.Context(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != second || got[1].ID != first {
		t.Fatalf("changes = %+v, want newest first", got)
	}
	if got[1].Subject != "feat: First" || !strings.Contains(got[1].Message, "Capability: map") {
		t.Errorf("change = %+v", got[1])
	}
	if !strings.HasPrefix(first, got[1].Short) || got[1].Short == "" {
		t.Errorf("short %q is not a prefix of %q", got[1].Short, first)
	}
	one, err := r.Changes(t.Context(), first+".."+second)
	if err != nil || len(one) != 1 || one[0].ID != second {
		t.Errorf("range = %+v, %v", one, err)
	}
}

func TestChangesSkipsMerges(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "base")
	run(t, e, dir, "checkout", "-q", "-b", "side")
	commit(t, e, dir, "side")
	run(t, e, dir, "checkout", "-q", "main")
	commit(t, e, dir, "main work")
	run(t, e, dir, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	got, err := r.Changes(t.Context(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Subject == "merge side" {
			t.Errorf("merge change listed: %+v", c)
		}
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestChangesBadRange(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	_, err := r.Changes(t.Context(), "nope..HEAD")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want git's own message naming the range", err)
	}
}

func TestHasChange(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	id := commit(t, e, dir, "one")
	if !r.HasChange(t.Context(), id) || !r.HasChange(t.Context(), id[:8]) {
		t.Error("full and abbreviated id should name the change")
	}
	if r.HasChange(t.Context(), "") || r.HasChange(t.Context(), "0123456789abcdef0123456789abcdef01234567") || r.HasChange(t.Context(), "nope") {
		t.Error("empty or unknown id should not name a change")
	}
}

func TestPartial(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	commit(t, e, dir, "two")
	if r.Partial(t.Context()) {
		t.Error("a full repo is not partial")
	}
	clone := filepath.Join(t.TempDir(), "clone")
	run(t, e, dir, "clone", "-q", "--depth", "1", "file://"+dir, clone)
	c, err := Open(t.Context(), clone, e)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Partial(t.Context()) {
		t.Error("a shallow clone is partial")
	}
}

func TestStoreDir(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	got, err := r.StoreDir(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	gitDir, _ := filepath.EvalSymlinks(filepath.Join(dir, ".git"))
	want := filepath.Join(gitDir, "physa")
	if g, _ := filepath.EvalSymlinks(filepath.Dir(got)); filepath.Join(g, "physa") != want {
		t.Errorf("StoreDir = %q, want %q", got, want)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	run(t, e, dir, "worktree", "add", "-q", "-b", "other", wt)
	w, err := Open(t.Context(), wt, e)
	if err != nil {
		t.Fatal(err)
	}
	wgot, err := w.StoreDir(t.Context())
	if err != nil || wgot != got {
		t.Errorf("worktree StoreDir = %q, %v; want %q", wgot, err, got)
	}
}

func TestFiles(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	before := commit(t, e, dir, "empty")
	if got, err := r.Files(t.Context(), before, "d"); err != nil || len(got) != 0 {
		t.Fatalf("absent dir: %v, %v", got, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "d", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"d/a.md", "d/sub/deep.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, e, dir, "add", ".")
	after := commit(t, e, dir, "add")
	got, err := r.Files(t.Context(), after, "d")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"d/a.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Files = %v, want %v", got, want)
	}
	if got, _ := r.Files(t.Context(), before, "d"); len(got) != 0 {
		t.Errorf("before the change: %v", got)
	}
	if _, err := r.Files(t.Context(), "nope", "d"); err == nil {
		t.Error("unknown change should error")
	}
}

func TestListed(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	write := func(p, body string) {
		t.Helper()
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "ignored.log\n")
	write("tracked.md", "t")
	run(t, e, dir, "add", ".")
	commit(t, e, dir, "tracked")
	write("untracked.md", "u")
	write("sub/deep.md", "d")
	write("ignored.log", "i")
	lister, ok := r.(vcs.Lister)
	if !ok {
		t.Fatal("git repo should implement vcs.Lister")
	}
	got, err := lister.Listed(t.Context())
	want := []string{".gitignore", "sub/deep.md", "tracked.md", "untracked.md"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Listed = %v, %v; want %v", got, err, want)
	}
	for _, p := range got {
		if p == "ignored.log" || strings.HasPrefix(p, ".git/") {
			t.Errorf("Listed holds %q, which must be absent", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ignored.log")); err != nil {
		t.Errorf("the ignored file should exist on disk: %v", err)
	}
	sub := filepath.Join(dir, "sub")
	fromSub, err := Open(t.Context(), sub, e)
	if err != nil {
		t.Fatal(err)
	}
	gotSub, err := fromSub.(vcs.Lister).Listed(t.Context())
	if err != nil || !reflect.DeepEqual(gotSub, want) {
		t.Errorf("Listed from a subdirectory = %v, %v; want the root's %v", gotSub, err, want)
	}
}

func TestTracked(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	write := func(p, body string) {
		t.Helper()
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitattributes", "third_party/** linguist-vendored\n")
	write("main.go", "m")
	write("third_party/lib/x.go", "v")
	write("gen/.gitattributes", "*.pb.go linguist-generated=true\nkeep.pb.go -linguist-generated\n")
	write("gen/a.pb.go", "g")
	write("gen/keep.pb.go", "k")
	write("dir with space/my file.go", "s")
	write("both/.gitattributes", "* linguist-vendored linguist-generated\n")
	write("both/z.txt", "b")
	run(t, e, dir, "add", ".")
	commit(t, e, dir, "tracked")
	write("untracked.go", "u")
	tracker, ok := r.(vcs.Tracker)
	if !ok {
		t.Fatal("git repo should implement vcs.Tracker")
	}
	want := []vcs.TrackedFile{
		{Path: ".gitattributes"},
		{Path: "both/.gitattributes", Vendored: true, Generated: true},
		{Path: "both/z.txt", Vendored: true, Generated: true},
		{Path: "dir with space/my file.go"},
		{Path: "gen/.gitattributes"},
		{Path: "gen/a.pb.go", Generated: true},
		{Path: "gen/keep.pb.go"},
		{Path: "main.go"},
		{Path: "third_party/lib/x.go", Vendored: true},
	}
	got, err := tracker.Tracked(t.Context())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Tracked = %v, %v; want %v", got, err, want)
	}
	fromSub, err := Open(t.Context(), filepath.Join(dir, "gen"), e)
	if err != nil {
		t.Fatal(err)
	}
	gotSub, err := fromSub.(vcs.Tracker).Tracked(t.Context())
	if err != nil || !reflect.DeepEqual(gotSub, want) {
		t.Errorf("Tracked from a subdirectory = %v, %v; want the root's %v", gotSub, err, want)
	}
}

func TestTrackedEmpty(t *testing.T) {
	t.Parallel()
	r, _, _ := newRepo(t)
	got, err := r.(vcs.Tracker).Tracked(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("Tracked = %v, %v; want none", got, err)
	}
}

func TestTrackedNotRepo(t *testing.T) {
	t.Parallel()
	r := &repo{dir: t.TempDir(), env: env()}
	got, err := r.Tracked(t.Context())
	t.Logf("Tracked in a folder that is no repo: %v", err)
	if err == nil || got != nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("Tracked = %v, %v; want an error naming git's message", got, err)
	}
}

func TestChangeTime(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	commit(t, e, dir, "two")
	got, err := r.Changes(t.Context(), "HEAD")
	if err != nil || len(got) != 2 {
		t.Fatalf("%v, %v", got, err)
	}
	if got[0].Time.IsZero() || got[1].Time.IsZero() || got[0].Time.Before(got[1].Time) {
		t.Errorf("times = %v, %v; want set, newest first", got[0].Time, got[1].Time)
	}
	if time.Since(got[0].Time) > time.Hour {
		t.Errorf("time %v is not recent", got[0].Time)
	}
}

func TestLatest(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	commit(t, e, dir, "two")
	three := commit(t, e, dir, "three")
	got, err := r.Latest(t.Context(), "HEAD", 2)
	if err != nil || len(got) != 2 || got[0].ID != three || got[1].Subject != "two" {
		t.Fatalf("Latest(2) = %+v, %v", got, err)
	}
	if got[0].Time.IsZero() {
		t.Error("time not set")
	}
	all, err := r.Latest(t.Context(), "HEAD", 10)
	if err != nil || len(all) != 3 {
		t.Errorf("Latest(10) = %d changes, %v; want all 3", len(all), err)
	}
}

func TestLatestBadRef(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	_, err := r.Latest(t.Context(), "nope", 5)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want git's own message naming the ref", err)
	}
}

func TestReverts(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, e, dir, "add", ".")
	target := commit(t, e, dir, "feat: Thing")
	run(t, e, dir, "revert", "--no-edit", "HEAD")
	got, err := r.Latest(t.Context(), "HEAD", 1)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if id, ok := r.Reverts(got[0]); !ok || id != target {
		t.Errorf("Reverts = %q, %v; want %q", id, ok, target)
	}
	run(t, e, dir, "revert", "--no-edit", "HEAD")
	again, _ := r.Latest(t.Context(), "HEAD", 1)
	if id, ok := r.Reverts(again[0]); !ok || id != got[0].ID {
		t.Errorf("revert of a revert = %q, %v; want %q", id, ok, got[0].ID)
	}
}

func TestRevertsMessages(t *testing.T) {
	t.Parallel()
	r, _, _ := newRepo(t)
	cases := []struct {
		name, msg, id string
		ok            bool
	}{
		{"short id", "Revert it\n\nThis reverts commit abc1234.", "abc1234", true},
		{"no line", "feat: Thing\n\nbody", "", false},
		{"too short", "This reverts commit abc12.", "", false},
		{"quoted then real", "Quote\n\nThis reverts commit aaaaaaa. is what git writes.\n\nThis reverts commit bbbbbbb.", "bbbbbbb", true},
		{"reversing clause", "Revert\n\nThis reverts commit ccccccc, reversing\nchanges made to ddddddd.", "ccccccc", true},
		{"mid line", "we say This reverts commit eeeeeee. here", "", false},
	}
	for _, c := range cases {
		id, ok := r.Reverts(vcs.Change{Message: c.msg})
		if id != c.id || ok != c.ok {
			t.Errorf("%s: Reverts = %q, %v; want %q, %v", c.name, id, ok, c.id, c.ok)
		}
	}
}

func TestDefaultBranch(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	if got, err := r.DefaultBranch(t.Context()); err != nil || got != "main" {
		t.Errorf("no remote: %q, %v; want main", got, err)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	run(t, e, dir, "clone", "-q", dir, clone)
	c, err := Open(t.Context(), clone, e)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.DefaultBranch(t.Context()); err != nil || got != "origin/main" {
		t.Errorf("clone: %q, %v; want origin/main", got, err)
	}
	// Without origin/HEAD the remote's main still wins.
	run(t, e, clone, "remote", "set-head", "origin", "--delete")
	if got, err := c.DefaultBranch(t.Context()); err != nil || got != "origin/main" {
		t.Errorf("clone without HEAD: %q, %v; want origin/main", got, err)
	}
}

func TestDefaultBranchMissing(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	run(t, e, dir, "checkout", "-q", "-b", "trunk")
	commit(t, e, dir, "one")
	got, err := r.DefaultBranch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "default branch cannot be found") {
		t.Fatalf("DefaultBranch = %q, %v; want an error", got, err)
	}
}

func TestBranch(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	if got := r.Branch(t.Context(), "physa/x"); len(got) != 0 {
		t.Errorf("neither: %v", got)
	}
	remote := t.TempDir()
	run(t, e, remote, "init", "-q", "--bare", "-b", "main")
	run(t, e, dir, "remote", "add", "origin", remote)
	run(t, e, dir, "branch", "physa/x")
	if got := r.Branch(t.Context(), "physa/x"); !reflect.DeepEqual(got, []string{"physa/x"}) {
		t.Errorf("local only: %v", got)
	}
	run(t, e, dir, "push", "-q", "origin", "physa/x")
	run(t, e, dir, "fetch", "-q", "origin")
	if got := r.Branch(t.Context(), "physa/x"); !reflect.DeepEqual(got, []string{"physa/x", "origin/physa/x"}) {
		t.Errorf("both: %v", got)
	}
	run(t, e, dir, "branch", "-q", "-D", "physa/x")
	if got := r.Branch(t.Context(), "physa/x"); !reflect.DeepEqual(got, []string{"origin/physa/x"}) {
		t.Errorf("origin only: %v", got)
	}
}

func TestChangeAuthor(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	for _, name := range []string{"Dörte van Berg", "李 雷"} {
		cmd := exec.Command("git", "-C", dir, "-c", "user.name="+name, "-c", "user.email=a@example.com",
			"-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "by "+name+"\n\nbody")
		cmd.Env = e
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}
		got, err := r.Latest(t.Context(), "HEAD", 1)
		if err != nil || len(got) != 1 {
			t.Fatalf("Latest = %+v, %v", got, err)
		}
		if got[0].Author != name {
			t.Errorf("Author = %q, want %q", got[0].Author, name)
		}
		if got[0].Message != "by "+name+"\n\nbody\n" || got[0].Subject != "by "+name {
			t.Errorf("message changed: %q / %q", got[0].Subject, got[0].Message)
		}
	}
}

func commitAt(t *testing.T, e []string, dir, message, when string) string {
	t.Helper()
	return commit(t, with(e, "GIT_COMMITTER_DATE="+when), dir, message)
}

func TestSince(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commitAt(t, e, dir, "old", "2024-01-01T00:00:00Z")
	newer := commitAt(t, e, dir, "new", "2024-03-01T00:00:00Z")
	cut := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	got, err := r.Since(t.Context(), "HEAD", cut)
	if err != nil || len(got) != 1 || got[0].ID != newer {
		t.Fatalf("since = %+v, %v", got, err)
	}
}

func TestSinceZeroTime(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	first := commitAt(t, e, dir, "old", "2001-01-01T00:00:00Z")
	second := commitAt(t, e, dir, "new", "2024-03-01T00:00:00Z")
	got, err := r.Since(t.Context(), "HEAD", time.Time{})
	if err != nil || len(got) != 2 || got[0].ID != second || got[1].ID != first {
		t.Fatalf("since = %+v, %v", got, err)
	}
}

func TestSinceBadRef(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	if got, err := r.Since(t.Context(), "nope", time.Time{}); err == nil {
		t.Fatalf("since = %+v, want an error", got)
	}
}

func TestAlteredRootCommit(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, e, dir, "add", ".")
	id := commit(t, e, dir, "root")
	got, err := r.Altered(t.Context(), id)
	if err != nil || !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("altered = %v, %v", got, err)
	}
}

func TestAlteredSubdirectories(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "base")
	for _, p := range []string{"z/deep/b.go", "a/c.go"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, e, dir, "add", ".")
	id := commit(t, e, dir, "two files")
	got, err := r.Altered(t.Context(), id)
	if err != nil || !reflect.DeepEqual(got, []string{"a/c.go", "z/deep/b.go"}) {
		t.Fatalf("altered = %v, %v", got, err)
	}
	empty, err := r.Altered(t.Context(), commit(t, e, dir, "empty"))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty commit altered = %#v, %v", empty, err)
	}
}

func TestAlteredBadID(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "one")
	if got, err := r.Altered(t.Context(), "deadbeefdeadbeef"); err == nil {
		t.Fatalf("altered = %v, want an error", got)
	}
}

func TestEmail(t *testing.T) {
	t.Parallel()
	_, dir, e := newRepo(t)
	e = with(e, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	got, err := Email(t.Context(), dir, e)
	if err != nil || got != "" {
		t.Errorf("unset: %q, %v", got, err)
	}
	run(t, e, dir, "config", "--local", "user.email", "Darin.H@example.com")
	if got, err = Email(t.Context(), dir, e); err != nil || got != "Darin.H@example.com" {
		t.Errorf("set: %q, %v", got, err)
	}
}

// TestEnvReachesGit shows that the environment passed in is the one git
// runs in: a global config named only there gives Email its address.
func TestEnvReachesGit(t *testing.T) {
	t.Parallel()
	_, dir, e := newRepo(t)
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[user]\n\temail = passed@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Email(t.Context(), dir, with(e, "GIT_CONFIG_GLOBAL="+cfg, "GIT_CONFIG_NOSYSTEM=1"))
	if err != nil || got != "passed@example.com" {
		t.Errorf("Email = %q, %v; want the address from the passed GIT_CONFIG_GLOBAL", got, err)
	}
}

// TestEmptyEnvIsRefused shows that git never runs with no environment, which
// would leave it no HOME or PATH, whichever way it is opened.
func TestEmptyEnvIsRefused(t *testing.T) {
	t.Parallel()
	_, dir, _ := newRepo(t)
	for _, e := range [][]string{nil, {}} {
		if _, err := Open(t.Context(), dir, e); !errors.Is(err, errNoEnv) || errors.Is(err, vcs.ErrNotRepo) {
			t.Errorf("Open(%#v) = %v, want the empty environment refused", e, err)
		}
		if _, err := OpenWriter(t.Context(), dir, e); !errors.Is(err, errNoEnv) {
			t.Errorf("OpenWriter(%#v) = %v, want the empty environment refused", e, err)
		}
		if _, err := OpenHooks(t.Context(), dir, e); !errors.Is(err, errNoEnv) {
			t.Errorf("OpenHooks(%#v) = %v, want the empty environment refused", e, err)
		}
		if got, err := Email(t.Context(), dir, e); !errors.Is(err, errNoEnv) || got != "" {
			t.Errorf("Email(%#v) = %q, %v, want the empty environment refused", e, got, err)
		}
	}
}

// TestEnvIsNotAppendedInPlace shows that git's own LC_ALL=C goes in a copy:
// the caller's slice, with room to grow, is left as it was.
func TestEnvIsNotAppendedInPlace(t *testing.T) {
	t.Parallel()
	_, dir, e := newRepo(t)
	roomy := append(make([]string, 0, len(e)+4), e...)
	if _, err := Email(t.Context(), dir, roomy); err != nil {
		t.Fatal(err)
	}
	if spare := roomy[len(roomy):cap(roomy)]; slices.ContainsFunc(spare, func(kv string) bool { return kv != "" }) {
		t.Errorf("the caller's slice was written past its length: %q", spare)
	}
}

// mergeHistory is a made-up repo that lands PRs as merge commits: PR #3
// adds two notes files on a branch, PR #4 adds a tag file on a branch that
// first merges main into itself, and a stable branch is merged without a PR.
// It returns the repo, its dir and the merge of each PR.
func mergeHistory(t *testing.T) (vcs.Repo, string, map[int]string) {
	t.Helper()
	r, dir, base := newRepo(t)
	e, day := base, 0
	at := func() { day++; e = with(base, fmt.Sprintf("GIT_COMMITTER_DATE=2024-01-%02dT00:00:00Z", day)) }
	file := func(p string) {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, e, dir, "add", ".")
		at()
		commit(t, e, dir, "Add "+p)
	}
	merge := func(branch, message string) string {
		at()
		run(t, e, dir, "merge", "-q", "--no-ff", "-m", message, branch)
		return run(t, e, dir, "rev-parse", "HEAD")
	}
	file("README.md")
	run(t, e, dir, "checkout", "-q", "-b", "notes")
	file("notes/model.rb")
	file("notes/view.rb")
	run(t, e, dir, "checkout", "-q", "main")
	file("CHANGELOG.md")
	prs := map[int]string{3: merge("notes", "Merge pull request #3 from dana/notes\n\nKeep notes")}
	run(t, e, dir, "checkout", "-q", "-b", "tags")
	file("tags/tag.rb")
	merge("main", "Merge branch 'main' into tags")
	run(t, e, dir, "checkout", "-q", "-b", "stable", "main")
	file("stable.txt")
	run(t, e, dir, "checkout", "-q", "main")
	prs[4] = merge("tags", "Merge pull request #4 from sam/tags")
	merge("stable", "Merge branch 'stable'")
	return r, dir, prs
}

func TestMergesFollowTheFirstParent(t *testing.T) {
	t.Parallel()
	r, _, prs := mergeHistory(t)
	got, err := r.(vcs.Merges).Merges(t.Context(), "main", time.Time{})
	var subjects []string
	for _, c := range got {
		subjects = append(subjects, c.Subject)
	}
	if want := []string{"Merge branch 'stable'", "Merge pull request #4 from sam/tags", "Merge pull request #3 from dana/notes"}; err != nil || !reflect.DeepEqual(subjects, want) {
		t.Fatalf("merges %v, %v", subjects, err)
	}
	for n, files := range map[int][]string{3: {"notes/model.rb", "notes/view.rb"}, 4: {"tags/tag.rb"}} {
		if got, err := r.Altered(t.Context(), prs[n]); err != nil || !reflect.DeepEqual(got, files) {
			t.Errorf("#%d's merge altered %v, %v; want %v", n, got, err, files)
		}
	}
}

func TestSinceWithPRMergesAddsOnlyPRMerges(t *testing.T) {
	t.Parallel()
	r, _, prs := mergeHistory(t)
	got, err := vcs.SinceWithPRMerges(t.Context(), r, "main", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for i, c := range got {
		subjects = append(subjects, c.Subject)
		if i > 0 && c.Time.After(got[i-1].Time) {
			t.Errorf("not newest first at %d: %v", i, subjects)
		}
	}
	want := []string{"Merge pull request #4 from sam/tags", "Add stable.txt", "Add tags/tag.rb", "Merge pull request #3 from dana/notes",
		"Add CHANGELOG.md", "Add notes/view.rb", "Add notes/model.rb", "Add README.md"}
	if !reflect.DeepEqual(subjects, want) || got[3].ID != prs[3] {
		t.Errorf("got %v\nwant %v", subjects, want)
	}
}

// cancelled returns a context that is already cancelled.
func cancelled(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

func TestChangesCancel(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "feat: First")
	if _, err := r.Changes(cancelled(t), "HEAD"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDefaultBranchCancel(t *testing.T) {
	t.Parallel()
	r, dir, e := newRepo(t)
	commit(t, e, dir, "feat: First")
	if _, err := r.DefaultBranch(cancelled(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestOpenCancel(t *testing.T) {
	t.Parallel()
	_, err := Open(cancelled(t), t.TempDir(), env())
	if !errors.Is(err, context.Canceled) || errors.Is(err, vcs.ErrNotRepo) {
		t.Fatalf("err = %v, want context.Canceled and not ErrNotRepo", err)
	}
}
