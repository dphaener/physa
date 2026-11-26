package skillcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter/claude"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/skillhook"
	"github.com/dphaener/physa/internal/skillstub"
)

// harness runs the skill group over a temporary home, never the real one.
type harness struct {
	home        string
	interactive bool
	answer      bool
	asked       []string
	stdout      bytes.Buffer
}

func newHarness(t *testing.T) *harness { return &harness{home: t.TempDir()} }

func (h *harness) run(args ...string) (int, string, string) {
	h.stdout.Reset()
	var errb bytes.Buffer
	e := env{
		home:        func() (string, error) { return h.home, nil },
		interactive: func() bool { return h.interactive },
		confirm: func(q string) (bool, error) {
			h.asked = append(h.asked, q+"|"+h.stdout.String())
			return h.answer, nil
		},
	}
	code := command.Execute(command.New("physa", "", nil, nil, newCommand(e)), append([]string{"skill"}, args...), &h.stdout, &errb)
	return code, h.stdout.String(), errb.String()
}

func (h *harness) path() string { return claude.SkillFile(h.home, "physa") }

// listing is every file under dir with its bytes, and every directory.
func listing(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			lines = append(lines, rel+"/")
			return nil
		}
		b, err := os.ReadFile(p)
		lines = append(lines, rel+": "+string(b))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

type agentsOut struct {
	Agents []agentJSON `json:"agents"`
}

func decode(t *testing.T, s string) agentJSON {
	t.Helper()
	var o agentsOut
	if err := json.Unmarshal([]byte(s), &o); err != nil || len(o.Agents) != 1 {
		t.Fatalf("not one agent: %q, %v", s, err)
	}
	return o.Agents[0]
}

func (h *harness) state(t *testing.T) string {
	t.Helper()
	code, out, errs := h.run("status", "--json")
	if code != 0 || errs != "" {
		t.Fatalf("status: %d %q", code, errs)
	}
	return decode(t, out).State
}

