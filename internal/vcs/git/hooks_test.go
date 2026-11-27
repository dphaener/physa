package git

import (
	"context"
	"errors"
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
// shipped hooks, made with no session, and the environment it was made in.
func hookedRepo(t *testing.T) (string, []string) {
	t.Helper()
	e := with(isolate(t), "CLAUDE_CODE_SESSION_ID=")
	dir := t.TempDir()
	run(t, e, dir, "init", "-q", "-b", "main")
	run(t, e, dir, "config", "--local", "core.hooksPath", shippedHooks(t))
	writeFile(t, dir, "a.txt", "one\n")
	run(t, e, dir, "add", "a.txt")
	commit(t, e, dir, "feat: Base")
	return dir, e
}

// message returns the full message of the change rev names.
func message(t *testing.T, e []string, dir, rev string) string {
	t.Helper()
	return run(t, e, dir, "log", "-1", "--format=%B", rev)
}

func TestHookStamps(t *testing.T) {
	t.Parallel()
	dir, e := hookedRepo(t)
	e = with(e, "CLAUDE_CODE_SESSION_ID=0b6c1f2e-5d6a-4c1b-9a3e-2f4d5c6b7a8e")
	commit(t, e, dir, "feat: Add a\n\nWhy it matters.\n\nCapability: learning\nRefs: #20")
	got := message(t, e, dir, "HEAD")
	t.Logf("git log -1 --format=%%B:\n%s", got)
	want := "feat: Add a\n\nWhy it matters.\n\nCapability: learning\nRefs: #20\nSession: 0b6c1f2e-5d6a-4c1b-9a3e-2f4d5c6b7a8e"
	if got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestHookStampsBelowDivider(t *testing.T) {
	t.Parallel()
	dir, e := hookedRepo(t)
	e = with(e, "CLAUDE_CODE_SESSION_ID=s1")
	commit(t, e, dir, "feat: Add b\n\nAbove.\n---\nBelow the divider.\n\nCapability: learning")
	want := "feat: Add b\n\nAbove.\n---\nBelow the divider.\n\nCapability: learning\nSession: s1"
	if got := message(t, e, dir, "HEAD"); got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestHookWithoutSession(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"", "a b", "x;rm -rf", "s1\nSession: s2"} {
		dir, e := hookedRepo(t)
		e = with(e, "CLAUDE_CODE_SESSION_ID="+id)
		commit(t, e, dir, "feat: Add c\n\nCapability: learning")
		if got := message(t, e, dir, "HEAD"); strings.Contains(got, "Session:") {
			t.Errorf("id %q: message = %q, want no Session:", id, got)
		}
	}
}

func TestHookAmend(t *testing.T) {
	t.Parallel()
	dir, e := hookedRepo(t)
	e = with(e, "CLAUDE_CODE_SESSION_ID=s1")
	commit(t, e, dir, "feat: Add d\n\nCapability: learning")
	run(t, e, dir, "commit", "-q", "--amend", "--allow-empty", "--no-edit")
	if got := message(t, e, dir, "HEAD"); got != "feat: Add d\n\nCapability: learning\nSession: s1" {
		t.Errorf("an amend in the same session: message = %q", got)
	}
	e = with(e, "CLAUDE_CODE_SESSION_ID=s2")
	run(t, e, dir, "commit", "-q", "--amend", "--allow-empty", "--no-edit")
	if got := message(t, e, dir, "HEAD"); got != "feat: Add d\n\nCapability: learning\nSession: s1\nSession: s2" {
		t.Errorf("an amend from another session: message = %q", got)
	}
}

func TestHookSkipsRebaseAndCherryPick(t *testing.T) {
	t.Parallel()
	dir, e := hookedRepo(t)
	run(t, e, dir, "checkout", "-q", "-b", "side")
	writeFile(t, dir, "b.txt", "b\n")
	run(t, e, dir, "add", "b.txt")
	commit(t, e, dir, "feat: Add b\n\nCapability: learning")
	writeFile(t, dir, "c.txt", "c\n")
	run(t, e, dir, "add", "c.txt")
	pick := commit(t, e, dir, "feat: Add c\n\nCapability: learning")
	run(t, e, dir, "checkout", "-q", "main")
	writeFile(t, dir, "m.txt", "m\n")
	run(t, e, dir, "add", "m.txt")
	commit(t, e, dir, "feat: Add m")

	e = with(e, "CLAUDE_CODE_SESSION_ID=s1")
	run(t, e, dir, "checkout", "-q", "-b", "picked", "main")
	run(t, e, dir, "cherry-pick", pick)
	if got := message(t, e, dir, "HEAD"); got != "feat: Add c\n\nCapability: learning" {
		t.Errorf("a cherry-pick stamped the picked change: %q", got)
	}
	run(t, e, dir, "cherry-pick", "-x", "side~1")
	if got := message(t, e, dir, "HEAD"); strings.Contains(got, "Session:") {
		t.Errorf("a cherry-pick -x stamped the picked change: %q", got)
	}

	run(t, e, dir, "checkout", "-q", "side")
	old := run(t, e, dir, "rev-parse", "HEAD")
	run(t, e, dir, "rebase", "-q", "main")
	if run(t, e, dir, "rev-parse", "HEAD") == old {
		t.Fatal("the rebase rewrote nothing")
	}
	for rev, want := range map[string]string{"HEAD": "feat: Add c\n\nCapability: learning", "HEAD~1": "feat: Add b\n\nCapability: learning"} {
		if got := message(t, e, dir, rev); got != want {
			t.Errorf("a rebase stamped %s: %q", rev, got)
		}
	}

	run(t, e, dir, "revert", "--no-edit", "HEAD")
	got := message(t, e, dir, "HEAD")
	if !strings.HasPrefix(got, `Revert "feat: Add c"`) || !strings.HasSuffix(got, "\nSession: s1") || strings.Count(got, "Session:") != 1 {
		t.Errorf("a revert in the session is not stamped once: %q", got)
	}
}

func TestHookSkipsRebaseContinue(t *testing.T) {
	t.Parallel()
	dir, e := hookedRepo(t)
	run(t, e, dir, "checkout", "-q", "-b", "side")
	writeFile(t, dir, "a.txt", "side\n")
	run(t, e, dir, "commit", "-q", "-a", "-m", "feat: Side a\n\nCapability: learning")
	run(t, e, dir, "checkout", "-q", "main")
	writeFile(t, dir, "a.txt", "main\n")
	run(t, e, dir, "commit", "-q", "-a", "-m", "feat: Main a")
	e = with(e, "CLAUDE_CODE_SESSION_ID=s1")
	run(t, e, dir, "checkout", "-q", "side")
	rebase := exec.Command("git", "-C", dir, "rebase", "main")
	rebase.Env = e
	if out, err := rebase.CombinedOutput(); err == nil {
		t.Fatalf("the rebase did not stop on its conflict:\n%s", out)
	}
	writeFile(t, dir, "a.txt", "both\n")
	run(t, e, dir, "add", "a.txt")
	cont := exec.Command("git", "-C", dir, "rebase", "--continue")
	cont.Env = with(e, "GIT_EDITOR=true")
	if out, err := cont.CombinedOutput(); err != nil {
		t.Fatalf("rebase --continue: %v\n%s", err, out)
	}
	if got := message(t, e, dir, "HEAD"); got != "feat: Side a\n\nCapability: learning" {
		t.Errorf("a rebase continued after a conflict stamped the change: %q", got)
	}
}

func TestHookSkipsMerge(t *testing.T) {
	t.Parallel()
	dir, e := hookedRepo(t)
	run(t, e, dir, "checkout", "-q", "-b", "side")
	commit(t, e, dir, "feat: Side")
	run(t, e, dir, "checkout", "-q", "main")
	commit(t, e, dir, "feat: Main")
	e = with(e, "CLAUDE_CODE_SESSION_ID=s1")
	run(t, e, dir, "merge", "-q", "--no-ff", "--no-edit", "side")
	if got := message(t, e, dir, "HEAD"); strings.Contains(got, "Session:") {
		t.Errorf("a merge was stamped: %q", got)
	}
}

func TestHookEmptyMessageAborts(t *testing.T) {
	t.Parallel()
	dir, e := hookedRepo(t)
	e = with(e, "CLAUDE_CODE_SESSION_ID=s1")
	head := run(t, e, dir, "rev-parse", "HEAD")
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
		cmd.Env = with(e, "GIT_EDITOR=true")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%s: an empty message committed:\n%s", name, out)
		}
		if got := run(t, e, dir, "rev-parse", "HEAD"); got != head {
			t.Errorf("%s: HEAD moved to %s", name, got)
		}
	}
}

