package skillagent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dphaener/physa/internal/adapter/claude"
)

func env(home string, vars map[string]string, bins ...string) Env {
	return Env{
		Home:   home,
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(name string) (string, error) {
			for _, b := range bins {
				if b == name {
					return "/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func founds(as []Agent) map[string]bool {
	m := map[string]bool{}
	for _, a := range as {
		m[a.Name] = a.Found
	}
	return m
}

func TestListOrderAndPaths(t *testing.T) {
	home := t.TempDir()
	as := List(env(home, nil))
	names := []string{"claude-code", "codex", "opencode", "pi"}
	labels := []string{"Claude Code", "Codex", "OpenCode", "Pi"}
	if len(as) != 4 {
		t.Fatalf("got %d agents", len(as))
	}
	shared := filepath.Join(home, ".agents", "skills", "physa", "SKILL.md")
	for i, a := range as {
		if a.Name != names[i] || a.Label != labels[i] {
			t.Errorf("agent %d = %s/%s", i, a.Name, a.Label)
		}
	}
	if as[0].Name != claude.Name || as[0].Stub != claude.SkillFile(home, "physa") || as[0].Hook != claude.SettingsFile(home) || as[0].Shared {
		t.Errorf("claude = %+v", as[0])
	}
	if as[1].Stub != shared || as[1].Hook != filepath.Join(home, ".codex", "hooks.json") || !as[1].Shared || !as[1].Supported() {
		t.Errorf("codex = %+v", as[1])
	}
	for _, a := range as[2:] {
		if a.Stub != shared || a.Hook != "" || !a.Shared || a.Supported() {
			t.Errorf("%s = %+v", a.Name, a)
		}
	}
}

func TestListNotFoundEmptyHome(t *testing.T) {
	for name, f := range founds(List(env(t.TempDir(), nil))) {
		if f {
			t.Errorf("%s found in an empty home", name)
		}
	}
}

func TestListFoundByDirectory(t *testing.T) {
	for name, dir := range map[string]string{
		"claude-code": ".claude",
		"codex":       ".codex",
		"opencode":    filepath.Join(".config", "opencode"),
		"pi":          filepath.Join(".pi", "agent"),
	} {
		home := t.TempDir()
		mkdir(t, filepath.Join(home, dir))
		for n, f := range founds(List(env(home, nil))) {
			if f != (n == name) {
				t.Errorf("dir for %s: %s found=%v", name, n, f)
			}
		}
	}
}

func TestListFoundByBinary(t *testing.T) {
	for name, bin := range map[string]string{
		"claude-code": "claude", "codex": "codex", "opencode": "opencode", "pi": "pi",
	} {
		for n, f := range founds(List(env(t.TempDir(), nil, bin))) {
			if f != (n == name) {
				t.Errorf("binary %s: %s found=%v", bin, n, f)
			}
		}
	}
}

func TestListOverrides(t *testing.T) {
	home := t.TempDir()
	codex, xdg, pi := filepath.Join(t.TempDir(), "c"), filepath.Join(t.TempDir(), "x"), filepath.Join(t.TempDir(), "p")
	mkdir(t, codex)
	mkdir(t, filepath.Join(xdg, "opencode"))
	mkdir(t, pi)
	as := List(env(home, map[string]string{"CODEX_HOME": codex, "XDG_CONFIG_HOME": xdg, "PI_CODING_AGENT_DIR": pi}))
	for _, a := range as[1:] {
		if !a.Found {
			t.Errorf("%s not found through its override", a.Name)
		}
	}
	if as[1].Hook != filepath.Join(codex, "hooks.json") {
		t.Errorf("codex hook = %s", as[1].Hook)
	}
}

func TestListOverrideReplacesDefault(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, ".codex"))
	as := List(env(home, map[string]string{"CODEX_HOME": filepath.Join(home, "elsewhere")}))
	if as[1].Found {
		t.Error("codex found by default directory despite CODEX_HOME")
	}
}

func TestListRelativeXDGIgnored(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, ".config", "opencode"))
	mkdir(t, filepath.Join("testdata-rel", "opencode"))
	defer os.RemoveAll("testdata-rel")
	as := List(env(home, map[string]string{"XDG_CONFIG_HOME": "testdata-rel"}))
	if !as[2].Found {
		t.Error("relative XDG_CONFIG_HOME was not ignored")
	}
	home2 := t.TempDir()
	if List(env(home2, map[string]string{"XDG_CONFIG_HOME": "testdata-rel"}))[2].Found {
		t.Error("relative XDG_CONFIG_HOME was followed")
	}
}

func TestListRegularFileNotFound(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".claude", ".codex", filepath.Join(".config", "opencode"), filepath.Join(".pi", "agent")} {
		mkdir(t, filepath.Dir(filepath.Join(home, d)))
		if err := os.WriteFile(filepath.Join(home, d), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for n, f := range founds(List(env(home, nil))) {
		if f {
			t.Errorf("%s found from a regular file", n)
		}
	}
}

func TestListUsesInjectedStat(t *testing.T) {
	e := env(t.TempDir(), nil)
	called := false
	e.Stat = func(string) (os.FileInfo, error) { called = true; return nil, os.ErrNotExist }
	List(e)
	if !called {
		t.Error("Stat not used")
	}
}
