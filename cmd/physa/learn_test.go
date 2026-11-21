package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/forge"
)

// prForge answers one change with a PR, and no PR for the others.
type prForge struct {
	change string
	pr     forge.PR
}

func (f prForge) Name() string               { return "fake" }
func (f prForge) PRExists(int) (bool, error) { return false, nil }
func (f prForge) ChangePR(id string) (forge.PR, bool, error) {
	return f.pr, id == f.change, nil
}

func runLearn(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"map", "learn"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func learnRepo(t *testing.T) (dir, head string) {
	t.Helper()
	dir = trailersRepo(t)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitIn(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return dir, strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD~3"))
}

func TestLearnText(t *testing.T) {
	dir, id := learnRepo(t)
	t.Chdir(dir)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := prForge{change: id, pr: forge.PR{
		Number:   7,
		Comments: []forge.Comment{{Author: "ana", At: at, Kind: forge.KindReview, Body: "rename this"}},
		Commits:  []forge.Commit{{ID: id, At: at.Add(time.Hour)}},
	}}
	old := newForge
	newForge = func(string) forge.Forge { return f }
	t.Cleanup(func() { newForge = old })
	code, out, errOut := runLearn(t, "--signals")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"Forge: fake", "correction", "feat: ok", "Nothing to learn"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
}

func TestLearnJSON(t *testing.T) {
	dir, _ := learnRepo(t)
	t.Chdir(dir)
	code, out, _ := runLearn(t, "--signals", "--json")
	var got struct{ Version int }
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || got.Version != 1 {
		t.Errorf("code %d, version %d, stdout %q", code, got.Version, out)
	}
}

func TestLearnForgeUnavailable(t *testing.T) {
	dir, _ := learnRepo(t)
	t.Chdir(dir)
	useNoGH(t)
	code, out, _ := runLearn(t, "--signals")
	if code != 0 || !strings.Contains(out, "Forge: unavailable") {
		t.Errorf("code %d, stdout %q", code, out)
	}
}

func TestLearnOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	if code, _, errOut := runLearn(t, "--signals"); code != 2 || !strings.HasPrefix(errOut, "physa map learn: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestLearnBadSince(t *testing.T) {
	dir, _ := learnRepo(t)
	t.Chdir(dir)
	code, _, errOut := runLearn(t, "--signals", "--since=nosuch")
	if code != 2 || !strings.HasPrefix(errOut, "physa map learn: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestLearnBadUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--since"}, {"--nosuch"}, {"x"}, {"--appetite", "0"}, {"--appetite=x"}, {"--appetite"},
		{"--appetite=-3"}, {"--agent"}, {"--agent="}, {"--signals", "--appetite", "5"}, {"--signals", "--agent=claude"},
	} {
		if _, ok := parseLearn(args); ok {
			t.Errorf("parseLearn(%v) ok, want not", args)
		}
	}
	o, ok := parseLearn([]string{"--signals", "--since", "abc", "--json"})
	if !ok || o.since != "abc" || !o.json || !o.signals {
		t.Errorf("got %+v, %v", o, ok)
	}
}

func TestLearnHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run([]string{"map", "learn", "--help"}, &stdout, &stderr)
	if !strings.Contains(stdout.String()+stderr.String(), "physa map learn [--signals] [--since <change>] [--appetite <tokens>] [--agent <name>] [--json]") {
		t.Errorf("help lacks usage: %q %q", stdout.String(), stderr.String())
	}
}

// unexpectedAgent is what TestMain installs as claude: starting it is a bug
// in the test, and stops the binary.
type unexpectedAgent struct{}

func (unexpectedAgent) Name() string { return "unexpected" }
func (unexpectedAgent) Start(context.Context, adapter.Spec) (adapter.Session, error) {
	fmt.Fprintln(os.Stderr, "a test started the real agent")
	os.Exit(3)
	return nil, nil
}

// fakeAgent replays scripted usage and text, and records its starts.
type fakeAgent struct {
	startErr error
	waitErr  error
	reports  []adapter.Tokens
	result   adapter.Result
	starts   int
}

