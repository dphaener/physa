package claudelog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
)

const (
	fixtureLead  = "11111111-1111-4111-8111-111111111111"
	fixtureOther = "22222222-2222-4222-8222-222222222222"
	fakeSecret   = "q8w7e6r5t4"
)

// fixtureLogs reads the testdata transcripts, never the real ~/.claude.
func fixtureLogs(t *testing.T) *Logs {
	t.Helper()
	l := New("/fixture/repo")
	l.Root = "testdata"
	return l
}

func readFixture(t *testing.T) adapter.Log {
	t.Helper()
	log, err := fixtureLogs(t).ReadSession(fixtureLead)
	if err != nil {
		t.Fatal(err)
	}
	return log
}

func at(s string) time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return ts
}

func TestLogsInterventions(t *testing.T) {
	log := readFixture(t)
	want := []adapter.Message{
		{At: at("2026-11-20T10:02:00Z"), Text: "Keep the old --write flag working too."},
		{At: at("2026-11-20T10:07:00Z"), Text: "Stop. Use [redacted] and password=[redacted] for the stub only."},
	}
	if !reflect.DeepEqual(log.Messages, want) {
		t.Errorf("messages:\n got %+v\nwant %+v", log.Messages, want)
	}
}

func TestLogsRedactsKeys(t *testing.T) {
	s := fmt.Sprintf("%+v", readFixture(t))
	for _, secret := range []string{"sk-ant-", "hunter2x9"} {
		if strings.Contains(s, secret) {
			t.Errorf("log holds %q: %s", secret, s)
		}
	}
	if !strings.Contains(s, "password=[redacted]") {
		t.Errorf("log lost the name password: %s", s)
	}
}

func TestLogsAnsweredAsk(t *testing.T) {
	log := readFixture(t)
	want := []adapter.Ask{{
		At:      at("2026-11-20T10:09:00Z"),
		Text:    "Should --dry-run print the proposals?",
		Options: []string{"Print them", "Count only"},
		Answer:  "Print them\nNotes: but bound each to a screen",
	}}
	if !reflect.DeepEqual(log.Asks, want) {
		t.Errorf("asks:\n got %+v\nwant %+v", log.Asks, want)
	}
}

func TestLogsReports(t *testing.T) {
	log := readFixture(t)
	want := []adapter.Report{
		{
			At: at("2026-11-20T10:13:00Z"),
			Findings: []adapter.Finding{
				{Level: "major", Text: "The parser wrote to a nil map.", Fixed: true},
				{Level: "minor", Text: "The help text is long."},
			},
		},
		{
			At: at("2026-11-20T10:21:00Z"), Agent: "dharness:verifier verify:W1.T1", Label: "verify:W1.T1",
			Verdict: "FAIL", Findings: []adapter.Finding{{Text: "The --dry-run flag still writes the branch."}},
		},
		{
			At: at("2026-11-20T10:32:00Z"), Agent: "dharness:verifier verify:W1.T1", Label: "verify:W1.T1",
			Verdict: "PASS", Passed: true,
		},
		{
			At: at("2026-11-20T10:41:00Z"), Agent: "dharness:reviewer review:quality", Group: "wf_fixture",
			Label: "review:quality", Verdict: "PASS", Passed: true,
			Findings: []adapter.Finding{{Level: "minor", Text: "Name the 8000 bound as a constant."}},
		},
	}
	if !reflect.DeepEqual(log.Reports, want) {
		t.Errorf("reports:\n got %+v\nwant %+v", log.Reports, want)
	}
}

func TestLogsNeverReturnToolOutput(t *testing.T) {
	log := readFixture(t)
	s := fmt.Sprintf("%+v", log)
	for _, secret := range []string{fakeSecret, "FIXTURE_SECRET_FILE_CONTENTS", "/fixture/repo/.env"} {
		if strings.Contains(s, secret) {
			t.Errorf("log holds tool output %q: %s", secret, s)
		}
	}
}

func TestLogsSkipsMalformedLines(t *testing.T) {
	if log := readFixture(t); log.Skipped < 1 {
		t.Errorf("skipped = %d, want at least 1", log.Skipped)
	}
}

func TestLogsSessionInAnotherProject(t *testing.T) {
	log, err := fixtureLogs(t).ReadSession(fixtureOther)
	if err != nil {
		t.Fatal(err)
	}
	want := []adapter.Message{{At: at("2026-11-21T09:05:00Z"), Text: "Use the glossary's words only."}}
	if log.Session != fixtureOther || !reflect.DeepEqual(log.Messages, want) {
		t.Errorf("log = %+v", log)
	}
}

