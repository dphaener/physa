package history

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// newRepo makes a git repo in a temp dir, isolated from the user's git config.
func newRepo(t *testing.T) (vcs.Repo, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	r, err := git.Open(context.Background(), dir)
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

// addCapability writes the old-layout capability file of slug and commits it with message.
func addCapability(t *testing.T, dir, slug, message string) {
	t.Helper()
	p := filepath.Join(dir, ".physa", "capabilities", slug+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\npromise: x\n---\n# "+slug+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".physa", "product.md"), []byte("# Product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", message)
}

func TestReadTrailers(t *testing.T) {
	tests := []struct {
		name, msg string
		caps      []string
	}{
		{"single block", "feat: x\n\nBody text.\n\nCapability: slices\n", []string{"slices"}},
		{"squash merge", "feat(map): Add x (#10)\n\nBody.\n\nCapability: x\n\nCo-authored-by: A <a@b.c>\nCo-authored-by: B <b@b.c>\n", []string{"x"}},
		{"in body text", "feat: x\n\nCapability: x\nmore text after it\n\nNothing here.\n", []string{}},
		{"subject only", "feat: x\n", []string{}},
		{"subject looks like trailer", "Capability: x\n", []string{}},
		{"loaded is ignored", "feat: x\n\nCapability: a\nLoaded: a, b\n", []string{"a"}},
		{"continuation", "feat: x\n\nCapability: a,\n  b\n", []string{"a", "b"}},
		{"case insensitive", "feat: x\n\nCAPABILITY: a\n", []string{"a"}},
		{"two capabilities", "feat: x\n\nCapability: a\nCapability: b,, c\n", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readTrailers(tt.msg); !reflect.DeepEqual(got, tt.caps) {
				t.Errorf("got %v, want %v", got, tt.caps)
			}
		})
	}
}

func TestTrailers(t *testing.T) {
	tests := []struct {
		name, msg, key string
		values         []string
	}{
		{"repeated key joins", "feat: x\n\nLearned: a, b\nLearned: c\n", "Learned", []string{"a", "b", "c"}},
		{"continuation", "feat: x\n\nLearned: a,\n  b\n", "Learned", []string{"a", "b"}},
		{"key case", "feat: x\n\nlearned: a\nLEARNED: b\n", "Learned", []string{"a", "b"}},
		{"body line ignored", "feat: x\n\nLearned: x\n\nBody text.\n\nLearned: y\n", "Learned", []string{"y"}},
		{"in body text", "feat: x\n\nLearned: x\nmore text after it\n\nNothing here.\n", "Learned", []string{}},
		{"prose body before trailer block", "feat: x\n\nLearned: x\nmore prose after it\n\nCapability: a\n", "Learned", []string{}},
		{"key-only paragraph is a trailer", "feat: x\n\nLearned: x\n\nCapability: a\n", "Learned", []string{"x"}},
		{"other keys ignored", "feat: x\n\nCapability: a\nLearned: b\n", "Capability", []string{"a"}},
		{"absent key", "feat: x\n\nCapability: a\n", "Learned", []string{}},
		{"subject only", "Learned: x\n", "Learned", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Trailers(tt.msg, tt.key); !reflect.DeepEqual(got, tt.values) {
				t.Errorf("got %v, want %v", got, tt.values)
			}
		})
	}
}

func TestReadParsesTrailers(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "chore: base")
	commit(t, dir, "feat: a\n\nBody.\n\nCapability: slices\n\nCo-authored-by: X <x@y.z>")
	commit(t, dir, "feat: b\n\nCapability: map")
	commit(t, dir, "feat: c")
	got, err := Read(r, "HEAD~3..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d commits, want 3", len(got))
	}
	if got[0].Subject != "feat: c" || len(got[0].Capabilities) != 0 || got[0].Capabilities == nil {
		t.Errorf("newest commit wrong: %+v", got[0])
	}
	if !reflect.DeepEqual(got[1].Capabilities, []string{"map"}) {
		t.Errorf("commit b wrong: %+v", got[1])
	}
	if !reflect.DeepEqual(got[2].Capabilities, []string{"slices"}) || got[2].Subject != "feat: a" {
		t.Errorf("squash-shaped commit wrong: %+v", got[2])
	}
	if len(got[2].Hash) != 40 || !strings.HasPrefix(got[2].Hash, got[2].Short) {
		t.Errorf("hashes wrong: %+v", got[2])
	}
}

func TestReadBadRange(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "chore: base")
	_, err := Read(r, "nosuch..HEAD")
	if err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("want git's error naming the range, got %v", err)
	}
}

type fk struct{ subject, kind, slug string }

