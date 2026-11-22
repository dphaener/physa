//go:build linux || darwin

package betcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/apiclient"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/enginestart"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/workapi"
)

const goodPitch = "# Try a quiet mode\n## Problem\nToo noisy.\n## Appetite\n200k\n## Solution\nA flag.\n## Scope\nCLI only.\n## Signal\nFewer complaints.\n## Rabbit holes\nColour themes.\n## No-gos\nNo config file.\n"

// TestMain never lets the test binary act as an engine.
func TestMain(m *testing.M) {
	if i := slices.Index(os.Args, "engine"); i >= 0 && i+1 < len(os.Args) && os.Args[i+1] == "run" {
		fmt.Fprintln(os.Stderr, "betcli test binary: refusing to run as an engine")
		os.Exit(3)
	}
	os.Exit(m.Run())
}

type tenv struct {
	t   *testing.T
	e   *ideacli.Env
	env map[string]string
	b   bets
}

// newTest runs an engine with the idea and bet methods in this process on a
// short temporary settings directory and works in a fresh git repo. The
// person is "tester", nothing is interactive and nothing may spawn an engine.
func newTest(t *testing.T) *tenv {
	t.Helper()
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	t.Setenv("XDG_CONFIG_HOME", tmp)
	repo := filepath.Join(tmp, "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Chdir(repo)
	dir := filepath.Join(tmp, "physa")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- engine.Run(ctx, engine.Options{Dir: dir, Idle: time.Minute, Version: "test", Methods: workapi.Methods})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after its context was cancelled")
		}
	})
	p, err := engine.PathsIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		dctx, dcancel := context.WithTimeout(context.Background(), time.Second)
		c, err := apiclient.Dial(dctx, p.Socket)
		if err == nil {
			err = c.Call(dctx, "engine.status", nil, nil)
			c.Close()
		}
		dcancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine did not answer within 5s: %v", err)
		}
	}
	te := &tenv{t: t, env: map[string]string{}}
	e := ideacli.New(nil)
	e.Person = func(string, forge.Forge, io.Writer) string { return "tester" }
	e.Start = enginestart.Options{Command: func(dir string, _ time.Duration) *exec.Cmd {
		t.Errorf("a command tried to start an engine on %s", dir)
		return exec.Command("false")
	}}
	e.Getenv = func(k string) string { return te.env[k] }
	e.IsInteractive = func() bool { return false }
	e.Pick = func(string, []picker.Item) (string, error) { t.Error("Pick called"); return "", picker.ErrAborted }
	e.Confirm = func(string) (bool, error) { t.Error("Confirm called"); return false, picker.ErrAborted }
	e.Ask = func(string, string, func(string) error) (string, error) {
		t.Error("Ask called")
		return "", picker.ErrAborted
	}
	e.Stdin = strings.NewReader("")
	te.e = e
	te.b = bets{e}
	return te
}

