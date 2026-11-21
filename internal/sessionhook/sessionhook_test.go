package sessionhook

import (
	"bytes"
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

// bare makes a repo that ships no hook, with no global or system config.
func bare(t *testing.T) (string, vcs.Hooks) {
	t.Helper()
	root, h := repo(t)
	if err := os.RemoveAll(filepath.Join(root, "tools")); err != nil {
		t.Fatal(err)
	}
	return root, h
}

func TestEmbeddedHookMatchesShipped(t *testing.T) {
	shipped, err := os.ReadFile(filepath.Join("..", "..", Hook))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shipped, script) {
		t.Errorf("internal/sessionhook/prepare-commit-msg differs from %s; copy it over", Hook)
	}
}

func TestInstallWritesEmbeddedHook(t *testing.T) {
	root, h := bare(t)
	pre := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(pre, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Install(root, h)
	file := filepath.Join(root, ".git", "hooks", name)
	if err != nil || got != (Outcome{Written: true, Path: file}) {
		t.Fatalf("Install = %+v, %v, want Written", got, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("hook mode = %v, %v, want 0755", info, err)
	}
	if b, _ := os.ReadFile(file); !bytes.Equal(b, script) {
		t.Error("the written hook is not the embedded one")
	}
	if p := hooksPath(t, root); p != "" {
		t.Errorf("core.hooksPath = %q, want it unset", p)
	}
	if info, err := os.Stat(pre); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("pre-commit not kept executable: %v, %v", info, err)
	}
	got, err = Install(root, h)
	if err != nil || got != (Outcome{Already: true, Path: file}) {
		t.Errorf("Install again = %+v, %v, want Already", got, err)
	}
}

func TestInstallCreatesHooksDir(t *testing.T) {
	root, h := bare(t)
	if err := os.RemoveAll(filepath.Join(root, ".git", "hooks")); err != nil {
		t.Fatal(err)
	}
	if got, err := Install(root, h); err != nil || !got.Written {
		t.Fatalf("Install = %+v, %v, want Written", got, err)
	}
}

func TestInstallRefusesDifferentHook(t *testing.T) {
	root, h := bare(t)
	file := filepath.Join(root, ".git", "hooks", name)
	if err := os.WriteFile(file, []byte("#!/bin/sh\necho mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Install(root, h)
	if err == nil || got != (Outcome{}) {
		t.Fatalf("Install = %+v, %v, want an error", got, err)
	}
	for _, want := range []string{file, "left alone", Hook, "your own prepare-commit-msg"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not say %q: %v", want, err)
		}
	}
	b, _ := os.ReadFile(file)
	info, _ := os.Stat(file)
	if string(b) != "#!/bin/sh\necho mine\n" || info.Mode().Perm() != 0o644 {
		t.Errorf("the existing hook changed: %q %v", b, info.Mode())
	}
}

func TestInstallRefusesHooksPathWhenNothingShipped(t *testing.T) {
	root, h := bare(t)
	gitRun(t, root, "config", "--local", "core.hooksPath", Dir)
	got, err := Install(root, h)
	if err == nil || got != (Outcome{}) {
		t.Fatalf("Install = %+v, %v, want an error", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", name)); err == nil {
		t.Error("a hook was written despite the hooks path")
	}
}

func TestInstallNonExecutableShippedHookWritesEmbedded(t *testing.T) {
	root, h := repo(t)
	if err := os.Chmod(filepath.Join(root, Hook), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Install(root, h)
	if err != nil || !got.Written {
		t.Errorf("Install = %+v, %v, want Written", got, err)
	}
	if p := hooksPath(t, root); p != "" {
		t.Errorf("core.hooksPath = %q, want it unset", p)
	}
}
