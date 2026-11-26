package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestListed(t *testing.T) {
	r, dir := newRepo(t)
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
	run(t, dir, "add", ".")
	commit(t, dir, "tracked")
	write("untracked.md", "u")
	write("sub/deep.md", "d")
	write("ignored.log", "i")
	lister, ok := r.(vcs.Lister)
	if !ok {
		t.Fatal("git repo should implement vcs.Lister")
	}
	got, err := lister.Listed()
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
	fromSub, err := Open(sub)
	if err != nil {
		t.Fatal(err)
	}
	gotSub, err := fromSub.(vcs.Lister).Listed()
	if err != nil || !reflect.DeepEqual(gotSub, want) {
		t.Errorf("Listed from a subdirectory = %v, %v; want the root's %v", gotSub, err, want)
	}
}

func TestChangeTime(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "one")
	commit(t, dir, "two")
	got, err := r.Changes("HEAD")
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
	r, dir := newRepo(t)
	commit(t, dir, "one")
	commit(t, dir, "two")
	three := commit(t, dir, "three")
	got, err := r.Latest("HEAD", 2)
	if err != nil || len(got) != 2 || got[0].ID != three || got[1].Subject != "two" {
		t.Fatalf("Latest(2) = %+v, %v", got, err)
	}
	if got[0].Time.IsZero() {
		t.Error("time not set")
	}
	all, err := r.Latest("HEAD", 10)
	if err != nil || len(all) != 3 {
		t.Errorf("Latest(10) = %d changes, %v; want all 3", len(all), err)
	}
}

func TestLatestBadRef(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "one")
	_, err := r.Latest("nope", 5)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want git's own message naming the ref", err)
	}
}

func TestReverts(t *testing.T) {
	r, dir := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	target := commit(t, dir, "feat: Thing")
	run(t, dir, "revert", "--no-edit", "HEAD")
	got, err := r.Latest("HEAD", 1)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if id, ok := r.Reverts(got[0]); !ok || id != target {
		t.Errorf("Reverts = %q, %v; want %q", id, ok, target)
	}
	run(t, dir, "revert", "--no-edit", "HEAD")
	again, _ := r.Latest("HEAD", 1)
	if id, ok := r.Reverts(again[0]); !ok || id != got[0].ID {
		t.Errorf("revert of a revert = %q, %v; want %q", id, ok, got[0].ID)
	}
}

func TestRevertsMessages(t *testing.T) {
	r, _ := newRepo(t)
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
	r, dir := newRepo(t)
	commit(t, dir, "one")
	if got, err := r.DefaultBranch(); err != nil || got != "main" {
		t.Errorf("no remote: %q, %v; want main", got, err)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	run(t, dir, "clone", "-q", dir, clone)
	c, err := Open(clone)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.DefaultBranch(); err != nil || got != "origin/main" {
		t.Errorf("clone: %q, %v; want origin/main", got, err)
	}
	// Without origin/HEAD the remote's main still wins.
	run(t, clone, "remote", "set-head", "origin", "--delete")
	if got, err := c.DefaultBranch(); err != nil || got != "origin/main" {
		t.Errorf("clone without HEAD: %q, %v; want origin/main", got, err)
	}
}

func TestDefaultBranchMissing(t *testing.T) {
	r, dir := newRepo(t)
	run(t, dir, "checkout", "-q", "-b", "trunk")
	commit(t, dir, "one")
	got, err := r.DefaultBranch()
	if err == nil || !strings.Contains(err.Error(), "default branch cannot be found") {
		t.Fatalf("DefaultBranch = %q, %v; want an error", got, err)
	}
}

func TestBranch(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "one")
	if got := r.Branch("physa/x"); len(got) != 0 {
		t.Errorf("neither: %v", got)
	}
	remote := t.TempDir()
	run(t, remote, "init", "-q", "--bare", "-b", "main")
	run(t, dir, "remote", "add", "origin", remote)
	run(t, dir, "branch", "physa/x")
	if got := r.Branch("physa/x"); !reflect.DeepEqual(got, []string{"physa/x"}) {
		t.Errorf("local only: %v", got)
	}
	run(t, dir, "push", "-q", "origin", "physa/x")
	run(t, dir, "fetch", "-q", "origin")
	if got := r.Branch("physa/x"); !reflect.DeepEqual(got, []string{"physa/x", "origin/physa/x"}) {
		t.Errorf("both: %v", got)
	}
	run(t, dir, "branch", "-q", "-D", "physa/x")
	if got := r.Branch("physa/x"); !reflect.DeepEqual(got, []string{"origin/physa/x"}) {
		t.Errorf("origin only: %v", got)
	}
}

