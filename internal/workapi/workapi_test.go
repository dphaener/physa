//go:build linux || darwin

package workapi

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/apiclient"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/state"
	stateevidence "github.com/dphaener/physa/internal/state/evidence"
)

const goodPitch = "# Try a quiet mode\n## Problem\nToo noisy.\n## Appetite\n200k\n## Solution\nA flag.\n## Scope\nCLI only.\n## Signal\nFewer complaints.\n## Rabbit holes\nColour themes.\n## No-gos\nNo config file.\n"

type env struct {
	t    *testing.T
	c    *apiclient.Client
	repo string
}

// newEnv runs an engine with the eight methods in this process, on a short
// temporary directory, with a new git repo, and dials it.
func newEnv(t *testing.T) *env { return newEnvWith(t, &fakeForge{}) }

// fakeForge is a forge with no merged PRs.
type fakeForge struct{}

func (*fakeForge) Name() string                            { return "fake" }
func (*fakeForge) PRExists(int) (bool, error)              { return false, nil }
func (*fakeForge) ChangePR(string) (forge.PR, bool, error) { return forge.PR{}, false, nil }
func (*fakeForge) MergedPRs(time.Time, int) ([]forge.PR, bool, error) {
	return nil, false, nil
}

// newEnvWith is newEnv with the evidence kinds registered on a forge: the
// poll runs in the engine, the forge is the fake, nothing reaches GitHub.
func newEnvWith(t *testing.T, ff forge.Forge) *env {
	t.Helper()
	timers, intents := stateevidence.Options(func(string) forge.Forge { return ff })
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	repo := filepath.Join(tmp, "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if repo, err = filepath.EvalSymlinks(repo); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "physa")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- engine.Run(ctx, engine.Options{Dir: dir, Idle: time.Minute, Version: "test", Methods: Methods,
			State: state.Options{Timers: timers, Intents: intents}})
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
	var c *apiclient.Client
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		dctx, dcancel := context.WithTimeout(context.Background(), time.Second)
		c, err = apiclient.Dial(dctx, p.Socket)
		dcancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine did not answer within 5s: %v", err)
		}
	}
	t.Cleanup(func() { c.Close() })
	return &env{t, c, repo}
}

func (e *env) call(method string, params map[string]any, out any) error {
	e.t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["repo"] = e.repo
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return e.c.Call(ctx, method, params, out)
}

func (e *env) must(method string, params map[string]any, out any) {
	e.t.Helper()
	if err := e.call(method, params, out); err != nil {
		e.t.Fatalf("%s: %v", method, err)
	}
}

// badRequest checks that the call replies bad_request with a message
// containing every part of want.
func (e *env) badRequest(method string, params map[string]any, want ...string) {
	e.t.Helper()
	err := e.call(method, params, nil)
	ae, ok := err.(*api.Error)
	if !ok || ae.Code != api.CodeBadRequest {
		e.t.Fatalf("%s: err = %v, want bad_request", method, err)
	}
	for _, w := range want {
		if !strings.Contains(ae.Message, w) {
			e.t.Errorf("%s: message %q lacks %q", method, ae.Message, w)
		}
	}
}

