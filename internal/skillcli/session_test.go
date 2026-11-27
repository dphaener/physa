package skillcli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/command"
)

// listsSession matches help that lists the hidden session command among the
// group's commands, or names it in the usage line.
var listsSession = regexp.MustCompile(`(?m)^\s+session\s|physa skill session|\| session|session \|`)

const reminder = "Before changing code, run `physa map index`, then `physa map slice <capabilities>` for the ones your task touches.\n"

// mapRepo is a temporary git repo whose map holds n capabilities.
func mapRepo(t *testing.T, n int) string {
	t.Helper()
	d := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", d).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	for _, name := range []string{"alpha", "beta", "gamma", "delta"}[:n] {
		dir := filepath.Join(d, ".physa", "capabilities", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("# "+name+"\n\nPromise of "+name+".\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n == 0 {
		if err := os.MkdirAll(filepath.Join(d, ".physa"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

type sessionEnv struct {
	wd      string
	session string
	stdin   string
	current CurrentFunc
	timeout time.Duration
}

// run runs `skill session` and returns its code, stdout and stderr.
func (b sessionEnv) run(args ...string) (int, string, string) {
	e := env{
		current:         b.current,
		getwd:           func() (string, error) { return b.wd, nil },
		getenv:          func(k string) string { return map[string]string{sessionVar: b.session}[k] },
		environ:         os.Environ,
		stdin:           strings.NewReader(b.stdin),
		stdinIsTerminal: func() bool { return false },
		deadline:        b.timeout,
	}
	var out, errb bytes.Buffer
	code := command.Execute(context.Background(), command.New("physa", "", nil, nil, newCommand(e)), append([]string{"skill", "session"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func TestSessionInAMapRepoPrintsCountAndReminder(t *testing.T) {
	t.Parallel()
	code, out, errs := sessionEnv{wd: mapRepo(t, 3)}.run()
	want := "This repo has a Physa map with 3 capabilities.\n" + reminder
	if code != 0 || out != want || errs != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestSessionSaysOneCapability(t *testing.T) {
	t.Parallel()
	_, out, _ := sessionEnv{wd: mapRepo(t, 1)}.run()
	if !strings.HasPrefix(out, "This repo has a Physa map with 1 capability.\n") {
		t.Errorf("stdout %q", out)
	}
}

func TestSessionPrintsTheChangeWhenRecorded(t *testing.T) {
	t.Parallel()
	var gotWd, gotSession string
	var gotEnv []string
	wd := mapRepo(t, 2)
	cur := func(w, s string, e []string) (string, string, error) {
		gotWd, gotSession, gotEnv = w, s, e
		return "ship-it", "first-step", nil
	}
	code, out, _ := sessionEnv{wd: wd, session: "sess-1", current: cur}.run()
	want := "This repo has a Physa map with 2 capabilities.\nThis session works on ship-it/first-step.\n" + reminder
	if code != 0 || out != want || gotWd != wd || gotSession != "sess-1" || !slices.Equal(gotEnv, os.Environ()) {
		t.Errorf("code %d, stdout %q, asked %q %q with %d variables", code, out, gotWd, gotSession, len(gotEnv))
	}
}

func TestSessionOmitsTheChangeLineWhenNoneIsRecorded(t *testing.T) {
	t.Parallel()
	cur := func(string, string, []string) (string, string, error) { return "", "", nil }
	_, out, _ := sessionEnv{wd: mapRepo(t, 2), session: "sess-1", current: cur}.run()
	if strings.Contains(out, "works on") || !strings.HasSuffix(out, reminder) {
		t.Errorf("stdout %q", out)
	}
}

func TestSessionTakesTheSessionFromStdinWhenTheEnvironmentHasNone(t *testing.T) {
	t.Parallel()
	var got string
	cur := func(_, s string, _ []string) (string, string, error) { got = s; return "p", "c", nil }
	_, out, _ := sessionEnv{wd: mapRepo(t, 2), stdin: `{"session_id":"from-stdin","cwd":"/x"}`, current: cur}.run()
	if got != "from-stdin" || !strings.Contains(out, "p/c") {
		t.Errorf("session %q, stdout %q", got, out)
	}
	got = ""
	_, out, _ = sessionEnv{wd: mapRepo(t, 2), stdin: `{not json`, current: cur}.run()
	if got != "" || !strings.HasSuffix(out, reminder) || strings.Contains(out, "works on") {
		t.Errorf("bad JSON: session %q, stdout %q", got, out)
	}
}

func TestSessionPrintsNothingOutsideAMap(t *testing.T) {
	t.Parallel()
	notRepo := t.TempDir()
	if err := os.Mkdir(filepath.Join(notRepo, ".physa"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoNoMap := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repoNoMap).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	fileMap := t.TempDir()
	if err := os.WriteFile(filepath.Join(fileMap, ".physa"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, wd := range map[string]string{"empty dir": t.TempDir(), ".physa but no git": notRepo, "git but no .physa": repoNoMap, ".physa is a file": fileMap, "missing dir": "/nonexistent/physa-session"} {
		code, out, errs := sessionEnv{wd: wd}.run()
		if code != 0 || out != "" || errs != "" {
			t.Errorf("%s: code %d, stdout %q, stderr %q", name, code, out, errs)
		}
	}
}

func TestSessionPrintsNothingWhenCurrentFails(t *testing.T) {
	t.Parallel()
	cur := func(string, string, []string) (string, string, error) { return "p", "c", errors.New("broken store") }
	code, out, errs := sessionEnv{wd: mapRepo(t, 2), session: "sess-1", current: cur}.run()
	if code != 0 || out != "" || errs != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestSessionPrintsNothingWhenCurrentBlocksPastTheDeadline(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	cur := func(string, string, []string) (string, string, error) { <-release; return "p", "c", nil }
	start := time.Now()
	code, out, errs := sessionEnv{wd: mapRepo(t, 2), session: "sess-1", current: cur, timeout: 100 * time.Millisecond}.run()
	if code != 0 || out != "" || errs != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v with a 100ms deadline", took)
	}
}

func TestSessionTakesNoArguments(t *testing.T) {
	t.Parallel()
	code, out, _ := sessionEnv{wd: mapRepo(t, 2)}.run("extra")
	if code != 2 || out != "" {
		t.Errorf("code %d, stdout %q", code, out)
	}
}

func TestSessionIsHiddenFromHelp(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, args := range [][]string{{"--help"}, {}} {
		_, out, errs := h.run(args...)
		if listsSession.MatchString(out + errs) {
			t.Errorf("skill %v lists session: %q", args, out+errs)
		}
	}
	code, out, _ := h.run("session", "--help")
	if code != 0 || !strings.Contains(out, "physa skill session") {
		t.Errorf("session --help: code %d, %q", code, out)
	}
}
