package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/skillcli"
)

func runSkill(t *testing.T, d deps, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), d, args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// isolate returns deps whose state points every variable the skill commands
// read at a temporary value, and the home directory. It empties the
// process's PATH too, since the skill commands find each agent's program
// with exec.LookPath, which reads the process's PATH: so a test that
// isolates stays serial.
func isolate(t *testing.T) (deps, string) {
	t.Helper()
	home := t.TempDir()
	path := t.TempDir()
	t.Setenv("PATH", path)
	d := depsIn(t, t.TempDir(), "HOME="+home, "PATH="+path,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"CLAUDE_CONFIG_DIR="+t.TempDir(),
		"CODEX_HOME="+filepath.Join(home, ".codex"),
		"PI_CODING_AGENT_DIR="+filepath.Join(home, ".pi", "agent"),
		"OPENCODE_CONFIG_DIR="+t.TempDir())
	return d, home
}

func TestSkillShowPrintsTheGuide(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSkill(t, testDeps(t), "skill", "show")
	if code != 0 || stdout != skillcli.Guide() || stderr != "" {
		t.Errorf("skill show: exit %d, guide equal %v, stderr %q", code, stdout == skillcli.Guide(), stderr)
	}
}

func TestSkillShowWithAnArgumentIsBadUsage(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runSkill(t, testDeps(t), "skill", "show", "extra")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "physa skill show") {
		t.Errorf("skill show extra: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestBareSkillPrintsStatus(t *testing.T) {
	d, _ := isolate(t)
	code, stdout, stderr := runSkill(t, d, "skill")
	if code != 0 || !strings.Contains(stdout, "Claude Code") || !strings.Contains(stdout, "missing") || stderr != "" {
		t.Errorf("skill: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestSkillSetupStatusRemoveEndToEnd(t *testing.T) {
	d, home := isolate(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	stub := filepath.Join(home, ".claude", "skills", "physa", "SKILL.md")
	// The tests have no terminal, so setup needs --yes.
	code, _, stderr := runSkill(t, d, "skill", "setup")
	if _, err := os.Stat(filepath.Join(home, ".claude")); code != 2 || !strings.Contains(stderr, "--yes") || err == nil {
		t.Fatalf("setup without --yes: exit %d, stderr %q, stat err %v", code, stderr, err)
	}
	if code, _, stderr := runSkill(t, d, "skill", "setup", "--yes"); code != 0 {
		t.Fatalf("setup --yes: exit %d, stderr %q", code, stderr)
	}
	if b, err := os.ReadFile(stub); err != nil || !strings.Contains(string(b), "physa skill show") {
		t.Fatalf("stub: %q, %v", b, err)
	}
	if code, stdout, _ := runSkill(t, d, "skill", "status", "--json"); code != 0 || !strings.Contains(stdout, `"state": "current"`) {
		t.Fatalf("status --json: exit %d, stdout %q", code, stdout)
	}
	afterSetup, err := os.ReadFile(settings)
	if err != nil || strings.Count(string(afterSetup), "physa skill session") != 1 || !strings.Contains(string(afterSetup), `"SessionStart"`) {
		t.Fatalf("settings.json after setup: %q, %v", afterSetup, err)
	}
	if code, stdout, stderr := runSkill(t, d, "skill", "setup", "--yes"); code != 0 || strings.Count(stdout, "already current") != 2 {
		t.Fatalf("second setup: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if again, _ := os.ReadFile(settings); !bytes.Equal(again, afterSetup) {
		t.Fatalf("a second setup changed settings.json: %q", again)
	}
	if code, _, stderr := runSkill(t, d, "skill", "remove", "--yes"); code != 0 {
		t.Fatalf("remove --yes: exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(stub); err == nil {
		t.Error("the stub is still there")
	}
	if b, err := os.ReadFile(settings); err != nil || strings.Contains(string(b), "physa skill session") || strings.Contains(string(b), "SessionStart") {
		t.Errorf("settings.json after remove: %q, %v", b, err)
	}
}

func TestSkillSetupWithCodexFoundEndToEnd(t *testing.T) {
	d, home := isolate(t)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(home, ".agents", "skills", "physa", "SKILL.md")
	hooks := filepath.Join(home, ".codex", "hooks.json")
	code, stdout, stderr := runSkill(t, d, "skill", "setup", "--yes")
	if code != 0 || !strings.Contains(stdout, "/hooks") {
		t.Fatalf("setup --yes: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if b, err := os.ReadFile(shared); err != nil || !strings.Contains(string(b), "physa skill show") {
		t.Fatalf("shared stub: %q, %v", b, err)
	}
	if b, err := os.ReadFile(hooks); err != nil || strings.Count(string(b), "physa skill session") != 1 {
		t.Fatalf("hooks.json: %q, %v", b, err)
	}
	if code, stdout, _ := runSkill(t, d, "skill", "status"); code != 0 || !strings.Contains(stdout, "unsupported") || !strings.Contains(stdout, "not found") {
		t.Fatalf("status: exit %d, stdout %q", code, stdout)
	}
	if code, _, stderr := runSkill(t, d, "skill", "remove", "--yes"); code != 0 {
		t.Fatalf("remove --yes: exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(shared); err == nil {
		t.Error("the shared stub is still there")
	}
	if b, err := os.ReadFile(hooks); err != nil || strings.Contains(string(b), "physa skill session") {
		t.Errorf("hooks.json after remove: %q, %v", b, err)
	}
}

var wordPattern = regexp.MustCompile(`^[a-z]+$`)

// TestGuideCommandsExist runs --help on every command the guide names.
func TestGuideCommandsExist(t *testing.T) {
	t.Parallel()
	spans := regexp.MustCompile("`(physa [^`]*)`").FindAllStringSubmatch(skillcli.Guide(), -1)
	seen := map[string]bool{}
	for _, m := range spans {
		var words []string
		for _, tok := range strings.Fields(m[1])[1:] {
			if !wordPattern.MatchString(tok) {
				break
			}
			words = append(words, tok)
		}
		key := strings.Join(words, " ")
		if len(words) == 0 || seen[key] {
			continue
		}
		seen[key] = true
		cmd, rest, err := rootCommand(testDeps(t)).Find(words)
		if err != nil || len(rest) != 0 || cmd.CommandPath() != "physa "+key {
			// A trailing positional word (a command's own argument) is not a command.
			t.Errorf("the guide names `physa %s`, which is not a command (found %q, left over %q, err %v)", key, cmd.CommandPath(), rest, err)
			continue
		}
		code, stdout, stderr := runSkill(t, testDeps(t), append(slices.Clone(words), "--help")...)
		if code != 0 || stdout == "" {
			t.Errorf("physa %s --help: exit %d, stdout %q, stderr %q", key, code, stdout, stderr)
		}
	}
	if len(seen) < 8 {
		t.Errorf("the guide names %d distinct commands, want at least 8", len(seen))
	}
}
