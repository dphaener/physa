package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// shippedHooks returns the absolute path of the repo's tools/hooks.
func shippedHooks(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "tools", "hooks"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prepare-commit-msg")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// hookedRepo returns a repo with one change on main whose hooks path is the
// shipped hooks, made with no session.
func hookedRepo(t *testing.T) string {
	t.Helper()
	isolate(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "--local", "core.hooksPath", shippedHooks(t))
	writeFile(t, dir, "a.txt", "one\n")
	run(t, dir, "add", "a.txt")
	commit(t, dir, "feat: Base")
	return dir
}

// message returns the full message of the change rev names.
func message(t *testing.T, dir, rev string) string {
	t.Helper()
	return run(t, dir, "log", "-1", "--format=%B", rev)
}

func TestHookStamps(t *testing.T) {
	dir := hookedRepo(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "0b6c1f2e-5d6a-4c1b-9a3e-2f4d5c6b7a8e")
	commit(t, dir, "feat: Add a\n\nWhy it matters.\n\nCapability: learning\nRefs: #20")
	got := message(t, dir, "HEAD")
	t.Logf("git log -1 --format=%%B:\n%s", got)
	want := "feat: Add a\n\nWhy it matters.\n\nCapability: learning\nRefs: #20\nSession: 0b6c1f2e-5d6a-4c1b-9a3e-2f4d5c6b7a8e"
	if got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestHookStampsBelowDivider(t *testing.T) {
	dir := hookedRepo(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1")
	commit(t, dir, "feat: Add b\n\nAbove.\n---\nBelow the divider.\n\nCapability: learning")
	want := "feat: Add b\n\nAbove.\n---\nBelow the divider.\n\nCapability: learning\nSession: s1"
	if got := message(t, dir, "HEAD"); got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestHookWithoutSession(t *testing.T) {
	for _, id := range []string{"", "a b", "x;rm -rf", "s1\nSession: s2"} {
		dir := hookedRepo(t)
		t.Setenv("CLAUDE_CODE_SESSION_ID", id)
		commit(t, dir, "feat: Add c\n\nCapability: learning")
		if got := message(t, dir, "HEAD"); strings.Contains(got, "Session:") {
			t.Errorf("id %q: message = %q, want no Session:", id, got)
		}
	}
}

func TestHookAmend(t *testing.T) {
	dir := hookedRepo(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1")
	commit(t, dir, "feat: Add d\n\nCapability: learning")
	run(t, dir, "commit", "-q", "--amend", "--allow-empty", "--no-edit")
	if got := message(t, dir, "HEAD"); got != "feat: Add d\n\nCapability: learning\nSession: s1" {
		t.Errorf("an amend in the same session: message = %q", got)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s2")
	run(t, dir, "commit", "-q", "--amend", "--allow-empty", "--no-edit")
	if got := message(t, dir, "HEAD"); got != "feat: Add d\n\nCapability: learning\nSession: s1\nSession: s2" {
		t.Errorf("an amend from another session: message = %q", got)
	}
}

func TestHookSkipsRebaseAndCherryPick(t *testing.T) {
	dir := hookedRepo(t)
	run(t, dir, "checkout", "-q", "-b", "side")
	writeFile(t, dir, "b.txt", "b\n")
	run(t, dir, "add", "b.txt")
	commit(t, dir, "feat: Add b\n\nCapability: learning")
	writeFile(t, dir, "c.txt", "c\n")
	run(t, dir, "add", "c.txt")
	pick := commit(t, dir, "feat: Add c\n\nCapability: learning")
	run(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "m.txt", "m\n")
	run(t, dir, "add", "m.txt")
	commit(t, dir, "feat: Add m")

	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1")
	run(t, dir, "checkout", "-q", "-b", "picked", "main")
	run(t, dir, "cherry-pick", pick)
	if got := message(t, dir, "HEAD"); got != "feat: Add c\n\nCapability: learning" {
		t.Errorf("a cherry-pick stamped the picked change: %q", got)
	}
	run(t, dir, "cherry-pick", "-x", "side~1")
	if got := message(t, dir, "HEAD"); strings.Contains(got, "Session:") {
		t.Errorf("a cherry-pick -x stamped the picked change: %q", got)
	}

	run(t, dir, "checkout", "-q", "side")
	old := run(t, dir, "rev-parse", "HEAD")
	run(t, dir, "rebase", "-q", "main")
	if run(t, dir, "rev-parse", "HEAD") == old {
		t.Fatal("the rebase rewrote nothing")
	}
	for rev, want := range map[string]string{"HEAD": "feat: Add c\n\nCapability: learning", "HEAD~1": "feat: Add b\n\nCapability: learning"} {
		if got := message(t, dir, rev); got != want {
			t.Errorf("a rebase stamped %s: %q", rev, got)
		}
	}

	run(t, dir, "revert", "--no-edit", "HEAD")
	got := message(t, dir, "HEAD")
	if !strings.HasPrefix(got, `Revert "feat: Add c"`) || !strings.HasSuffix(got, "\nSession: s1") || strings.Count(got, "Session:") != 1 {
		t.Errorf("a revert in the session is not stamped once: %q", got)
	}
}

func TestHookSkipsRebaseContinue(t *testing.T) {
	dir := hookedRepo(t)
	run(t, dir, "checkout", "-q", "-b", "side")
	writeFile(t, dir, "a.txt", "side\n")
	run(t, dir, "commit", "-q", "-a", "-m", "feat: Side a\n\nCapability: learning")
	run(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "a.txt", "main\n")
	run(t, dir, "commit", "-q", "-a", "-m", "feat: Main a")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1")
	run(t, dir, "checkout", "-q", "side")
	if out, err := exec.Command("git", "-C", dir, "rebase", "main").CombinedOutput(); err == nil {
		t.Fatalf("the rebase did not stop on its conflict:\n%s", out)
	}
	writeFile(t, dir, "a.txt", "both\n")
	run(t, dir, "add", "a.txt")
	cont := exec.Command("git", "-C", dir, "rebase", "--continue")
	cont.Env = append(os.Environ(), "GIT_EDITOR=true")
	if out, err := cont.CombinedOutput(); err != nil {
		t.Fatalf("rebase --continue: %v\n%s", err, out)
	}
	if got := message(t, dir, "HEAD"); got != "feat: Side a\n\nCapability: learning" {
		t.Errorf("a rebase continued after a conflict stamped the change: %q", got)
	}
}

func TestHookSkipsMerge(t *testing.T) {
	dir := hookedRepo(t)
	run(t, dir, "checkout", "-q", "-b", "side")
	commit(t, dir, "feat: Side")
	run(t, dir, "checkout", "-q", "main")
	commit(t, dir, "feat: Main")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1")
	run(t, dir, "merge", "-q", "--no-ff", "--no-edit", "side")
	if got := message(t, dir, "HEAD"); strings.Contains(got, "Session:") {
		t.Errorf("a merge was stamped: %q", got)
	}
}

func TestHookEmptyMessageAborts(t *testing.T) {
	dir := hookedRepo(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1")
	head := run(t, dir, "rev-parse", "HEAD")
	empty := filepath.Join(t.TempDir(), "msg")
	if err := os.WriteFile(empty, []byte("\n# Only a comment.\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "a.txt", "two\n")
	cases := map[string][]string{
		"comments only":           {"commit", "--allow-empty", "-e", "-F", empty},
		"diff below the scissors": {"commit", "-a", "-v"},
	}
	for name, args := range cases {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%s: an empty message committed:\n%s", name, out)
		}
		if got := run(t, dir, "rev-parse", "HEAD"); got != head {
			t.Errorf("%s: HEAD moved to %s", name, got)
		}
	}
}

func TestHooksPath(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	h, err := OpenHooks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p, set, err := h.HooksPath(); err != nil || set || p != "" {
		t.Errorf("unset: HooksPath = %q, %v, %v", p, set, err)
	}
	if err := h.SetHooksPath("tools/hooks"); err != nil {
		t.Fatal(err)
	}
	if p, set, err := h.HooksPath(); err != nil || !set || p != "tools/hooks" {
		t.Errorf("set: HooksPath = %q, %v, %v", p, set, err)
	}
	if got := run(t, dir, "config", "--local", "--get", "core.hooksPath"); got != "tools/hooks" {
		t.Errorf("local core.hooksPath = %q", got)
	}
	if err := h.SetHooksPath(""); err == nil {
		t.Error("an empty hooks path is no error")
	}
	if _, err := OpenHooks(t.TempDir()); err == nil {
		t.Error("hooks of no repo is no error")
	}
}

func TestHooksPathGlobal(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[core]\n\thooksPath = ~/hooks\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	h, err := OpenHooks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p, set, err := h.HooksPath(); err != nil || !set || p != "~/hooks" {
		t.Errorf("global: HooksPath = %q, %v, %v, want the global setting", p, set, err)
	}
}

func TestActiveHooks(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	h, err := OpenHooks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := h.ActiveHooks(); err != nil || len(got) != 0 {
		t.Errorf("a fresh repo: ActiveHooks = %v, %v, want none (samples only)", got, err)
	}
	hooksDir := filepath.Join(dir, ".git", "hooks")
	for name, mode := range map[string]os.FileMode{"pre-push": 0o755, "commit-msg": 0o755, "notes": 0o644, "pre-commit.sample": 0o755} {
		if err := os.WriteFile(filepath.Join(hooksDir, name), []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(hooksDir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "config", "--local", "core.hooksPath", "elsewhere")
	got, err := h.ActiveHooks()
	if err != nil || !reflect.DeepEqual(got, []string{"commit-msg", "pre-push"}) {
		t.Errorf("ActiveHooks = %v, %v, want commit-msg and pre-push", got, err)
	}
	if err := os.RemoveAll(hooksDir); err != nil {
		t.Fatal(err)
	}
	if got, err := h.ActiveHooks(); err != nil || len(got) != 0 {
		t.Errorf("no hooks dir: ActiveHooks = %v, %v", got, err)
	}
}