func TestMethodsHappyPath(t *testing.T) {
	e := newEnv(t)
	var idea IdeaOut
	e.must("idea.add", map[string]any{"text": "Try a quiet mode", "person": "ann"}, &idea)
	if idea.Slug != "try-a-quiet-mode" || idea.Person != "ann" || idea.Pitches != 0 || !strings.HasSuffix(idea.CreatedAt, "Z") {
		t.Fatalf("idea.add = %+v", idea)
	}
	e.must("idea.add", map[string]any{"text": "Second", "slug": "second", "person": "bo"}, &idea)
	var list []IdeaOut
	e.must("idea.list", nil, &list)
	if len(list) != 2 || list[0].Slug != "second" || list[1].Slug != "try-a-quiet-mode" {
		t.Fatalf("idea.list = %+v", list)
	}
	e.must("idea.shape", map[string]any{"slug": "try-a-quiet-mode", "markdown": goodPitch, "person": "ann"}, &idea)
	if idea.Pitches != 1 || idea.Pitch == nil || idea.Pitch.Appetite != 200000 || idea.Pitch.RabbitHoles != "Colour themes." {
		t.Fatalf("idea.shape = %+v", idea)
	}
	var bet BetOut
	e.must("bet.start", map[string]any{"slug": "try-a-quiet-mode", "person": "ann"}, &bet)
	if bet.State != "active" || bet.Appetite != 200000 || bet.Idea != "try-a-quiet-mode" || bet.Pitch == nil || bet.StartedAt == "" || bet.EndedAt != "" {
		t.Fatalf("bet.start = %+v", bet)
	}
	e.must("idea.show", map[string]any{"slug": "try-a-quiet-mode"}, &idea)
	if idea.Bet != bet.Slug || idea.BetState != "active" || len(idea.Bets) != 1 || idea.Pitch == nil {
		t.Fatalf("idea.show = %+v", idea)
	}
	var bets []BetOut
	e.must("bet.list", nil, &bets)
	if len(bets) != 1 || bets[0].Slug != bet.Slug {
		t.Fatalf("bet.list = %+v", bets)
	}
	e.must("bet.show", map[string]any{"slug": bet.Slug}, &bet)
	e.must("bet.stop", map[string]any{"slug": bet.Slug, "reason": "not worth it", "person": "ann"}, &bet)
	if bet.State != "stopped" || bet.StopReason != "not worth it" || bet.EndedAt == "" {
		t.Fatalf("bet.stop = %+v", bet)
	}
	var evs []engine.EventOut
	e.must("engine.events", nil, &evs)
	kinds := []string{}
	for _, ev := range evs {
		if ev.Kind != "intent.done" { // the evidence poll's own
			kinds = append(kinds, ev.Kind)
		}
	}
	if got := strings.Join(kinds, " "); got != "bet.stopped bet.started pitch.shaped idea.added idea.added" {
		t.Errorf("events = %s", got)
	}
}

func TestEmptyLists(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{"idea.list", "bet.list"} {
		var raw json.RawMessage
		e.must(m, nil, &raw)
		if string(raw) != "[]" {
			t.Errorf("%s = %s, want []", m, raw)
		}
	}
}

