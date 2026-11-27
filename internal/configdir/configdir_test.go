package configdir

import (
	"os"
	"path/filepath"
	"testing"
)

// lookup is a getenv over vars, standing in for the command's environment.
func lookup(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestDirXDGAbsolute(t *testing.T) {
	t.Parallel()
	x := t.TempDir()
	got, err := Dir(lookup(map[string]string{"XDG_CONFIG_HOME": x, "HOME": t.TempDir()}))
	if err != nil || got != filepath.Join(x, "physa") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDirXDGRelative(t *testing.T) {
	t.Parallel()
	h := t.TempDir()
	got, err := Dir(lookup(map[string]string{"XDG_CONFIG_HOME": "relative/path", "HOME": h}))
	if err != nil || got != filepath.Join(h, ".config", "physa") {
		t.Fatalf("relative XDG must be ignored, got %q, %v", got, err)
	}
}

func TestDirNoHome(t *testing.T) {
	t.Parallel()
	got, err := Dir(lookup(map[string]string{"XDG_CONFIG_HOME": "", "HOME": ""}))
	if err == nil || err.Error() != "no XDG_CONFIG_HOME and no HOME" {
		t.Fatalf("want the error naming both, got %q, %v", got, err)
	}
}

func TestEnsureCreates(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "physa")
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf("stat %v, %v", st, err)
	}
}

func TestEnsureMissingParent(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "a", "b", "physa")
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("stat %v, %v", st, err)
	}
}

func TestEnsureTightens(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "physa")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v, %v", st.Mode().Perm(), err)
	}
}

func TestEnsureRefusesOther(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err == nil {
		t.Fatal("want refusal for a non-physa directory that is group- or other-accessible")
	}
	st, err := os.Stat(dir)
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("refused directory must be left alone: mode %v, %v", st.Mode().Perm(), err)
	}
}

func TestEnsureRelativeRefused(t *testing.T) {
	t.Parallel()
	if err := Ensure("relative/physa"); err == nil {
		t.Fatal("want error for relative dir")
	}
}