func (f *fakeAgent) Name() string { return "fake" }
func (f *fakeAgent) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	f.starts++
	if f.startErr != nil {
		return nil, f.startErr
	}
	return fakeSession{f: f, usage: s.Usage}, nil
}

type fakeSession struct {
	f     *fakeAgent
	usage func(adapter.Tokens)
}

func (s fakeSession) Wait() (adapter.Result, error) {
	for _, t := range s.f.reports {
		s.usage(t)
	}
	return s.f.result, s.f.waitErr
}

// useAgent installs f as the agent called name for one test.
func useAgent(t *testing.T, name string, f *fakeAgent) {
	t.Helper()
	old, had := agents[name]
	agents[name] = func() adapter.Adapter { return f }
	t.Cleanup(func() {
		if had {
			agents[name] = old
		} else {
			delete(agents, name)
		}
	})
}

// signalRepo is a learn repo where the change "feat: ok" has a correction
// signal; it returns the repo and that change's short id.
func signalRepo(t *testing.T) (dir, short string) {
	t.Helper()
	dir, id := learnRepo(t)
	t.Chdir(dir)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := prForge{change: id, pr: forge.PR{
		Number:   7,
		Comments: []forge.Comment{{Author: "ana", At: at, Kind: forge.KindReview, Body: "rename this"}},
		Commits:  []forge.Commit{{ID: id, At: at.Add(time.Hour)}},
	}}
	old := newForge
	newForge = func(string) forge.Forge { return f }
	t.Cleanup(func() { newForge = old })
	_, out, _ := runLearn(t, "--signals", "--json")
	var res struct {
		Learn []struct{ Short string }
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Learn) != 1 {
		t.Fatalf("signals: %v, %q", err, out)
	}
	return dir, res.Learn[0].Short
}

// answer is a session's text with one good rule and one proposal naming a
// capability the map lacks.
func answer(short string) string {
	good := fmt.Sprintf(`{"kind":"rule","capability":"a","section":"Rules","signal":{"kind":"correction","changes":[%q]},"changes":[%q],"entry":"### Rename on review\n`+"`rename-on-review`"+` · %s\nA name a reviewer asked to change is changed before merge.\nBecause: the review said so."}`, short, short, short)
	bad := fmt.Sprintf(`{"kind":"rule","capability":"nosuch","section":"Rules","signal":{"kind":"correction","changes":[%q]},"changes":[%q],"entry":"### X\n`+"`x`"+` · %s\nX.\nBecause: y."}`, short, short, short)
	return "Thinking.\n```json\n{\"proposals\":[" + good + "," + bad + "]}\n```\n"
}

func branches(t *testing.T, dir string) string { return gitIn(t, dir, "branch", "--list") }