func TestBadRequests(t *testing.T) {
	e := newEnv(t)
	e.must("idea.add", map[string]any{"text": "Try a quiet mode", "person": "ann"}, nil)
	e.badRequest("idea.show", map[string]any{"slug": "nope"}, `"nope"`)
	e.badRequest("bet.show", map[string]any{"slug": "nope"}, `"nope"`)
	e.badRequest("bet.stop", map[string]any{"slug": "nope", "person": "ann"}, `"nope"`)
	e.badRequest("idea.add", map[string]any{"text": "x", "slug": "try-a-quiet-mode", "person": "ann"}, "try-a-quiet-mode")
	e.badRequest("idea.add", map[string]any{"text": "x", "person": "ann", "extra": 1}, "extra")
	e.badRequest("idea.shape", map[string]any{"slug": "try-a-quiet-mode", "markdown": "## Problem\nOnly this.\n", "person": "ann"}, "Appetite", "Solution", "No-gos")
	e.badRequest("bet.start", map[string]any{"slug": "try-a-quiet-mode", "person": "ann"}, "appetite")
	e.badRequest("bet.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "lots", "person": "ann"}, "lots")
	e.must("bet.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "10k", "person": "ann"}, nil)
	e.badRequest("bet.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "10k", "person": "ann"}, "active bet")
	e.must("bet.stop", map[string]any{"slug": "try-a-quiet-mode", "person": "ann"}, nil)
	e.badRequest("bet.stop", map[string]any{"slug": "try-a-quiet-mode", "person": "ann"}, "not active")
	err := e.c.Call(context.Background(), "idea.list", map[string]any{"repo": "some/repo"}, nil)
	if ae, ok := err.(*api.Error); !ok || ae.Code != api.CodeBadRequest {
		t.Errorf("a repo that is not absolute: %v", err)
	}
}

func TestDescribeShowsEachMethod(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{"idea.add", "idea.list", "idea.show", "idea.shape", "bet.start", "bet.list", "bet.show", "bet.stop", "plan.set", "plan.show", "bet.work", "bet.spend", "evidence.list"} {
		var d api.Description
		if err := e.c.Call(context.Background(), "api.describe", api.DescribeInput{Name: m}, &d); err != nil {
			t.Fatalf("describe %s: %v", m, err)
		}
		if d.Name != m || d.Summary == "" || !json.Valid([]byte(d.RequestExample)) || !json.Valid([]byte(d.ResponseExample)) || len(d.Notes) == 0 {
			t.Errorf("describe %s = %+v", m, d)
		}
		if d.Input.Properties["repo"] == nil {
			t.Errorf("%s: input schema has no repo", m)
		}
	}
	var list []api.MethodSummary
	if err := e.c.Call(context.Background(), "api.list", nil, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) < 13+2 {
		t.Errorf("api.list has %d methods", len(list))
	}
}

func TestPersonOnly(t *testing.T) {
	for m, want := range map[string]bool{"bet.start": true, "bet.stop": true, "bet.list": false, "idea.add": false, "idea.shape": false} {
		if PersonOnly(m) != want {
			t.Errorf("PersonOnly(%s) = %v", m, !want)
		}
	}
	if got := Refusal("bet.start", "Claude Code"); got != "A person must make this bet: physa is running inside a Claude Code session." {
		t.Errorf("Refusal = %q", got)
	}
	if got := Refusal("bet.stop", "Claude Code"); !strings.HasPrefix(got, "A person must stop this bet:") {
		t.Errorf("Refusal = %q", got)
	}
}

const twoChanges = `{"changes":[
 {"name":"first-step","capability":"alpha","what":"Do the first.","acceptance":["failure case: a bad input exits 2"],"must_not":[],"after":[]},
 {"name":"second-step","capability":"beta","what":"Do the second.","acceptance":["no failure case: it only reads"],"must_not":[],"after":["first-step"]}]}`

// planEnv is an env whose repo holds a map of two capabilities, a first
// commit, and an active bet of 100k on an idea, with ann as the person.
func planEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	for _, c := range []string{"alpha", "beta"} {
		dir := filepath.Join(e.repo, ".physa", "capabilities", c)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("# "+c+"\n\nPromise of "+c+".\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.git("add", ".")
	e.git("commit", "-q", "-m", "chore: map")
	e.must("idea.add", map[string]any{"text": "Try a quiet mode", "person": "ann"}, nil)
	e.must("bet.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "100k", "person": "ann"}, nil)
	return e
}

func (e *env) git(args ...string) {
	e.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = e.repo
	if out, err := cmd.CombinedOutput(); err != nil {
		e.t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func (e *env) setPlan() PlanOut {
	e.t.Helper()
	var out PlanOut
	e.must("plan.set", map[string]any{"bet": "try-a-quiet-mode", "person": "ann", "plan": jsonRaw(twoChanges)}, &out)
	return out
}

func TestPlanSetAndShow(t *testing.T) {
	e := planEnv(t)
	got := e.setPlan()
	if got.N != 1 || got.Replans != 0 || len(got.Changes) != 2 || got.Changes[1].After[0] != "first-step" {
		t.Fatalf("plan.set = %+v", got)
	}
	var shown PlanOut
	e.must("plan.show", map[string]any{"bet": "try-a-quiet-mode", "history": true}, &shown)
	if shown.N != 1 || len(shown.History) != 1 || strings.Join(shown.History[0].Changes, ",") != "first-step,second-step" {
		t.Fatalf("plan.show = %+v", shown)
	}
	e.badRequest("plan.show", map[string]any{"bet": "nope"}, `"nope"`)
}

func TestBadPlanAndReason(t *testing.T) {
	e := planEnv(t)
	bad := `{"changes":[
 {"name":"a","capability":"alpha, beta","what":"x","acceptance":["failure case: y"],"after":["b"]},
 {"name":"b","capability":"alpha","what":"x","acceptance":["failure case: y"],"after":["a"]}]}`
	e.badRequest("plan.set", map[string]any{"bet": "try-a-quiet-mode", "person": "ann", "plan": jsonRaw(bad)},
		"the plan has problems:", "names two capabilities", "a -> b -> a")
	e.setPlan()
	e.badRequest("plan.set", map[string]any{"bet": "try-a-quiet-mode", "person": "ann", "plan": jsonRaw(twoChanges)}, "needs a reason")
	var out PlanOut
	e.must("plan.set", map[string]any{"bet": "try-a-quiet-mode", "person": "ann", "reason": "missed a step", "plan": jsonRaw(twoChanges)}, &out)
	if out.N != 2 || out.Replans != 1 || out.Reason != "missed a step" {
		t.Fatalf("second revision = %+v", out)
	}
}

func TestSpendBreakerAndNeeds(t *testing.T) {
	e := planEnv(t)
	e.setPlan()
	e.must("bet.work", map[string]any{"bet": "try-a-quiet-mode", "change": "first-step", "session": "s-1"}, nil)
	var sp SpendOut
	e.must("bet.spend", map[string]any{"bet": "try-a-quiet-mode", "change": "first-step", "session": "s-1", "tokens": 79000}, &sp)
	if sp.Warned || sp.Broke || sp.Spent != 79000 {
		t.Fatalf("79k = %+v", sp)
	}
	e.must("bet.spend", map[string]any{"bet": "try-a-quiet-mode", "change": "first-step", "session": "s-1", "tokens": 80000}, &sp)
	if !sp.Warned || sp.Broke {
		t.Fatalf("80k = %+v", sp)
	}
	e.must("bet.spend", map[string]any{"bet": "try-a-quiet-mode", "change": "first-step", "session": "s-1", "tokens": 80000}, &sp)
	if sp.Warned || sp.Spent != 80000 {
		t.Fatalf("repeat = %+v", sp)
	}
	e.must("bet.spend", map[string]any{"bet": "try-a-quiet-mode", "change": "first-step", "session": "s-1", "tokens": 100000}, &sp)
	if !sp.Broke || sp.State != "stopped" || sp.Appetite != 100000 {
		t.Fatalf("100k = %+v", sp)
	}
	var bet BetOut
	e.must("bet.show", map[string]any{"slug": "try-a-quiet-mode"}, &bet)
	if bet.State != "stopped" || !strings.HasPrefix(bet.StopReason, "circuit breaker: spent") || len(bet.NeedsYou) != 2 || bet.Appetite != 100000 {
		t.Fatalf("bet.show = %+v", bet)
	}
	if p := bet.Progress; p == nil || p.Spent != 100000 || len(p.PerChange) != 1 || p.PerChange[0].Change != "first-step" || len(p.InWork) != 1 {
		t.Fatalf("progress = %+v", bet.Progress)
	}
	e.badRequest("bet.spend", map[string]any{"bet": "try-a-quiet-mode", "change": "nope", "session": "s-1", "tokens": 1}, "nope")
}

func TestShowLandsChangesAndFinishesBet(t *testing.T) {
	e := planEnv(t)
	e.setPlan()
	e.git("commit", "-q", "--allow-empty", "-m", "feat: first\n\nChange: try-a-quiet-mode/first-step")
	var bet BetOut
	e.must("bet.show", map[string]any{"slug": "try-a-quiet-mode"}, &bet)
	if bet.State != "active" || len(bet.Progress.Landed) != 1 || bet.Progress.Landed[0].Change != "first-step" || len(bet.Progress.Landed[0].Commit) != 40 {
		t.Fatalf("one landed = %+v", bet)
	}
	e.git("commit", "-q", "--allow-empty", "-m", "feat: second\n\nChange: try-a-quiet-mode/second-step")
	e.must("bet.show", map[string]any{"slug": "try-a-quiet-mode"}, &bet)
	if bet.State != "done" || len(bet.Progress.Landed) != 2 || bet.EndedAt == "" {
		t.Fatalf("both landed = %+v", bet)
	}
}

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

// commitFile commits a new file with message, so each commit changes the tree.
func (e *env) commitFile(name, message string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.repo, name), []byte(name), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.git("add", name)
	e.git("commit", "-q", "-m", message)
}

// facts calls evidence.list until it returns want facts: the engine polls
// on its own, so the facts arrive a moment after the commits.
func (e *env) facts(want int, params map[string]any) []EvidenceOut {
	e.t.Helper()
	var out []EvidenceOut
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		out = nil
		e.must("evidence.list", params, &out)
		if len(out) == want {
			return out
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("evidence.list %v returned %d facts, want %d: %+v", params, len(out), want, out)
		}
	}
}

func TestEvidenceListAndShow(t *testing.T) {
	e := newEnv(t)
	e.commitFile("a.txt", "feat: a\n\nCapability: alpha\nChange: try-a-quiet-mode/first-step")
	e.commitFile("b.txt", "feat: b\n\nCapability: beta")
	e.must("idea.add", map[string]any{"text": "Try a quiet mode", "person": "ann"}, nil)
	e.must("bet.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "100k", "person": "ann"}, nil)
	all := e.facts(2, nil)
	if all[0].Kind != "landed" || all[0].Source != "vcs" || len(all[0].Commit) != 8 || all[0].At == "" {
		t.Fatalf("a fact = %+v", all[0])
	}
	if got := e.facts(1, map[string]any{"capability": "alpha"}); got[0].Bet != "try-a-quiet-mode" || got[0].Change != "first-step" || got[0].Capability != "alpha" {
		t.Errorf("capability filter = %+v", got)
	}
	e.facts(1, map[string]any{"bet": "try-a-quiet-mode"})
	e.facts(0, map[string]any{"bet": "no-such-bet"})
	e.facts(1, map[string]any{"limit": 1})
	e.git("revert", "--no-edit", "HEAD")
	// A new bet wakes the poll, so the revert is seen at once, not in 5 minutes.
	e.must("idea.add", map[string]any{"text": "Second", "slug": "second", "person": "ann"}, nil)
	e.must("bet.start", map[string]any{"slug": "second", "appetite": "100k", "person": "ann"}, nil)
	if got := e.facts(4, nil); len(got) != 4 {
		t.Fatalf("after a revert: %+v", got)
	}
	var bet BetOut
	e.must("bet.show", map[string]any{"slug": "try-a-quiet-mode"}, &bet)
	if len(bet.Evidence) == 0 {
		t.Fatalf("bet.show carries no evidence: %+v", bet)
	}
	for _, f := range bet.Evidence {
		if f.Bet != "try-a-quiet-mode" {
			t.Errorf("bet.show evidence for another bet: %+v", f)
		}
	}
}

func TestEvidenceBadRepo(t *testing.T) {
	e := newEnv(t)
	e.t.Helper()
	for _, repo := range []string{"some/repo", filepath.Join(t.TempDir(), "not-a-repo")} {
		err := e.c.Call(context.Background(), "evidence.list", map[string]any{"repo": repo}, nil)
		ae, ok := err.(*api.Error)
		if !ok || ae.Code != api.CodeBadRequest || !strings.Contains(ae.Message, repo) {
			t.Errorf("evidence.list %q: err = %v, want bad_request naming the path", repo, err)
		}
	}
}

func TestProductionRegistersThePoll(t *testing.T) {
	o := StateOptions()
	var timers, intents []string
	for _, k := range o.Timers {
		timers = append(timers, k.Name)
	}
	for _, k := range o.Intents {
		intents = append(intents, k.Name)
	}
	if got := strings.Join(timers, " "); !strings.Contains(got, stateevidence.PollKind) || !strings.Contains(got, stateevidence.ExpireKind) {
		t.Errorf("timers = %v", timers)
	}
	if got := strings.Join(intents, " "); got != stateevidence.PollKind {
		t.Errorf("intents = %v", intents)
	}
}
