package sessionhook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// repo makes a git repo that ships an executable hook, with no global or
// system config, and returns its root and hooks.
func repo(t *testing.T) (string, vcs.Hooks) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	root := t.TempDir()
	gitRun(t, root, "init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Hook), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := git.OpenHooks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, h
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// hooksPath returns the repo's local core.hooksPath, "" when unset.
func hooksPath(t *testing.T, root string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", root, "config", "--local", "--get", "core.hooksPath").Output()
	return strings.TrimSpace(string(out))
}

func TestInstallSets(t *testing.T) {
	root, h := repo(t)
	got, err := Install(root, h)
	if err != nil || got != (Outcome{Set: true}) {
		t.Fatalf("Install = %+v, %v, want Set", got, err)
	}
	if p := hooksPath(t, root); p != Dir {
		t.Errorf("core.hooksPath = %q, want %q", p, Dir)
	}
	got, err = Install(root, h)
	if err != nil || got != (Outcome{Already: true}) {
		t.Errorf("Install again = %+v, %v, want Already", got, err)
	}
}

func TestInstallAlready(t *testing.T) {
	for _, p := range []string{"tools/hooks", "tools/hooks/", "./tools/hooks", "ABS"} {
		root, h := repo(t)
		if p == "ABS" {
			p = filepath.Join(root, Dir)
		}
		gitRun(t, root, "config", "--local", "core.hooksPath", p)
		got, err := Install(root, h)
		if err != nil || got != (Outcome{Already: true}) {
			t.Errorf("hooksPath %q: Install = %+v, %v, want Already", p, got, err)
		}
		if after := hooksPath(t, root); after != p {
			t.Errorf("hooksPath %q changed to %q", p, after)
		}
	}
}

func TestInstallRefusesHooksPathElsewhere(t *testing.T) {
	root, h := repo(t)
	gitRun(t, root, "config", "--local", "core.hooksPath", ".githooks")
	got, err := Install(root, h)
	if err == nil || got != (Outcome{}) {
		t.Fatalf("Install = %+v, %v, want an error", got, err)
	}
	t.Logf("error: %v", err)
	for _, want := range []string{`".githooks"`, "left alone", Hook} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say %q: %v", want, err)
		}
	}
	p := hooksPath(t, root)
	t.Logf("git config --local --get core.hooksPath: %s", p)
	if p != ".githooks" {
		t.Errorf("core.hooksPath = %q, want it unchanged", p)
	}
}

func TestInstallRefusesActiveHooks(t *testing.T) {
	root, h := repo(t)
	active := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(active, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Install(root, h)
	if err == nil || got != (Outcome{}) {
		t.Fatalf("Install = %+v, %v, want an error", got, err)
	}
	t.Logf("error: %v", err)
	for _, want := range []string{"pre-commit", "turn off", Hook} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say %q: %v", want, err)
		}
	}
	p := hooksPath(t, root)
	t.Logf("git config --local --get core.hooksPath: %q", p)
	if p != "" {
		t.Errorf("core.hooksPath = %q, want it still unset", p)
	}
	if b, err := os.ReadFile(active); err != nil || string(b) != "#!/bin/sh\nexit 0\n" {
		t.Errorf("the active hook changed: %q, %v", b, err)
	}
}

func TestInstallNoHook(t *testing.T) {
	root, h := repo(t)
	if err := os.Remove(filepath.Join(root, Hook)); err != nil {
		t.Fatal(err)
	}
	got, err := Install(root, h)
	if err == nil || got != (Outcome{}) || !strings.Contains(err.Error(), "ships no hook") {
		t.Errorf("Install = %+v, %v, want the repo ships no hook", got, err)
	}
	if p := hooksPath(t, root); p != "" {
		t.Errorf("core.hooksPath = %q, want it unset", p)
	}
}

func TestInstallHookNotExecutable(t *testing.T) {
	root, h := repo(t)
	if err := os.Chmod(filepath.Join(root, Hook), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Install(root, h)
	if err == nil || got != (Outcome{}) || !strings.Contains(err.Error(), "not an executable") {
		t.Errorf("Install = %+v, %v, want not executable", got, err)
	}
	if p := hooksPath(t, root); p != "" {
		t.Errorf("core.hooksPath = %q, want it unset", p)
	}
}