func (te *tenv) bet(args ...string) (int, string, string) {
	te.t.Helper()
	var out, errb bytes.Buffer
	root := &cli.Command{Name: "physa", Subs: []*cli.Command{commandFor(te.e)}}
	code := cli.Dispatch(root, append([]string{"bet"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func (te *tenv) must(args ...string) string {
	te.t.Helper()
	code, out, errs := te.bet(args...)
	if code != 0 {
		te.t.Fatalf("%v: code %d, stderr %q", args, code, errs)
	}
	return out
}

func (te *tenv) fails(args ...string) string {
	te.t.Helper()
	code, out, errs := te.bet(args...)
	if code != 2 || out != "" {
		te.t.Fatalf("%v: code %d, stdout %q; want 2 and nothing", args, code, out)
	}
	return errs
}

// idea adds an idea through the engine and, when pitch is not empty, shapes it.
func (te *tenv) idea(text, slug, pitchMd string) {
	te.t.Helper()
	var out workapi.IdeaOut
	repo, err := te.e.Repo()
	if err != nil {
		te.t.Fatal(err)
	}
	if err := te.e.Call("idea.add", workapi.AddIn{Repo: repo, Text: text, Slug: slug, Person: "ann"}, &out); err != nil {
		te.t.Fatal(err)
	}
	if pitchMd != "" {
		if err := te.e.Call("idea.shape", workapi.ShapeIn{Repo: repo, Slug: out.Slug, Markdown: pitchMd, Person: "ann"}, &out); err != nil {
			te.t.Fatal(err)
		}
	}
}

func (te *tenv) list() []workapi.BetOut {
	te.t.Helper()
	var bs []workapi.BetOut
	if err := json.Unmarshal([]byte(te.must("list", "--json")), &bs); err != nil {
		te.t.Fatal(err)
	}
	return bs
}

func TestBetWithAppetiteAndFromThePitch(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "", "")
	te.idea("Show bets on the phone", "phone-bets", goodPitch)
	out := te.must("try-a-quiet-mode", "--appetite", "1.5m")
	if !strings.Contains(out, "Bet try-a-quiet-mode") || !strings.Contains(out, "1.5m") {
		t.Errorf("bet output %q", out)
	}
	var o workapi.BetOut
	if err := json.Unmarshal([]byte(te.must("phone-bets", "--json")), &o); err != nil {
		t.Fatal(err)
	}
	if o.Appetite != 200000 || o.State != "active" || o.Person != "tester" {
		t.Errorf("bet from the pitch: %+v", o)
	}
}

func TestSecondActiveBetRefused(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "", "")
	te.must("try-a-quiet-mode", "--appetite", "200k")
	errs := te.fails("try-a-quiet-mode", "--appetite", "100k")
	if !strings.Contains(errs, "try-a-quiet-mode") || len(te.list()) != 1 {
		t.Errorf("stderr %q, bets %d", errs, len(te.list()))
	}
}

func TestNoAppetiteOffTerminalIsBadUsage(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "", "")
	errs := te.fails("try-a-quiet-mode")
	if !strings.Contains(errs, "--appetite") || len(te.list()) != 0 {
		t.Errorf("stderr %q", errs)
	}
	if errs := te.fails("try-a-quiet-mode", "--appetite", "lots"); !strings.Contains(errs, "lots") {
		t.Errorf("stderr %q", errs)
	}
}

func TestAppetiteAskedOnTerminal(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "", "")
	te.e.IsInteractive = func() bool { return true }
	var validate func(string) error
	te.e.Ask = func(_, _ string, v func(string) error) (string, error) { validate = v; return "300k", nil }
	te.e.Pick = func(_ string, items []picker.Item) (string, error) { return items[0].Slug, nil }
	te.must()
	if validate == nil || validate("lots") == nil || validate("300k") != nil {
		t.Error("the appetite was not validated with ParseTokens")
	}
	if bs := te.list(); len(bs) != 1 || bs[0].Appetite != 300000 {
		t.Errorf("bets %+v", bs)
	}
}

func TestListAndShow(t *testing.T) {
	te := newTest(t)
	if out := te.must("list"); !strings.Contains(out, "No bets yet") {
		t.Errorf("empty list %q", out)
	}
	te.idea("Try a quiet mode", "", goodPitch)
	te.idea("Show bets on the phone", "phone-bets", "")
	te.must("try-a-quiet-mode")
	te.must("phone-bets", "--appetite", "50k")
	out := te.must("list")
	if strings.Contains(out, "\x1b") || strings.Index(out, "phone-bets") > strings.Index(out, "try-a-quiet-mode") {
		t.Errorf("list should be plain and newest first:\n%s", out)
	}
	for _, want := range []string{"Slug", "Idea", "Appetite", "Spent", "State", "Person", "200k", "50k", "active", "tester"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if bs := te.list(); len(bs) != 2 || bs[0].Slug != "phone-bets" {
		t.Errorf("json list %+v", bs)
	}
	show := te.must("show", "try-a-quiet-mode")
	for _, want := range []string{"Try a quiet mode", "state", "active", "Pitch", "Rabbit holes", "Colour themes."} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %q:\n%s", want, show)
		}
	}
	var o workapi.BetOut
	if err := json.Unmarshal([]byte(te.must("show", "try-a-quiet-mode", "--json")), &o); err != nil || o.Pitch == nil {
		t.Errorf("show json %+v, %v", o, err)
	}
}

