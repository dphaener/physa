// Package skillagent is the agents Physa's skill is set up for: where each
// keeps Physa's stub and session hook, and whether it is found on this
// account, by its config directory or its binary on PATH. It only reads.
//
// The paths, with home the person's home directory:
//
//   - Claude Code: stub claude.SkillFile(home, "physa"), hook
//     claude.SettingsFile(home); found by home/.claude or `claude`.
//     CLAUDE_CONFIG_DIR is not followed, as the adapter does not.
//   - Codex: config directory $CODEX_HOME when set, else home/.codex; stub
//     home/.agents/skills/physa/SKILL.md, hook <config directory>/hooks.json;
//     found by the directory or `codex`.
//   - OpenCode: config directory $XDG_CONFIG_HOME/opencode when
//     XDG_CONFIG_HOME is set and absolute, else home/.config/opencode; the
//     shared stub, no hook; found by the directory or `opencode`.
//   - Pi: config directory $PI_CODING_AGENT_DIR when set, else
//     home/.pi/agent; the shared stub, no hook; found by the directory or `pi`.
package skillagent

import (
	"os"
	"path/filepath"

	"github.com/dphaener/physa/internal/adapter/claude"
)

// Agent is one agent Physa's skill is set up for.
type Agent struct {
	Name  string // as --json names it
	Label string // as people read it
	Stub  string // the SKILL.md path
	Hook  string // the hook file path, empty when Physa installs no session hook
	Found bool
	// Shared is true when Stub is the shared ~/.agents/skills/physa/SKILL.md.
	Shared bool
}

// Supported reports whether Physa installs a session hook for the agent.
func (a Agent) Supported() bool { return a.Hook != "" }

// Env is what List reaches outside itself for. A nil Stat means os.Stat.
type Env struct {
	Home     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Stat     func(string) (os.FileInfo, error)
}

// List returns Claude Code, Codex, OpenCode and Pi, in that order.
func List(e Env) []Agent {
	stat := e.Stat
	if stat == nil {
		stat = os.Stat
	}
	found := func(dir, bin string) bool {
		if fi, err := stat(dir); err == nil && fi.IsDir() {
			return true
		}
		_, err := e.LookPath(bin)
		return err == nil
	}
	shared := filepath.Join(e.Home, ".agents", "skills", "physa", "SKILL.md")

	codexDir := filepath.Join(e.Home, ".codex")
	if v := e.Getenv("CODEX_HOME"); v != "" {
		codexDir = v
	}
	openDir := filepath.Join(e.Home, ".config", "opencode")
	if v := e.Getenv("XDG_CONFIG_HOME"); v != "" && filepath.IsAbs(v) {
		openDir = filepath.Join(v, "opencode")
	}
	piDir := filepath.Join(e.Home, ".pi", "agent")
	if v := e.Getenv("PI_CODING_AGENT_DIR"); v != "" {
		piDir = v
	}

	return []Agent{
		{Name: claude.Name, Label: "Claude Code", Stub: claude.SkillFile(e.Home, "physa"), Hook: claude.SettingsFile(e.Home),
			Found: found(filepath.Join(e.Home, ".claude"), "claude")},
		{Name: "codex", Label: "Codex", Stub: shared, Hook: filepath.Join(codexDir, "hooks.json"), Shared: true,
			Found: found(codexDir, "codex")},
		{Name: "opencode", Label: "OpenCode", Stub: shared, Shared: true, Found: found(openDir, "opencode")},
		{Name: "pi", Label: "Pi", Stub: shared, Shared: true, Found: found(piDir, "pi")},
	}
}