func TestHooksPath(t *testing.T) {
	t.Parallel()
	e := isolate(t)
	dir := t.TempDir()
	run(t, e, dir, "init", "-q", "-b", "main")
	h, err := OpenHooks(t.Context(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	if p, set, err := h.HooksPath(t.Context()); err != nil || set || p != "" {
		t.Errorf("unset: HooksPath = %q, %v, %v", p, set, err)
	}
	if err := h.SetHooksPath(t.Context(), "tools/hooks"); err != nil {
		t.Fatal(err)
	}
	if p, set, err := h.HooksPath(t.Context()); err != nil || !set || p != "tools/hooks" {
		t.Errorf("set: HooksPath = %q, %v, %v", p, set, err)
	}
	if got := run(t, e, dir, "config", "--local", "--get", "core.hooksPath"); got != "tools/hooks" {
		t.Errorf("local core.hooksPath = %q", got)
	}
	if err := h.SetHooksPath(t.Context(), ""); err == nil {
		t.Error("an empty hooks path is no error")
	}
	if _, err := OpenHooks(t.Context(), t.TempDir(), e); err == nil {
		t.Error("hooks of no repo is no error")
	}
}

func TestHooksPathGlobal(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[core]\n\thooksPath = ~/hooks\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := env("GIT_CONFIG_GLOBAL="+cfg, "GIT_CONFIG_NOSYSTEM=1")
	dir := t.TempDir()
	run(t, e, dir, "init", "-q", "-b", "main")
	h, err := OpenHooks(t.Context(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	if p, set, err := h.HooksPath(t.Context()); err != nil || !set || p != "~/hooks" {
		t.Errorf("global: HooksPath = %q, %v, %v, want the global setting", p, set, err)
	}
}

func TestActiveHooks(t *testing.T) {
	t.Parallel()
	e := isolate(t)
	dir := t.TempDir()
	run(t, e, dir, "init", "-q", "-b", "main")
	h, err := OpenHooks(t.Context(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := h.ActiveHooks(t.Context()); err != nil || len(got) != 0 {
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
	run(t, e, dir, "config", "--local", "core.hooksPath", "elsewhere")
	got, err := h.ActiveHooks(t.Context())
	if err != nil || !reflect.DeepEqual(got, []string{"commit-msg", "pre-push"}) {
		t.Errorf("ActiveHooks = %v, %v, want commit-msg and pre-push", got, err)
	}
	if err := os.RemoveAll(hooksDir); err != nil {
		t.Fatal(err)
	}
	if got, err := h.ActiveHooks(t.Context()); err != nil || len(got) != 0 {
		t.Errorf("no hooks dir: ActiveHooks = %v, %v", got, err)
	}
}

func TestHooksDir(t *testing.T) {
	t.Parallel()
	e := isolate(t)
	dir := t.TempDir()
	run(t, e, dir, "init", "-q", "-b", "main")
	h, err := OpenHooks(t.Context(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	run(t, e, dir, "config", "--local", "core.hooksPath", "elsewhere")
	got, err := h.HooksDir(t.Context())
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, ".git", "hooks"))
	gotReal, _ := filepath.EvalSymlinks(got)
	if err != nil || gotReal != want {
		t.Errorf("HooksDir = %q, %v, want %q whatever core.hooksPath says", got, err, want)
	}
}

func TestHooksPathCancel(t *testing.T) {
	t.Parallel()
	e := isolate(t)
	dir := t.TempDir()
	run(t, e, dir, "init", "-q", "-b", "main")
	h, err := OpenHooks(t.Context(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	if _, set, err := h.HooksPath(cancelled(t)); !errors.Is(err, context.Canceled) || set {
		t.Fatalf("set = %v, err = %v, want context.Canceled", set, err)
	}
}