func TestStopWithReasonAndStopTwice(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "", "")
	te.must("try-a-quiet-mode", "--appetite", "200k")
	out := te.must("stop", "try-a-quiet-mode", "--reason", "learned enough")
	if !strings.Contains(out, "Stopped bet try-a-quiet-mode") {
		t.Errorf("stop output %q", out)
	}
	show := te.must("show", "try-a-quiet-mode")
	if !strings.Contains(show, "stopped") || !strings.Contains(show, "stop reason  learned enough") {
		t.Errorf("show:\n%s", show)
	}
	if errs := te.fails("stop", "try-a-quiet-mode"); !strings.Contains(errs, "try-a-quiet-mode") {
		t.Errorf("stopping a stopped bet: %q", errs)
	}
	// The idea may be bet on again once its bet has stopped.
	te.must("try-a-quiet-mode", "--appetite", "10k")
}

func TestMissingSlugOffTerminalListsChoices(t *testing.T) {
	te := newTest(t)
	if errs := te.fails("show"); !strings.Contains(errs, "name a bet; there are none yet") {
		t.Errorf("no bets: %q", errs)
	}
	te.idea("Try a quiet mode", "", "")
	te.idea("Show bets on the phone", "phone-bets", "")
	te.must("phone-bets", "--appetite", "1k")
	errs := te.fails()
	want := "physa bet: name an idea; the ideas are:\n  try-a-quiet-mode  Try a quiet mode\nusage: physa bet"
	if !strings.HasPrefix(errs, want) {
		t.Errorf("ideas without an active bet:\n%s", errs)
	}
	if errs := te.fails("stop"); !strings.Contains(errs, "phone-bets  Show bets on the phone") || strings.Contains(errs, "try-a") {
		t.Errorf("active bets:\n%s", errs)
	}
}

func TestAgentSessionRefusedBeforeAnything(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "", "")
	te.must("try-a-quiet-mode", "--appetite", "1k")
	te.env["CLAUDE_CODE_SESSION_ID"], te.env["CLAUDECODE"] = "x", "1"
	te.e.Person = func(string, forge.Forge, io.Writer) string { t.Error("person asked"); return "" }
	for _, args := range [][]string{{"try-a-quiet-mode", "--appetite", "1k"}, {"--bogus"}} {
		if errs := te.fails(args...); !strings.Contains(errs, "A person must make this bet: physa is running inside a Claude Code session.") {
			t.Errorf("%v: %q", args, errs)
		}
	}
	if errs := te.fails("stop", "try-a-quiet-mode", "--yes"); !strings.Contains(errs, "A person must stop this bet: physa is running inside a Claude Code session.") {
		t.Errorf("stop: %q", errs)
	}
	if bs := te.list(); len(bs) != 1 || bs[0].State != "active" {
		t.Errorf("an agent changed bets: %+v", bs)
	}
	te.must("show", "try-a-quiet-mode")
}

func TestStopConfirmedOnTerminal(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "", "")
	te.must("try-a-quiet-mode", "--appetite", "1k")
	te.e.IsInteractive = func() bool { return true }
	var asked string
	answer := false
	te.e.Confirm = func(q string) (bool, error) { asked = q; return answer, nil }
	if errs := te.fails("stop", "try-a-quiet-mode"); !strings.Contains(errs, "not stopped") || asked != "Stop bet try-a-quiet-mode?" {
		t.Errorf("declined: %q, asked %q", errs, asked)
	}
	te.e.Confirm = func(string) (bool, error) { return false, picker.ErrAborted }
	te.fails("stop", "try-a-quiet-mode")
	if te.list()[0].State != "active" {
		t.Error("a declined confirm stopped the bet")
	}
	asked, answer = "", true
	te.e.Confirm = func(q string) (bool, error) { asked = q; return answer, nil }
	te.must("stop", "try-a-quiet-mode")
	if te.list()[0].State != "stopped" {
		t.Error("a confirmed stop did not stop the bet")
	}
	te.must("try-a-quiet-mode", "--appetite", "1k")
	te.e.Confirm = func(string) (bool, error) { t.Error("--yes still asked"); return false, nil }
	te.must("stop", "try-a-quiet-mode-2", "--yes")
}

const twoChanges = `{"changes":[
 {"name":"first-step","capability":"alpha","what":"Do the first.","acceptance":["failure case: a bad input exits 2"],"must_not":["touch beta"],"after":[]},
 {"name":"second-step","capability":"beta","what":"Do the second.","acceptance":["no failure case: it only reads"],"must_not":[],"after":["first-step"]}]}`

