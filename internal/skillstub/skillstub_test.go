package skillstub

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubPath(t *testing.T) (skills, path string) {
	t.Helper()
	skills = filepath.Join(t.TempDir(), ".claude", "skills")
	return skills, filepath.Join(skills, "physa", "SKILL.md")
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) State {
	t.Helper()
	st, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStubText(t *testing.T) {
	text := Text()
	for _, want := range []string{"name: physa", ".physa", `physa-stub: "` + Version + `"`, "physa skill show", "physa --help"} {
		if !strings.Contains(text, want) {
			t.Errorf("Text lacks %q", want)
		}
	}
}

func TestStateString(t *testing.T) {
	for st, want := range map[State]string{Missing: "missing", Current: "current", Stale: "stale", Other: "other"} {
		if st.String() != want {
			t.Errorf("%d = %q, want %q", st, st.String(), want)
		}
	}
}

func TestReadStates(t *testing.T) {
	_, path := stubPath(t)
	if got := read(t, path); got != Missing {
		t.Errorf("no directory = %v", got)
	}
	put(t, path, Text())
	if got := read(t, path); got != Current {
		t.Errorf("stub = %v", got)
	}
	put(t, path, strings.Replace(Text(), `physa-stub: "`+Version+`"`, `physa-stub: "0"`, 1))
	if got := read(t, path); got != Stale {
		t.Errorf("older stub = %v", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != Missing {
		t.Errorf("no file = %v", got)
	}
}

func TestReadOtherHandWritten(t *testing.T) {
	_, path := stubPath(t)
	put(t, path, "---\nname: physa\n---\nmine\n")
	if got := read(t, path); got != Other {
		t.Errorf("hand-written = %v", got)
	}
}

func TestReadOtherSymlinks(t *testing.T) {
	skills, path := stubPath(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	put(t, filepath.Join(elsewhere, "SKILL.md"), Text())
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != Other {
		t.Errorf("symlinked directory = %v", got)
	}
	if err := os.Remove(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "SKILL.md"), path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != Other {
		t.Errorf("symlinked file = %v", got)
	}
	if err := Write(path); !errors.Is(err, ErrNotPhysas) {
		t.Errorf("Write over symlink = %v", err)
	}
	if err := Remove(path); !errors.Is(err, ErrNotPhysas) {
		t.Errorf("Remove of symlink = %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(elsewhere, "SKILL.md"))
	if string(b) != Text() {
		t.Error("the symlink's target changed")
	}
}

func TestWriteOnMissing(t *testing.T) {
	_, path := stubPath(t)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != Current {
		t.Errorf("after Write = %v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only SKILL.md", len(entries))
	}
	if err := Write(path); err != nil {
		t.Errorf("Write on current: %v", err)
	}
}

func TestWriteOnStale(t *testing.T) {
	_, path := stubPath(t)
	put(t, path, strings.Replace(Text(), `physa-stub: "`+Version+`"`, `physa-stub: "0"`, 1))
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != Current {
		t.Errorf("after Write = %v", got)
	}
}

func TestWriteOnOtherReturnsErrNotPhysas(t *testing.T) {
	_, path := stubPath(t)
	const mine = "my own skill\n"
	put(t, path, mine)
	if err := Write(path); !errors.Is(err, ErrNotPhysas) {
		t.Fatalf("Write = %v, want ErrNotPhysas", err)
	}
	if b, _ := os.ReadFile(path); string(b) != mine {
		t.Errorf("bytes changed to %q", b)
	}
}

func TestRemoveOnCurrent(t *testing.T) {
	skills, path := stubPath(t)
	sibling := filepath.Join(skills, "other", "SKILL.md")
	put(t, sibling, "other skill\n")
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("physa directory remains: %v", err)
	}
	if b, _ := os.ReadFile(sibling); string(b) != "other skill\n" {
		t.Errorf("sibling changed: %q", b)
	}
	entries, err := os.ReadDir(skills)
	if err != nil || len(entries) != 1 {
		t.Errorf("skills directory = %v, %v", entries, err)
	}
	if err := Remove(path); err != nil {
		t.Errorf("Remove on missing: %v", err)
	}
}

func TestRemoveKeepsDirectoryWithOtherFiles(t *testing.T) {
	_, path := stubPath(t)
	own := filepath.Join(filepath.Dir(path), "notes.md")
	put(t, own, "notes\n")
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(own); err != nil {
		t.Errorf("own file gone: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("SKILL.md remains: %v", err)
	}
}

func TestRemoveOnOtherReturnsErrNotPhysas(t *testing.T) {
	_, path := stubPath(t)
	const mine = "my own skill\n"
	put(t, path, mine)
	if err := Remove(path); !errors.Is(err, ErrNotPhysas) {
		t.Fatalf("Remove = %v, want ErrNotPhysas", err)
	}
	if b, _ := os.ReadFile(path); string(b) != mine {
		t.Errorf("bytes changed to %q", b)
	}
}