func TestLogsMissingIsUnavailable(t *testing.T) {
	l := fixtureLogs(t)
	for _, id := range []string{"33333333-3333-4333-8333-333333333333", "../x", "", "a/b"} {
		_, err := l.ReadSession(id)
		if !errors.Is(err, adapter.ErrUnavailable) {
			t.Errorf("ReadSession(%q) err = %v, want ErrUnavailable", id, err)
		}
	}
	l.Root = t.TempDir()
	if _, err := l.ReadSession(fixtureLead); !errors.Is(err, adapter.ErrUnavailable) {
		t.Errorf("no projects dir: err = %v, want ErrUnavailable", err)
	}
}

func TestProjectName(t *testing.T) {
	for dir, want := range map[string]string{
		"/home/a.b/c_d":          "-home-a-b-c-d",
		"/home/rhiza/code/physa": "-home-rhiza-code-physa",
		"/tmp/é x":               "-tmp---x",
	} {
		if got := projectName(dir); got != want {
			t.Errorf("projectName(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestNewLogsRoot(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	if l := New("."); l.Root != "/cfg" || !filepath.IsAbs(l.dir) {
		t.Errorf("logs = %+v", l)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "/home/someone")
	if l := New("/x"); l.Root != "/home/someone/.claude" {
		t.Errorf("root = %q", l.Root)
	}
}

// copyFixture copies the fixture project into a temp Root, for tests that
// add files beside it.
func copyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("testdata", "projects", "-fixture-repo")
	dst := filepath.Join(root, "projects", "-fixture-repo")
	err := filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if fi.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLogsReadsOnlyTranscripts(t *testing.T) {
	root := copyFixture(t)
	sub := filepath.Join(root, "projects", "-fixture-repo", fixtureLead, "subagents")
	report := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"x","name":"StructuredOutput","input":{"verdict":"FAIL","discrepancies":["from a file never read"]}}]},"timestamp":"2026-11-20T11:00:00Z"}` + "\n"
	for _, p := range []string{
		filepath.Join(sub, "workflows", "wf_fixture", "journal.jsonl"),
		filepath.Join(root, "projects", "-fixture-repo", fixtureLead, "tool-results", "agent-t.jsonl"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(report), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New("/fixture/repo")
	l.Root = root
	log, err := l.ReadSession(fixtureLead)
	if err != nil {
		t.Fatal(err)
	}
	if s := fmt.Sprintf("%+v", log); strings.Contains(s, "never read") {
		t.Errorf("read a file that is not a transcript: %s", s)
	}
}

func TestLogsBadSubagentFileIsSkipped(t *testing.T) {
	root := copyFixture(t)
	sub := filepath.Join(root, "projects", "-fixture-repo", fixtureLead, "subagents")
	if err := os.WriteFile(filepath.Join(sub, "agent-abad.meta.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"y","name":"StructuredOutput","input":{"verdict":"FAIL","discrepancies":["bad meta"]}}]},"timestamp":"2026-11-20T11:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(sub, "agent-abad.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New("/fixture/repo")
	l.Root = root
	log, err := l.ReadSession(fixtureLead)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Reports) != 5 {
		t.Fatalf("reports = %d, want 5: %+v", len(log.Reports), log.Reports)
	}
	if last := log.Reports[4]; last.Agent != "abad" || last.Label != "" {
		t.Errorf("no-meta report = %+v, want agent abad and no label", last)
	}
	if log.Skipped < 2 {
		t.Errorf("skipped = %d, want the bad line and the bad meta", log.Skipped)
	}
}

func TestLogsLongLinesAndBounds(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "-r")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", 3<<20)
	lines := `{"type":"user","origin":{"kind":"human"},"turnPosition":{"promptIndex":2},"message":{"content":"` + long + `"},"timestamp":"2026-11-20T10:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New("/r")
	l.Root = root
	log, err := l.ReadSession("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Messages) != 1 || len([]rune(log.Messages[0].Text)) != maxText {
		t.Errorf("messages = %d, want one bounded to %d runes", len(log.Messages), maxText)
	}
}

func TestCleanRedactsBeforeBounding(t *testing.T) {
	s := strings.Repeat("x", maxText-10) + " sk-ant-api03-AbCdEfGh12345678"
	if got := clean(s); strings.Contains(got, "sk-ant") || strings.Contains(got, "AbCdEf") {
		t.Errorf("clean kept part of a key: %q", got[len(got)-20:])
	}
}
