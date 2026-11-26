package skillcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter/claude"
	"github.com/dphaener/physa/internal/command"
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