// kinds checks the range and returns each finding as its commit's subject,
// kind and slug.
func kinds(t *testing.T, r vcs.Repo, rng string) ([]fk, []Finding) {
	t.Helper()
	commits, err := Read(r, rng)
	if err != nil {
		t.Fatal(err)
	}
	subjects := map[string]string{}
	for _, c := range commits {
		subjects[c.Short] = c.Subject
	}
	got, err := Check(r, commits)
	if err != nil {
		t.Fatal(err)
	}
	var out []fk
	for _, f := range got {
		out = append(out, fk{subjects[f.Commit], f.Kind, f.Slug})
	}
	return out, got
}

func TestCheck(t *testing.T) {
	r, dir := newRepo(t)
	addCapability(t, dir, "slices", "chore: slices")
	addCapability(t, dir, "map", "chore: map")
	commit(t, dir, "a1\n\nCapability: slices")
	commit(t, dir, "b2")
	commit(t, dir, "c3\n\nCapability: slices, nope")
	commit(t, dir, "d4\n\nCapability: slices.md")
	commit(t, dir, "e5\n\nCapability: map\nLoaded: gone")
	got, findings := kinds(t, r, "HEAD~5..HEAD")
	want := []fk{
		{"d4", KindUnknown, "slices.md"},
		{"c3", KindMultiple, ""},
		{"c3", KindUnknown, "nope"},
		{"b2", KindMissing, ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if findings[0].Message != `Capability "slices.md" is not in the map` || findings[3].Message != "no Capability trailer" {
		t.Errorf("messages: %q, %q", findings[0].Message, findings[3].Message)
	}
	if !strings.HasPrefix(findings[1].Message, "Capability names 2 capabilities (slices, nope)") {
		t.Errorf("multiple message: %q", findings[1].Message)
	}
	if got, err := Check(r, nil); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no commits should give an empty, non-nil list, got %v %v", got, err)
	}
}

// The slug must be in the map as the change leaves it: a change that adds a
// capability may name it; a change naming a slug a later change adds may not,
// nor one naming a slug only the working tree has.
func TestCheckMapAsTheChangeLeavesIt(t *testing.T) {
	r, dir := newRepo(t)
	addCapability(t, dir, "map", "chore: base")
	commit(t, dir, "early\n\nCapability: later")
	addCapability(t, dir, "later", "feat: add later\n\nCapability: later")
	addCapability(t, dir, "new", "feat: add new\n\nCapability: new")
	p := filepath.Join(dir, ".physa", "capabilities", "loose.md")
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit(t, dir, "untracked\n\nCapability: loose")
	got, _ := kinds(t, r, "HEAD~4..HEAD")
	want := []fk{
		{"untracked", KindUnknown, "loose"},
		{"early", KindUnknown, "later"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCheckFilesError(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "x\n\nCapability: a")
	_, err := Check(r, []Commit{{Hash: "deadbeefdeadbeef", Short: "deadbee", Capabilities: []string{"a"}}})
	if err == nil || !strings.Contains(err.Error(), "deadbeefdeadbeef") {
		t.Errorf("want an error naming the change, got %v", err)
	}
}

func TestSessions(t *testing.T) {
	cases := []struct {
		name, msg string
		want      []string
	}{
		{"one", "feat: a\n\nCapability: map\nSession: abc", []string{"abc"}},
		{"comma list", "feat: a\n\nSession: a, b,c", []string{"a", "b", "c"}},
		{"two lines", "feat: a\n\nSession: a\nSession: b", []string{"a", "b"}},
		{"duplicates", "feat: a\n\nSession: a, b\nSession: a", []string{"a", "b"}},
		{"body line is not read", "feat: a\n\nSession: body\n\nSome prose.\n\nCapability: map", []string{}},
		{"none", "feat: a", []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sessions(c.msg); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Sessions = %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestReadFillsSessions(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "chore: base")
	commit(t, dir, "feat: a\n\nCapability: map\nSession: s1, s2")
	got, err := Read(r, "HEAD~1..HEAD")
	if err != nil || len(got) != 1 {
		t.Fatalf("Read = %+v, %v", got, err)
	}
	if !reflect.DeepEqual(got[0].Sessions, []string{"s1", "s2"}) {
		t.Errorf("Sessions = %#v", got[0].Sessions)
	}
}

func TestTrailerBlock(t *testing.T) {
	tests := []struct {
		name, msg string
		want      [][2]string
	}{
		{"every key", "feat: x\n\nbody\n\nCapability: a\nSession: s1, s2\n", [][2]string{{"Capability", "a"}, {"Session", "s1, s2"}}},
		{"continuation", "feat: x\n\nLearned: a,\n  b\n", [][2]string{{"Learned", "a, b"}}},
		{"two trailer paragraphs", "feat: x\n\nA: 1\n\nB: 2\n", [][2]string{{"A", "1"}, {"B", "2"}}},
		{"prose after is no block", "feat: x\n\nA: 1\nprose\n", [][2]string{}},
		{"subject only", "Learned: x\n", [][2]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrailerBlock(tt.msg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// A change made before the map serves no capability yet: no finding for a
// change whose parent leaves no map, the root included, nor for the change
// that adds the map. The first change after the map needs its trailer.
func TestCheckExemptsChangesBeforeTheMap(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "root")
	commit(t, dir, "second")
	addCapability(t, dir, "map", "chore: add the map")
	commit(t, dir, "after")
	got, findings := kinds(t, r, "HEAD~3..HEAD")
	want := []fk{{"after", KindMissing, ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if findings[0].Message != "no Capability trailer" {
		t.Errorf("message: %q", findings[0].Message)
	}
	all, _ := kinds(t, r, "HEAD")
	if !reflect.DeepEqual(all, want) {
		t.Errorf("whole history with a root change: got %v, want %v", all, want)
	}
}

// addFolderCapability writes the folder of slug (an index.md) and the
// product folder and commits them with message.
func addFolderCapability(t *testing.T, dir, slug, message string) {
	t.Helper()
	for p, body := range map[string]string{
		filepath.Join(".physa", "product", "index.md"):            "# Product\n",
		filepath.Join(".physa", "capabilities", slug, "index.md"): "---\npromise: x\n---\n# " + slug + "\n",
	} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", message)
}

// A trailer names a capability folder of the map as the change leaves it.
func TestCheckFolderCapability(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "root")
	addFolderCapability(t, dir, "map", "chore: add the map")
	commit(t, dir, "ok\n\nCapability: map")
	commit(t, dir, "none")
	got, _ := kinds(t, r, "HEAD~3..HEAD")
	want := []fk{{"none", KindMissing, ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	addFolderCapability(t, dir, "later", "feat: add later\n\nCapability: later")
	got, _ = kinds(t, r, "HEAD~1..HEAD")
	if len(got) != 0 {
		t.Errorf("a change adding a folder may name it, got %v", got)
	}
}

// A change before the map is exempt in both layouts: its parent has neither
// .physa/product.md nor a file under .physa/product/.
func TestCheckBeforeTheMapInBothLayouts(t *testing.T) {
	for _, layout := range []string{"files", "folders"} {
		t.Run(layout, func(t *testing.T) {
			r, dir := newRepo(t)
			commit(t, dir, "root")
			commit(t, dir, "second")
			if layout == "files" {
				addCapability(t, dir, "map", "chore: add the map")
			} else {
				addFolderCapability(t, dir, "map", "chore: add the map")
			}
			commit(t, dir, "after")
			got, _ := kinds(t, r, "HEAD~3..HEAD")
			want := []fk{{"after", KindMissing, ""}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

// A series that converts from files to folders keeps resolving slugs on both
// sides: before the conversion the old file names the capability, after it
// the folder does, and a slug the folder map lacks is not rescued by a
// leftover old file.
func TestCheckConvertSeries(t *testing.T) {
	r, dir := newRepo(t)
	addCapability(t, dir, "map", "chore: files")
	commit(t, dir, "old\n\nCapability: map")
	// Convert: the folder replaces the file.
	run(t, dir, "rm", "-q", ".physa/capabilities/map.md", ".physa/product.md")
	addFolderCapability(t, dir, "map", "chore: convert\n\nCapability: map")
	commit(t, dir, "new\n\nCapability: map")
	// A stray old file beside the folders does not make a slug.
	if err := os.WriteFile(filepath.Join(dir, ".physa", "capabilities", "stray.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-q", "-m", "stray\n\nCapability: stray")
	got, _ := kinds(t, r, "HEAD~4..HEAD")
	want := []fk{{"stray", KindUnknown, "stray"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A slug the folder map lacks is unknown.
func TestCheckUnknownSlugInFolderMap(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "root")
	addFolderCapability(t, dir, "map", "chore: add the map")
	commit(t, dir, "bad\n\nCapability: nosuch")
	got, findings := kinds(t, r, "HEAD~1..HEAD")
	want := []fk{{"bad", KindUnknown, "nosuch"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if findings[0].Message != `Capability "nosuch" is not in the map` {
		t.Errorf("message: %q", findings[0].Message)
	}
}

// A trailer value that is not a slug is unknown, and never becomes part of
// a path: "../product" would list the product folder, "map/../map" and
// "Map" are no slugs either.
func TestCheckUnknownInvalidSlug(t *testing.T) {
	r, dir := newRepo(t)
	commit(t, dir, "root")
	addFolderCapability(t, dir, "map", "chore: add the map")
	commit(t, dir, "up\n\nCapability: ../product")
	commit(t, dir, "dots\n\nCapability: map/../map")
	commit(t, dir, "upper\n\nCapability: Map")
	commit(t, dir, "nested\n\nCapability: map/index.md")
	got, _ := kinds(t, r, "HEAD~4..HEAD")
	want := []fk{
		{"nested", KindUnknown, "map/index.md"},
		{"upper", KindUnknown, "Map"},
		{"dots", KindUnknown, "map/../map"},
		{"up", KindUnknown, "../product"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
