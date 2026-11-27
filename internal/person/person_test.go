package person

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fail() (string, error) { return "", errors.New("boom") }

func fixed(s string) func() (string, error) {
	return func() (string, error) { return s, nil }
}

func TestSettingWinsOverForge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "person"), []byte("\n  owner  \nignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := Resolve(Options{ConfigDir: dir, Login: fixed("dphaener"), Email: fixed("x@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "owner" || n.Source != Setting {
		t.Fatalf("got %+v", n)
	}
}

func TestSettingWithSpaceIsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "person"), []byte("Darin Haener\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(Options{ConfigDir: dir, Login: fixed("dphaener")})
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "person")) {
		t.Fatalf("want error naming the file, got %v", err)
	}
}

func TestEmptySettingFallsThrough(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "person"), []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := Resolve(Options{ConfigDir: dir, Login: fixed("dphaener")})
	if err != nil || n.Name != "dphaener" || n.Source != Forge {
		t.Fatalf("got %+v, %v", n, err)
	}
}

func TestForgeLoginIsKept(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n, err := Resolve(Options{ConfigDir: dir, Login: fixed("dphaener")})
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "dphaener" || n.Source != Forge || !n.Kept {
		t.Fatalf("got %+v", n)
	}
	b, err := os.ReadFile(filepath.Join(dir, "person"))
	if err != nil || string(b) != "dphaener\n" {
		t.Fatalf("file = %q, %v", b, err)
	}
	st, _ := os.Stat(filepath.Join(dir, "person"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	n, err = Resolve(Options{ConfigDir: dir, Login: fail})
	if err != nil || n.Name != "dphaener" || n.Source != Setting {
		t.Fatalf("second: %+v, %v", n, err)
	}
}

func TestForgeLoginNotKeptWhenWriteFails(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := Resolve(Options{ConfigDir: filepath.Join(blocker, "physa"), Login: fixed("dphaener")})
	if err != nil || n.Name != "dphaener" || n.Source != Forge || n.Kept {
		t.Fatalf("got %+v, %v", n, err)
	}
}

func TestEmailFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n, err := Resolve(Options{ConfigDir: dir, Login: fail, Email: fixed("Darin.H@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "Darin.H" || n.Source != Email || n.Kept {
		t.Fatalf("got %+v", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "person")); !os.IsNotExist(err) {
		t.Fatalf("email must not be kept: %v", err)
	}
}

func TestUnknown(t *testing.T) {
	t.Parallel()
	for _, o := range []Options{
		{ConfigDir: t.TempDir()},
		{ConfigDir: t.TempDir(), Login: fail, Email: fail},
		{ConfigDir: t.TempDir(), Login: fixed(""), Email: fixed("@example.com")},
	} {
		_, err := Resolve(o)
		if !errors.Is(err, ErrUnknown) {
			t.Fatalf("want ErrUnknown, got %v", err)
		}
		for _, w := range []string{"no setting", "no forge login", "no git email"} {
			if !strings.Contains(err.Error(), w) {
				t.Fatalf("message %q lacks %q", err, w)
			}
		}
	}
}

// envOf is a getenv over vars, standing in for the command's environment.
func envOf(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestConfigDirHonoursXDG(t *testing.T) {
	t.Parallel()
	x := t.TempDir()
	got, err := ConfigDir(envOf(map[string]string{"XDG_CONFIG_HOME": x, "HOME": t.TempDir()}))
	if err != nil || got != filepath.Join(x, "physa") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestConfigDirFallsBackToHome(t *testing.T) {
	t.Parallel()
	h := t.TempDir()
	got, err := ConfigDir(envOf(map[string]string{"XDG_CONFIG_HOME": "", "HOME": h}))
	if err != nil || got != filepath.Join(h, ".config", "physa") {
		t.Fatalf("got %q, %v", got, err)
	}
	got, _ = ConfigDir(envOf(map[string]string{"XDG_CONFIG_HOME": "relative/path", "HOME": h}))
	if got != filepath.Join(h, ".config", "physa") {
		t.Fatalf("relative XDG must be ignored, got %q", got)
	}
}

func TestConfigDirWithNoHomeSaysSo(t *testing.T) {
	t.Parallel()
	got, err := ConfigDir(envOf(nil))
	if err == nil || err.Error() != "person: no XDG_CONFIG_HOME and no HOME" {
		t.Fatalf("got %q, %v", got, err)
	}
}