func TestLearnNoSignals(t *testing.T) {
	dir, _ := learnRepo(t)
	t.Chdir(dir) // the default fake GitHub knows no PR, so no signal; claude must never start
	code, out, errOut := runLearn(t)
	if code != 0 || !strings.Contains(out, "no session started") || strings.Contains(errOut, "appetite") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestLearnDraftKeepsAndDrops(t *testing.T) {
	dir, short := signalRepo(t)
	before, status := branches(t, dir), gitIn(t, dir, "status", "--porcelain")
	f := &fakeAgent{
		reports: []adapter.Tokens{{Input: 100, Output: 20}},
		result:  adapter.Result{Text: answer(short), Tokens: adapter.Tokens{Input: 100, Output: 20}},
	}
	useAgent(t, "claude", f)
	code, out, errOut := runLearn(t, "--appetite", "5000")
	if code != 0 || f.starts != 1 {
		t.Fatalf("code %d, starts %d, stderr %q", code, f.starts, errOut)
	}
	for _, want := range []string{"rename-on-review", "nosuch"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if want := "physa map learn: appetite 5000 tokens; batch of 1 signals on 1 changes, 1 capabilities\n"; errOut != want {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}
	if got := branches(t, dir); got != before {
		t.Errorf("branches changed: %q -> %q", before, got)
	}
	if got := gitIn(t, dir, "status", "--porcelain"); got != status {
		t.Errorf("working tree changed: %q", got)
	}
}

func TestLearnDraftJSON(t *testing.T) {
	_, short := signalRepo(t)
	useAgent(t, "claude", &fakeAgent{result: adapter.Result{Text: answer(short), Tokens: adapter.Tokens{Input: 100, Output: 20}}})
	code, out, _ := runLearn(t, "--json")
	var got struct {
		Version int
		Session string
		Kept    []json.RawMessage
		Dropped []json.RawMessage
		Tokens  struct{ Input, Output int64 }
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != 0 {
		t.Fatalf("code %d, err %v, stdout %q", code, err, out)
	}
	if got.Version != 1 || got.Session != "ran" || len(got.Kept) != 1 || len(got.Dropped) != 1 || got.Tokens.Input != 100 || got.Tokens.Output != 20 {
		t.Errorf("got %+v", got)
	}
}

func TestLearnUnreadableAnswer(t *testing.T) {
	signalRepo(t)
	useAgent(t, "claude", &fakeAgent{result: adapter.Result{Text: "no block here"}})
	code, out, _ := runLearn(t)
	if code != 0 || !strings.Contains(out, "the session's output: ") {
		t.Errorf("code %d, stdout %q", code, out)
	}
}

func TestLearnBreaker(t *testing.T) {
	dir, short := signalRepo(t)
	before := branches(t, dir)
	useAgent(t, "claude", &fakeAgent{
		reports: []adapter.Tokens{{Input: 900, Output: 200}},
		result:  adapter.Result{Text: answer(short)},
	})
	code, out, _ := runLearn(t, "--appetite=1000")
	if code != 1 || !strings.Contains(out, "nothing kept") || strings.Contains(out, "rename-on-review") {
		t.Errorf("code %d, stdout %q", code, out)
	}
	if got := branches(t, dir); got != before {
		t.Errorf("branches changed: %q -> %q", before, got)
	}
}

func TestLearnAgentChoice(t *testing.T) {
	signalRepo(t)
	for name, run := range map[string]func(t *testing.T) (int, string, string){
		"flag": func(t *testing.T) (int, string, string) { return runLearn(t, "--agent", "nosuch") },
		"env": func(t *testing.T) (int, string, string) {
			t.Setenv("PHYSA_AGENT", "nosuch")
			return runLearn(t)
		},
	} {
		code, out, errOut := run(t)
		if code != 2 || out != "" || !strings.Contains(errOut, `unknown agent "nosuch" (known: claude)`) {
			t.Errorf("%s: code %d, stdout %q, stderr %q", name, code, out, errOut)
		}
	}
	other := &fakeAgent{result: adapter.Result{Text: "```json\n{\"proposals\":[]}\n```"}}
	useAgent(t, "other", other)
	if code, _, errOut := runLearn(t, "--agent=other"); code != 0 || other.starts != 1 {
		t.Errorf("code %d, starts %d, stderr %q", code, other.starts, errOut)
	}
}

func TestLearnAgentFailures(t *testing.T) {
	signalRepo(t)
	for _, err := range []error{adapter.ErrNotInstalled, fmt.Errorf("%w: boom", adapter.ErrFailed)} {
		useAgent(t, "claude", &fakeAgent{startErr: err})
		code, out, errOut := runLearn(t)
		if code != 2 || out != "" || !strings.Contains(errOut, err.Error()) {
			t.Errorf("%v: code %d, stdout %q, stderr %q", err, code, out, errOut)
		}
	}
	useAgent(t, "claude", &fakeAgent{waitErr: fmt.Errorf("%w: boom", adapter.ErrFailed)})
	if code, _, errOut := runLearn(t); code != 2 || !strings.Contains(errOut, "boom") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestLearnBadUsageFromRun(t *testing.T) {
	for _, args := range [][]string{{"--appetite", "0"}, {"--appetite", "x"}, {"--signals", "--appetite", "5"}} {
		if code, _, _ := runLearn(t, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}