// planRepo gives the test repo a map of two capabilities, a first commit and
// an active bet of 100k on the idea "try-a-quiet-mode".
func (te *tenv) planRepo() {
	te.t.Helper()
	repo, err := te.e.Repo()
	if err != nil {
		te.t.Fatal(err)
	}
	for _, c := range []string{"alpha", "beta"} {
		dir := filepath.Join(repo, ".physa", "capabilities", c)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			te.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("# "+c+"\n\nPromise of "+c+".\n"), 0o644); err != nil {
			te.t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "chore: map"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			te.t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	te.idea("Try a quiet mode", "", "")
	te.must("try-a-quiet-mode", "--appetite", "100k")
}

func (te *tenv) planFile(body string) string {
	te.t.Helper()
	p := filepath.Join(te.t.TempDir(), "plan.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		te.t.Fatal(err)
	}
	return p
}

func TestPlanSetFromFileAndStdinThenShow(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	out := te.must("plan", "set", "try-a-quiet-mode", "--from", te.planFile(twoChanges))
	if out != "Plan for try-a-quiet-mode set: revision 1, 2 changes\n" {
		t.Errorf("plan set: %q", out)
	}
	te.e.Stdin = strings.NewReader(twoChanges)
	if errs := te.fails("plan", "set", "try-a-quiet-mode"); !strings.Contains(errs, "needs a reason") {
		t.Errorf("second revision without reason: %q", errs)
	}
	te.e.Stdin = strings.NewReader(twoChanges)
	out = te.must("plan", "set", "try-a-quiet-mode", "--reason", "missed a step")
	if out != "Plan for try-a-quiet-mode set: revision 2, 2 changes, 1 replan\n" {
		t.Errorf("replan: %q", out)
	}
	te.e.Stdin = strings.NewReader(twoChanges)
	var po workapi.PlanOut
	if err := json.Unmarshal([]byte(te.must("plan", "set", "try-a-quiet-mode", "--reason", "again", "--from", "-", "--json")), &po); err != nil || po.N != 3 || po.Replans != 2 {
		t.Errorf("json plan set %+v, %v", po, err)
	}
	show := te.must("plan", "show", "try-a-quiet-mode")
	for _, want := range []string{"Plan for try-a-quiet-mode", "revision", "again", "first-step", "capability  alpha", "failure case: a bad input exits 2", "touch beta", "after       first-step"} {
		if !strings.Contains(show, want) {
			t.Errorf("plan show lacks %q:\n%s", want, show)
		}
	}
	if strings.Contains(show, "History") {
		t.Errorf("plan show without --history:\n%s", show)
	}
	hist := te.must("plan", "show", "try-a-quiet-mode", "--history")
	if !strings.Contains(hist, "History") {
		t.Errorf("history:\n%s", hist)
	}
	if i, j := strings.Index(hist, "reason: missed a step"), strings.Index(hist, "reason: again"); i < 0 || j < i {
		t.Errorf("history is not oldest first:\n%s", hist)
	}
}

func TestPlanSetRefusals(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	bad := `{"changes":[{"name":"a","capability":"alpha, beta","what":"x","acceptance":["failure case: y"],"after":[]}]}`
	errs := te.fails("plan", "set", "try-a-quiet-mode", "--from", te.planFile(bad))
	if !strings.HasPrefix(errs, "physa bet plan set: the plan has problems:") || !strings.Contains(errs, "names two capabilities") {
		t.Errorf("bad plan: %q", errs)
	}
	// Nothing on stdin and no --from off a terminal is an empty plan: bad usage.
	if errs := te.fails("plan", "set", "try-a-quiet-mode"); !strings.Contains(errs, "plan is empty") {
		t.Errorf("empty stdin: %q", errs)
	}
	te.e.IsInteractive = func() bool { return true }
	if errs := te.fails("plan", "set", "try-a-quiet-mode"); !strings.Contains(errs, "--from") {
		t.Errorf("terminal without --from: %q", errs)
	}
	te.e.IsInteractive = func() bool { return false }
	if errs := te.fails("plan", "set", "--from", te.planFile(twoChanges), "--bogus"); errs == "" {
		t.Error("bogus flag accepted")
	}
	if errs := te.fails("plan", "show", "try-a-quiet-mode"); !strings.Contains(errs, "try-a-quiet-mode") {
		t.Errorf("show without a plan: %q", errs)
	}
}

func TestPlanSetMissingBetListsActiveBets(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	te.e.Stdin = strings.NewReader(twoChanges)
	errs := te.fails("plan", "set")
	if !strings.Contains(errs, "name a bet; the bets are:") || !strings.Contains(errs, "try-a-quiet-mode") {
		t.Errorf("missing slug: %q", errs)
	}
}

func TestPlanAndWorkAllowedInAgentSessions(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	te.env["CLAUDE_CODE_SESSION_ID"], te.env["CLAUDECODE"] = "sess-1", "1"
	te.must("plan", "set", "try-a-quiet-mode", "--from", te.planFile(twoChanges))
	te.must("work", "try-a-quiet-mode/first-step")
}

func TestWorkAndCurrent(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	te.must("plan", "set", "try-a-quiet-mode", "--from", te.planFile(twoChanges))
	te.env["CLAUDE_CODE_SESSION_ID"] = "sess-1"
	if out := te.must("work", "--current"); out != "" {
		t.Errorf("--current before work: %q", out)
	}
	if out := te.must("work", "try-a-quiet-mode/first-step"); out != "Session sess-1 works on try-a-quiet-mode/first-step\n" {
		t.Errorf("work: %q", out)
	}
	if out := te.must("work", "--current"); out != "try-a-quiet-mode/first-step\n" {
		t.Errorf("--current: %q", out)
	}
	var wo workapi.WorkOut
	if err := json.Unmarshal([]byte(te.must("work", "try-a-quiet-mode/second-step", "--json")), &wo); err != nil || wo.Change != "second-step" {
		t.Errorf("work json %+v, %v", wo, err)
	}
	if out := te.must("work", "--current"); out != "try-a-quiet-mode/second-step\n" {
		t.Errorf("--current after a second work: %q", out)
	}
	te.env["CLAUDE_CODE_SESSION_ID"] = "other"
	if out := te.must("work", "--current"); out != "" {
		t.Errorf("--current for another session: %q", out)
	}
	te.env["CLAUDE_CODE_SESSION_ID"] = "sess-1"
	if errs := te.fails("work", "try-a-quiet-mode/nope"); !strings.Contains(errs, "nope") {
		t.Errorf("unknown change: %q", errs)
	}
	te.env["CLAUDE_CODE_SESSION_ID"] = ""
	if out := te.must("work", "--current"); out != "" {
		t.Errorf("--current with no session: %q", out)
	}
}

func TestWorkWithoutSessionAndBadArguments(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	want := "physa bet work: no agent session: run it inside the session that works on the change\n"
	if errs := te.fails("work", "try-a-quiet-mode/first-step"); errs != want {
		t.Errorf("no session: %q", errs)
	}
	te.env["CLAUDE_CODE_SESSION_ID"] = "sess-1"
	for _, args := range [][]string{{"work"}, {"work", "nochange"}, {"work", "a/"}, {"work", "/b"}, {"work", "a/b/c"}, {"work", "a/b", "--current"}, {"work", "--current", "--json"}} {
		code, out, _ := te.bet(args...)
		if code != 2 || out != "" {
			t.Errorf("%v: code %d, stdout %q", args, code, out)
		}
	}
}

func TestCurrentWithNoStoreOrRepoPrintsNothing(t *testing.T) {
	te := newTest(t)
	te.env["CLAUDE_CODE_SESSION_ID"] = "sess-1"
	// A repo whose engine never ran has no store, and --current must not make one.
	repo, err := te.e.Repo()
	if err != nil {
		t.Fatal(err)
	}
	code, out, errs := te.bet("work", "--current")
	if code != 0 || out != "" || errs != "" {
		t.Errorf("no store: %d %q %q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "physa")); !os.IsNotExist(err) {
		t.Errorf("--current made a store: %v", err)
	}
	t.Chdir(t.TempDir())
	if code, out, errs := te.bet("work", "--current"); code != 0 || out != "" || errs != "" {
		t.Errorf("not a repo: %d %q %q", code, out, errs)
	}
}

func TestShowProgressAsFactsAndNeedsYou(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	plain := te.must("show", "try-a-quiet-mode")
	if !strings.Contains(plain, "Progress") || !strings.Contains(plain, "planned       none yet") || strings.Contains(plain, "landed") {
		t.Errorf("no plan:\n%s", plain)
	}
	te.must("plan", "set", "try-a-quiet-mode", "--from", te.planFile(twoChanges))
	te.env["CLAUDE_CODE_SESSION_ID"] = "sess-1"
	te.must("work", "try-a-quiet-mode/first-step")
	spend := func(change string, tokens int64) {
		t.Helper()
		repo, _ := te.e.Repo()
		in := workapi.SpendIn{Repo: repo, Bet: "try-a-quiet-mode", Change: change, Session: "sess-1", Tokens: tokens}
		if err := te.e.Call("bet.spend", in, nil); err != nil {
			t.Fatal(err)
		}
	}
	spend("first-step", 30000)
	out := te.must("show", "try-a-quiet-mode")
	for _, want := range []string{"Progress", "planned       2 changes", "landed        0 of 2: none", "in work       first-step",
		"asks open     none yet", "spent         30k of 100k", "replans       0", "Spend per change", "first-step  30k"} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Needs you") {
		t.Errorf("Needs you before 80%%:\n%s", out)
	}
	spend("", 55000)
	out = te.must("show", "try-a-quiet-mode")
	if !strings.Contains(out, "Needs you") || !strings.Contains(out, "has spent 85k of its 100k appetite") || !strings.Contains(out, "(bet)  55k") {
		t.Errorf("after 80%%:\n%s", out)
	}
	spend("first-step", 45000)
	out = te.must("show", "try-a-quiet-mode")
	if !strings.Contains(out, "the circuit breaker stopped the bet") || !strings.Contains(out, "stopped") {
		t.Errorf("after 100%%:\n%s", out)
	}
	if strings.Contains(out, "%") {
		t.Errorf("a percentage in the output:\n%s", out)
	}
	var o workapi.BetOut
	if err := json.Unmarshal([]byte(te.must("show", "try-a-quiet-mode", "--json")), &o); err != nil || o.Progress == nil || len(o.NeedsYou) != 2 {
		t.Errorf("show json %+v, %v", o, err)
	}
}

func TestHelpListsPlanAndWork(t *testing.T) {
	te := newTest(t)
	var out bytes.Buffer
	root := &cli.Command{Name: "physa", Subs: []*cli.Command{commandFor(te.e)}}
	cli.Dispatch(root, []string{"bet", "--help"}, &out, io.Discard)
	for _, want := range []string{"physa bet plan set", "physa bet plan show", "physa bet work"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, out.String())
		}
	}
}