func TestChangeAuthor(t *testing.T) {
	r, dir := newRepo(t)
	for _, name := range []string{"Dörte van Berg", "李 雷"} {
		cmd := exec.Command("git", "-C", dir, "-c", "user.name="+name, "-c", "user.email=a@example.com",
			"-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "by "+name+"\n\nbody")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}
		got, err := r.Latest("HEAD", 1)
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

func commitAt(t *testing.T, dir, message, when string) string {
	t.Helper()
	t.Setenv("GIT_COMMITTER_DATE", when)
	return commit(t, dir, message)
}

func TestSince(t *testing.T) {
	r, dir := newRepo(t)
	commitAt(t, dir, "old", "2024-01-01T00:00:00Z")
	newer := commitAt(t, dir, "new", "2024-03-01T00:00:00Z")
	cut := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	got, err := r.Since("HEAD", cut)
	if err != nil || len(got) != 1 || got[0].ID != newer {
		t.Fatalf("since = %+v, %v", got, err)
	}
}

func TestSinceZeroTime(t *testing.T) {
	r, dir := newRepo(t)
	first := commitAt(t, dir, "old", "2001-01-01T00:00:00Z")
	second := commitAt(t, dir, "new", "2024-03-01T00:00:00Z")
	got, err := r.Since("HEAD", time.Time{})
	if err != nil || len(got) != 2 || got[0].ID != second || got[1].ID != first {
		t.Fatalf("since = %+v, %v", got, err)
	}
}

func TestSinceBadRef(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "one")
	if got, err := r.Since("nope", time.Time{}); err == nil {
		t.Fatalf("since = %+v, want an error", got)
	}
}

func TestAlteredRootCommit(t *testing.T) {
	r, dir := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	id := commit(t, dir, "root")
	got, err := r.Altered(id)
	if err != nil || !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("altered = %v, %v", got, err)
	}
}

func TestAlteredSubdirectories(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "base")
	for _, p := range []string{"z/deep/b.go", "a/c.go"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "add", ".")
	id := commit(t, dir, "two files")
	got, err := r.Altered(id)
	if err != nil || !reflect.DeepEqual(got, []string{"a/c.go", "z/deep/b.go"}) {
		t.Fatalf("altered = %v, %v", got, err)
	}
	empty, err := r.Altered(commit(t, dir, "empty"))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty commit altered = %#v, %v", empty, err)
	}
}

func TestAlteredBadID(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "one")
	if got, err := r.Altered("deadbeefdeadbeef"); err == nil {
		t.Fatalf("altered = %v, want an error", got)
	}
}

func TestEmail(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	_, dir := newRepo(t)
	got, err := Email(dir)
	if err != nil || got != "" {
		t.Errorf("unset: %q, %v", got, err)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "--local", "user.email", "Darin.H@example.com").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, err = Email(dir); err != nil || got != "Darin.H@example.com" {
		t.Errorf("set: %q, %v", got, err)
	}
}

// mergeHistory is a made-up repo that lands PRs as merge commits: PR #3
// adds two notes files on a branch, PR #4 adds a tag file on a branch that
// first merges main into itself, and a stable branch is merged without a PR.
// It returns the repo, its dir and the merge of each PR.
func mergeHistory(t *testing.T) (vcs.Repo, string, map[int]string) {
	t.Helper()
	r, dir := newRepo(t)
	day := 0
	at := func() { day++; t.Setenv("GIT_COMMITTER_DATE", fmt.Sprintf("2024-01-%02dT00:00:00Z", day)) }
	file := func(p string) {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, dir, "add", ".")
		at()
		commit(t, dir, "Add "+p)
	}
	merge := func(branch, message string) string {
		at()
		run(t, dir, "merge", "-q", "--no-ff", "-m", message, branch)
		return run(t, dir, "rev-parse", "HEAD")
	}
	file("README.md")
	run(t, dir, "checkout", "-q", "-b", "notes")
	file("notes/model.rb")
	file("notes/view.rb")
	run(t, dir, "checkout", "-q", "main")
	file("CHANGELOG.md")
	prs := map[int]string{3: merge("notes", "Merge pull request #3 from dana/notes\n\nKeep notes")}
	run(t, dir, "checkout", "-q", "-b", "tags")
	file("tags/tag.rb")
	merge("main", "Merge branch 'main' into tags")
	run(t, dir, "checkout", "-q", "-b", "stable", "main")
	file("stable.txt")
	run(t, dir, "checkout", "-q", "main")
	prs[4] = merge("tags", "Merge pull request #4 from sam/tags")
	merge("stable", "Merge branch 'stable'")
	return r, dir, prs
}

func TestMergesFollowTheFirstParent(t *testing.T) {
	r, _, prs := mergeHistory(t)
	got, err := r.(vcs.Merges).Merges("main", time.Time{})
	var subjects []string
	for _, c := range got {
		subjects = append(subjects, c.Subject)
	}
	if want := []string{"Merge branch 'stable'", "Merge pull request #4 from sam/tags", "Merge pull request #3 from dana/notes"}; err != nil || !reflect.DeepEqual(subjects, want) {
		t.Fatalf("merges %v, %v", subjects, err)
	}
	for n, files := range map[int][]string{3: {"notes/model.rb", "notes/view.rb"}, 4: {"tags/tag.rb"}} {
		if got, err := r.Altered(prs[n]); err != nil || !reflect.DeepEqual(got, files) {
			t.Errorf("#%d's merge altered %v, %v; want %v", n, got, err, files)
		}
	}
}

func TestSinceWithPRMergesAddsOnlyPRMerges(t *testing.T) {
	r, _, prs := mergeHistory(t)
	got, err := vcs.SinceWithPRMerges(r, "main", time.Time{})
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
