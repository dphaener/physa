package skillcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter/claude"
	"github.com/dphaener/physa/internal/skillstub"
)

func TestNotice(t *testing.T) {
	t.Parallel()
	const setup = "`physa skill setup`"
	write := func(t *testing.T, home, content string) {
		t.Helper()
		p := claude.SkillFile(home, "physa")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	missing := "Claude Code has no Physa skill on this account: run " + setup + " to install it, so its sessions learn Physa's commands and terms."
	stale := "Claude Code's Physa skill is out of date: run " + setup + " to update it."
	other := "~/.claude/skills/physa/SKILL.md was not written by Physa, so Claude Code has no current Physa skill, and " + setup + " leaves that file alone: move it aside, then run " + setup + "."

	tests := []struct {
		name  string
		agent string
		setup func(t *testing.T, home string) string
		want  string
	}{
		{"missing", claude.Name, func(t *testing.T, h string) string { return h }, missing},
		{"current", claude.Name, func(t *testing.T, h string) string {
			if err := skillstub.Write(claude.SkillFile(h, "physa")); err != nil {
				t.Fatal(err)
			}
			return h
		}, ""},
		{"stale", claude.Name, func(t *testing.T, h string) string {
			if err := skillstub.Write(claude.SkillFile(h, "physa")); err != nil {
				t.Fatal(err)
			}
			p := claude.SkillFile(h, "physa")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			write(t, h, string(b)+"\nedited\n")
			return h
		}, stale},
		{"other", claude.Name, func(t *testing.T, h string) string {
			write(t, h, "my own skill\n")
			return h
		}, other},
		{"unreadable", claude.Name, func(t *testing.T, h string) string {
			if os.Geteuid() == 0 {
				t.Skip("root reads everything")
			}
			if err := skillstub.Write(claude.SkillFile(h, "physa")); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Dir(claude.SkillFile(h, "physa"))
			if err := os.Chmod(dir, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			return h
		}, ""},
		{"empty home", claude.Name, func(t *testing.T, h string) string { return "" }, ""},
		{"unknown agent", "nobody", func(t *testing.T, h string) string { return h }, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := tc.setup(t, t.TempDir())
			got := Notice(tc.agent, home)
			if got != tc.want {
				t.Errorf("Notice = %q, want %q", got, tc.want)
			}
			if (home != "" && strings.Contains(got, home)) || strings.HasSuffix(got, "\n") {
				t.Errorf("Notice leaks a path or ends in a newline: %q", got)
			}
		})
	}
}
