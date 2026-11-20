package history

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// newRepo makes a git repo in a temp dir, isolated from the user's git config.
func newRepo(t *testing.T) (*Repo, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

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

func commit(t *testing.T, dir, message string) string {
	t.Helper()
	run(t, dir, "commit", "-q", "--allow-empty", "-m", message)
	return run(t, dir, "rev-parse", "HEAD")
}

func TestReadTrailers(t *testing.T) {
	tests := []struct {
		name, msg string
		caps, ld  []string
	}{
		{"single block", "feat: x\n\nBody text.\n\nCapability: slices\nLoaded: map, slices\n", []string{"slices"}, []string{"map", "slices"}},
		{"squash merge", "feat(map): Add x (#10)\n\nBody.\n\nCapability: x\n\nCo-authored-by: A <a@b.c>\nCo-authored-by: B <b@b.c>\n", []string{"x"}, []string{}},
		{"in body text", "feat: x\n\nCapability: x\nmore text after it\n\nNothing here.\n", []string{}, []string{}},
		{"subject only", "feat: x\n", []string{}, []string{}},
		{"subject looks like trailer", "Capability: x\n", []string{}, []string{}},
		{"two loaded lines", "feat: x\n\nCapability: a\nLoaded: a, b\nloaded: c,, d\n", []string{"a"}, []string{"a", "b", "c", "d"}},
		{"continuation", "feat: x\n\nLoaded: a,\n  b\nCapability: a\n", []string{"a"}, []string{"a", "b"}},
		{"case insensitive", "feat: x\n\nCAPABILITY: a\n", []string{"a"}, []string{}},
		{"two capabilities", "feat: x\n\nCapability: a\nCapability: b\n", []string{"a", "b"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps, ld := readTrailers(tt.msg)
			if !reflect.DeepEqual(caps, tt.caps) || !reflect.DeepEqual(ld, tt.ld) {
				t.Errorf("got %v %v, want %v %v", caps, ld, tt.caps, tt.ld)
			}
		})
	}
}

func TestLogReadsTrailers(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "chore: base")
	commit(t, dir, "feat: a\n\nBody.\n\nCapability: slices\n\nCo-authored-by: X <x@y.z>")
	commit(t, dir, "feat: b\n\nCapability: map\nLoaded: map, slices")
	commit(t, dir, "feat: c")
	got, err := r.Log("HEAD~3..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d commits, want 3", len(got))
	}
	if got[0].Subject != "feat: c" || len(got[0].Capabilities) != 0 {
		t.Errorf("newest commit wrong: %+v", got[0])
	}
	if !reflect.DeepEqual(got[1].Capabilities, []string{"map"}) || !reflect.DeepEqual(got[1].Loaded, []string{"map", "slices"}) {
		t.Errorf("commit b wrong: %+v", got[1])
	}
	if !reflect.DeepEqual(got[2].Capabilities, []string{"slices"}) || got[2].Subject != "feat: a" {
		t.Errorf("squash-shaped commit wrong: %+v", got[2])
	}
	if len(got[2].Hash) != 40 || !strings.HasPrefix(got[2].Hash, got[2].Short) {
		t.Errorf("hashes wrong: %+v", got[2])
	}
}

func TestLogBadRange(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "chore: base")
	_, err := r.Log("nosuch..HEAD")
	if err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("want git's error naming the range, got %v", err)
	}
}

func TestOpenNotRepo(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	if _, err := Open(t.TempDir()); !errors.Is(err, ErrNotRepo) {
		t.Errorf("want ErrNotRepo, got %v", err)
	}
}

func TestHasCommit(t *testing.T) {
	r, dir := newRepo(t)
	full := commit(t, dir, "chore: base")
	for _, h := range []string{full, full[:7]} {
		if !r.HasCommit(h) {
			t.Errorf("HasCommit(%q) = false", h)
		}
	}
	for _, h := range []string{"deadbee", "", "--all"} {
		if r.HasCommit(h) {
			t.Errorf("HasCommit(%q) = true", h)
		}
	}
	if r.Shallow() {
		t.Error("a fresh repo is not shallow")
	}
}

func TestCheck(t *testing.T) {
	known := func(s string) bool { return s == "slices" || s == "map" }
	commits := []Commit{
		{Short: "a1", Capabilities: []string{"slices"}, Loaded: []string{"slices", "map"}},
		{Short: "b2"},
		{Short: "c3", Capabilities: []string{"slices", "nope"}, Loaded: []string{"gone"}},
		{Short: "d4", Capabilities: []string{"slices.md"}},
	}
	got := Check(commits, known)
	type fk struct{ commit, kind, slug string }
	var kinds []fk
	for _, f := range got {
		kinds = append(kinds, fk{f.Commit, f.Kind, f.Slug})
	}
	want := []fk{
		{"b2", KindMissing, ""},
		{"c3", KindMultiple, ""},
		{"c3", KindUnknown, "nope"},
		{"c3", KindUnknown, "gone"},
		{"d4", KindUnknown, "slices.md"},
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("got %v, want %v", kinds, want)
	}
	if !strings.Contains(got[3].Message, "Loaded") || !strings.Contains(got[2].Message, "Capability") {
		t.Errorf("messages should name the trailer: %q, %q", got[2].Message, got[3].Message)
	}
	if got := Check(nil, known); got == nil || len(got) != 0 {
		t.Errorf("no commits should give an empty, non-nil list, got %v", got)
	}
}