func TestShowPrintsEvidenceAfterProgress(t *testing.T) {
	te := newTest(t)
	secs := int64(7500)
	o := workapi.BetOut{
		Progress: &workapi.ProgressOut{},
		Evidence: []workapi.EvidenceOut{
			{Kind: "lead_time", PR: 12, Commit: "3f9a2c1d", Capability: "cli", At: "2026-11-22T10:00:00Z", Seconds: &secs},
			{Kind: "landed", Commit: "3f9a2c1d", Capability: "cli", At: "2026-11-22T09:00:00Z"},
		},
		NeedsYou: []workapi.NeedOut{{Text: "ask"}},
	}
	var out bytes.Buffer
	showProgress(te.e.View(&out), o)
	got := out.String()
	for _, want := range []string{"Evidence", "lead time  #12  cli  2026-11-22 10:00 UTC  lead time 2h 5m", "landed  3f9a2c1d  cli  2026-11-22 09:00 UTC"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if p, e, n := strings.Index(got, "Progress"), strings.Index(got, "Evidence"), strings.Index(got, "Needs you"); !(p < e && e < n) {
		t.Errorf("order wrong (%d %d %d):\n%s", p, e, n, got)
	}
	out.Reset()
	o.Evidence = nil
	showProgress(te.e.View(&out), o)
	if strings.Contains(out.String(), "Evidence") {
		t.Errorf("an empty Evidence section:\n%s", out.String())
	}
}