func TestSetupStatusRemoveRoundTrip(t *testing.T) {
	h := newHarness(t)
	other := filepath.Join(h.home, ".claude", "skills", "other", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := listing(t, h.home)
	if code, _, errs := h.run("setup", "--yes"); code != 0 {
		t.Fatalf("setup: %d %q", code, errs)
	}
	if b, _ := os.ReadFile(h.path()); string(b) != skillstub.Text() {
		t.Fatalf("stub is %q", b)
	}
	if s := h.state(t); s != "current" {
		t.Fatalf("state after setup: %s", s)
	}
	if code, out, _ := h.run("remove", "--yes"); code != 0 || !strings.Contains(out, "removed") {
		t.Fatalf("remove: %d %q", code, out)
	}
	// Remove leaves the settings file in place, emptied of Physa's entry.
	if b, err := os.ReadFile(h.settings()); err != nil || strings.TrimSpace(string(b)) != "{}" {
		t.Fatalf("settings after remove: %q, %v", b, err)
	}
	if err := os.Remove(h.settings()); err != nil {
		t.Fatal(err)
	}
	if after := listing(t, h.home); after != before {
		t.Fatalf("skills directory changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestOlderStubIsStaleAndSetupReplacesIt(t *testing.T) {
	h := newHarness(t)
	old := "---\nname: physa\nmetadata:\n  physa-stub: \"0\"\n---\nold\n"
	if err := os.MkdirAll(filepath.Dir(h.path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.path(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := h.state(t); s != "stale" {
		t.Fatalf("state %s", s)
	}
	if code, _, errs := h.run("setup", "--yes"); code != 0 {
		t.Fatalf("setup: %d %q", code, errs)
	}
	if b, _ := os.ReadFile(h.path()); string(b) != skillstub.Text() {
		t.Fatalf("not replaced: %q", b)
	}
	if s := h.state(t); s != "current" {
		t.Fatalf("state %s", s)
	}
}

func TestStatusJSONReportsClaudeCode(t *testing.T) {
	h := newHarness(t)
	a := decode(t, func() string { _, out, _ := h.run("status", "--json"); return out }())
	if a.Agent != "claude-code" || a.State != "missing" || a.Path != h.path() || a.Action != "" {
		t.Fatalf("missing: %+v", a)
	}
	h.run("setup", "--yes")
	if a := decode(t, func() string { _, out, _ := h.run("status", "--json"); return out }()); a.State != "current" {
		t.Fatalf("current: %+v", a)
	}
}

func TestSetupOffATerminalWithoutYesWritesNothing(t *testing.T) {
	h := newHarness(t)
	code, out, errs := h.run("setup")
	if code != 2 || out != "" || !strings.Contains(errs, "--yes") || !strings.Contains(errs, "nothing written") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if entries, _ := os.ReadDir(h.home); len(entries) != 0 {
		t.Fatalf("home is not empty: %v", entries)
	}
	if code, out, _ := h.run("setup", "--json"); code != 2 || decode(t, out).Action != "refused" {
		t.Fatalf("json: %d %q", code, out)
	}
	if code, _, errs := h.run("remove"); code != 0 {
		t.Fatalf("remove of nothing: %d %q", code, errs)
	}
}

func TestSomeoneElsesSkillIsNeverTouched(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(filepath.Dir(h.path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.path(), []byte("hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"setup", "remove"} {
		code, _, errs := h.run(verb, "--yes")
		if code != 2 || !strings.Contains(errs, h.path()) || !strings.Contains(errs, "not written by Physa") {
			t.Errorf("%s: %d %q", verb, code, errs)
		}
	}
	if b, _ := os.ReadFile(h.path()); string(b) != "hand\n" {
		t.Fatalf("changed: %q", b)
	}
	if s := h.state(t); s != "other" {
		t.Fatalf("state %s", s)
	}
	if _, out, _ := h.run("status"); !strings.Contains(out, "not written by Physa") {
		t.Fatalf("status: %q", out)
	}
}

func TestOnATerminalSetupListsThenAsks(t *testing.T) {
	h := newHarness(t)
	h.interactive = true
	h.answer = true
	if code, _, errs := h.run("setup"); code != 0 {
		t.Fatalf("yes: %d %q", code, errs)
	}
	if len(h.asked) != 1 || !strings.HasPrefix(h.asked[0], "Write Physa's skill?|") ||
		!strings.Contains(h.asked[0], h.path()) || !strings.Contains(h.asked[0], "missing -> current") {
		t.Fatalf("asked %q", h.asked)
	}
	if h.state(t) != "current" {
		t.Fatal("not written")
	}
	h.run("remove", "--yes")
	h.answer = false
	code, _, errs := h.run("setup")
	if code != 2 || !strings.Contains(errs, "nothing written") {
		t.Fatalf("no: %d %q", code, errs)
	}
	if h.state(t) != "missing" {
		t.Fatal("written after a no")
	}
}

func TestOnATerminalRemoveAsks(t *testing.T) {
	h := newHarness(t)
	h.run("setup", "--yes")
	h.interactive = true
	h.answer = false
	if code, _, _ := h.run("remove"); code != 2 || h.state(t) != "current" {
		t.Fatal("a no removed it")
	}
	h.answer = true
	if code, _, _ := h.run("remove"); code != 0 || h.state(t) != "missing" {
		t.Fatal("a yes did not remove it")
	}
}

func TestSetupWhenCurrentAsksNothing(t *testing.T) {
	h := newHarness(t)
	h.interactive = true
	h.run("setup", "--yes")
	code, out, _ := h.run("setup")
	if code != 0 || len(h.asked) != 0 || !strings.Contains(out, "already current") {
		t.Fatalf("%d %q %v", code, out, h.asked)
	}
}

func TestBareSkillRunsStatus(t *testing.T) {
	h := newHarness(t)
	code, out, _ := h.run()
	if code != 0 || !strings.Contains(out, "Claude Code") || !strings.Contains(out, "missing") {
		t.Fatalf("%d %q", code, out)
	}
	if code, out, _ := h.run("--json"); code != 0 || decode(t, out).State != "missing" {
		t.Fatalf("json %d %q", code, out)
	}
	if entries, _ := os.ReadDir(h.home); len(entries) != 0 {
		t.Fatal("status wrote")
	}
}

func TestNoHomeIsAnIOError(t *testing.T) {
	e := env{home: func() (string, error) { return "", errors.New("$HOME is not defined") }}
	var out, errb bytes.Buffer
	code := command.Execute(command.New("physa", "", nil, nil, newCommand(e)), []string{"skill", "setup", "--yes"}, &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "physa skill setup:") {
		t.Fatalf("%d %q", code, errb.String())
	}
}

func TestPositionalArgumentsAreBadUsage(t *testing.T) {
	h := newHarness(t)
	for _, v := range []string{"setup", "status", "remove"} {
		if code, _, _ := h.run(v, "extra"); code != 2 {
			t.Errorf("%s extra: %d", v, code)
		}
	}
}

const prior = `{
  "model": "x",
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo hi"}]}],
    "SessionStart": [{"hooks": [{"type": "command", "command": "echo other"}]}]
  },
  "permissions": {"allow": ["Bash(ls)"]},
  "zeta": [1, 2]
}
`

func (h *harness) settings() string { return claude.SettingsFile(h.home) }

func (h *harness) seed(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.settings()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.settings(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) read(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(h.settings())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (h *harness) hookState(t *testing.T) string {
	t.Helper()
	_, out, _ := h.run("status", "--json")
	var o struct{ Agents []agentJSON }
	if err := json.Unmarshal([]byte(out), &o); err != nil || len(o.Agents) != 1 || o.Agents[0].Hook == nil {
		t.Fatalf("no hook object: %q, %v", out, err)
	}
	return o.Agents[0].Hook.State
}

func TestSetupAddsOneHookKeepingEverythingElse(t *testing.T) {
	h := newHarness(t)
	h.seed(t, prior)
	if code, _, errs := h.run("setup", "--yes"); code != 0 {
		t.Fatalf("setup: %d %q", code, errs)
	}
	got := h.read(t)
	if n := strings.Count(got, skillhook.Command); n != 1 {
		t.Fatalf("%d physa entries in %s", n, got)
	}
	for _, want := range []string{`"model": "x"`, `"echo hi"`, `"echo other"`, `"Bash(ls)"`} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %s in %s", want, got)
		}
	}
	if m, p, z := strings.Index(got, `"model"`), strings.Index(got, `"permissions"`), strings.Index(got, `"zeta"`); !(m < p && p < z) {
		t.Errorf("key order changed: %s", got)
	}
	if strings.Index(got, "echo other") > strings.Index(got, skillhook.Command) {
		t.Errorf("physa's entry should come after the other SessionStart entry: %s", got)
	}
	if h.hookState(t) != "current" {
		t.Fatal("hook not current")
	}
	if b, _ := os.ReadFile(h.path()); string(b) != skillstub.Text() {
		t.Fatal("stub not written")
	}
	// Twice: nothing to do, and the bytes stay.
	code, out, _ := h.run("setup", "--yes")
	if code != 0 || strings.Count(out, "already current") != 2 || h.read(t) != got {
		t.Fatalf("second setup: %d %q", code, out)
	}
}

func TestRemoveLeavesTheOthers(t *testing.T) {
	h := newHarness(t)
	h.seed(t, prior)
	var want any
	if err := json.Unmarshal([]byte(prior), &want); err != nil {
		t.Fatal(err)
	}
	h.run("setup", "--yes")
	if code, _, errs := h.run("remove", "--yes"); code != 0 {
		t.Fatalf("remove: %d %q", code, errs)
	}
	got := h.read(t)
	var back any
	if err := json.Unmarshal([]byte(got), &back); err != nil || !reflect.DeepEqual(want, back) {
		t.Fatalf("not the original: %s (%v)", got, err)
	}
	if strings.Contains(got, skillhook.Command) {
		t.Fatalf("still there: %s", got)
	}
	if strings.Index(got, `"model"`) > strings.Index(got, `"zeta"`) {
		t.Fatalf("order: %s", got)
	}
	if h.hookState(t) != "missing" {
		t.Fatal("hook still reported")
	}
	if _, err := os.Stat(h.path()); err == nil {
		t.Fatal("stub still there")
	}
}

func TestStatusShowsTheHookCurrentStaleAndMissing(t *testing.T) {
	h := newHarness(t)
	if h.hookState(t) != "missing" {
		t.Fatal("not missing")
	}
	h.run("setup", "--yes")
	if h.hookState(t) != "current" {
		t.Fatal("not current")
	}
	h.seed(t, strings.Replace(h.read(t), `"timeout": 5`, `"timeout": 9`, 1))
	if h.hookState(t) != "stale" {
		t.Fatalf("not stale: %s", h.read(t))
	}
	_, out, _ := h.run("status")
	if !strings.Contains(out, "hook") || !strings.Contains(out, "stale") || !strings.Contains(out, "part") {
		t.Fatalf("table: %q", out)
	}
	if code, _, _ := h.run("setup", "--yes"); code != 0 || h.hookState(t) != "current" {
		t.Fatal("stale not replaced")
	}
}

func TestInvalidSettingsRefusesTheHookButWritesTheStub(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "{not json")
	code, _, errs := h.run("setup", "--yes")
	if code != 2 || !strings.Contains(errs, h.settings()) || !strings.Contains(errs, "left as it is") {
		t.Fatalf("setup: %d %q", code, errs)
	}
	if h.read(t) != "{not json" {
		t.Fatal("settings changed")
	}
	if b, _ := os.ReadFile(h.path()); string(b) != skillstub.Text() {
		t.Fatal("stub not written")
	}
	if code, _, _ := h.run("remove", "--yes"); code != 2 || h.read(t) != "{not json" {
		t.Fatalf("remove: %d", code)
	}
	if _, out, _ := h.run("status"); !strings.Contains(out, "not editable by Physa") {
		t.Fatalf("status: %q", out)
	}
	if h.hookState(t) != "other" {
		t.Fatal("state")
	}
}

func TestStatusJSONCarriesTheHookObject(t *testing.T) {
	h := newHarness(t)
	_, out, _ := h.run("status", "--json")
	if !strings.Contains(out, `"hook": {`) || !strings.Contains(out, h.settings()) {
		t.Fatalf("%q", out)
	}
	_, out, _ = h.run("setup", "--yes", "--json")
	if !strings.Contains(out, `"action": "written"`) {
		t.Fatalf("%q", out)
	}
}
