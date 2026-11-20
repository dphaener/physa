package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/vcs"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) (vcs.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func commit(t *testing.T, dir, message string) string {
	t.Helper()
	run(t, dir, "commit", "-q", "--allow-empty", "-m", message)
	return run(t, dir, "rev-parse", "HEAD")
}

func TestOpenNotRepo(t *testing.T) {
	_, err := Open(t.TempDir())
	if !errors.Is(err, vcs.ErrNotRepo) {
		t.Fatalf("err = %v, want ErrNotRepo", err)
	}
}

func TestChanges(t *testing.T) {
	r, dir := newRepo(t)
	first := commit(t, dir, "feat: First\n\nbody one\n\nCapability: map")
	second := commit(t, dir, "fix: Second")
	got, err := r.Changes("HEAD")
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
	one, err := r.Changes(first + ".." + second)
	if err != nil || len(one) != 1 || one[0].ID != second {
		t.Errorf("range = %+v, %v", one, err)
	}
}

func TestChangesSkipsMerges(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "base")
	run(t, dir, "checkout", "-q", "-b", "side")
	commit(t, dir, "side")
	run(t, dir, "checkout", "-q", "main")
	commit(t, dir, "main work")
	run(t, dir, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	got, err := r.Changes("HEAD")
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
	r, dir := newRepo(t)
	commit(t, dir, "one")
	_, err := r.Changes("nope..HEAD")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want git's own message naming the range", err)
	}
}

func TestHasChange(t *testing.T) {
	r, dir := newRepo(t)
	id := commit(t, dir, "one")
	if !r.HasChange(id) || !r.HasChange(id[:8]) {
		t.Error("full and abbreviated id should name the change")
	}
	if r.HasChange("") || r.HasChange("0123456789abcdef0123456789abcdef01234567") || r.HasChange("nope") {
		t.Error("empty or unknown id should not name a change")
	}
}

func TestPartial(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "one")
	commit(t, dir, "two")
	if r.Partial() {
		t.Error("a full repo is not partial")
	}
	clone := filepath.Join(t.TempDir(), "clone")
	run(t, dir, "clone", "-q", "--depth", "1", "file://"+dir, clone)
	c, err := Open(clone)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Partial() {
		t.Error("a shallow clone is partial")
	}
}

func TestStoreDir(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "one")
	got, err := r.StoreDir()
	if err != nil {
		t.Fatal(err)
	}
	gitDir, _ := filepath.EvalSymlinks(filepath.Join(dir, ".git"))
	want := filepath.Join(gitDir, "physa")
	if g, _ := filepath.EvalSymlinks(filepath.Dir(got)); filepath.Join(g, "physa") != want {
		t.Errorf("StoreDir = %q, want %q", got, want)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	run(t, dir, "worktree", "add", "-q", "-b", "other", wt)
	w, err := Open(wt)
	if err != nil {
		t.Fatal(err)
	}
	wgot, err := w.StoreDir()
	if err != nil || wgot != got {
		t.Errorf("worktree StoreDir = %q, %v; want %q", wgot, err, got)
	}
}

func TestFiles(t *testing.T) {
	r, dir := newRepo(t)
	before := commit(t, dir, "empty")
	if got, err := r.Files(before, "d"); err != nil || len(got) != 0 {
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
	run(t, dir, "add", ".")
	after := commit(t, dir, "add")
	got, err := r.Files(after, "d")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"d/a.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Files = %v, want %v", got, want)
	}
	if got, _ := r.Files(before, "d"); len(got) != 0 {
		t.Errorf("before the change: %v", got)
	}
	if _, err := r.Files("nope", "d"); err == nil {
		t.Error("unknown change should error")
	}
}
