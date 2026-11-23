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

// goodSignal is a whole signal, read on a day well after any test runs.
var goodSignal = map[string]any{"claim": "Fewer complaints.", "for": "complaints fall", "against": "complaints stay", "window": "2099-01-01", "resolver": "ann"}

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
	if idea.Slug != "try-a-quiet-mode" || idea.Person != "ann" || idea.Pursuit != "" || !strings.HasSuffix(idea.CreatedAt, "Z") {
		t.Fatalf("idea.add = %+v", idea)
	}
	e.must("idea.add", map[string]any{"text": "Second", "slug": "second", "person": "bo"}, &idea)
	var list []IdeaOut
	e.must("idea.list", nil, &list)
	if len(list) != 2 || list[0].Slug != "second" || list[1].Slug != "try-a-quiet-mode" {
		t.Fatalf("idea.list = %+v", list)
	}
	var pursuit PursuitOut
	e.must("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "200k", "signal": goodSignal, "person": "ann"}, &pursuit)
	if pursuit.State != "active" || pursuit.Appetite != 200000 || pursuit.Idea != "try-a-quiet-mode" || pursuit.Signal == nil ||
		pursuit.Signal.Window != "2099-01-01" || pursuit.Signal.Against != "complaints stay" || pursuit.StartedAt == "" || pursuit.EndedAt != "" {
		t.Fatalf("pursuit.start = %+v", pursuit)
	}
	e.must("idea.show", map[string]any{"slug": "try-a-quiet-mode"}, &idea)
	if idea.Pursuit != pursuit.Slug || idea.PursuitState != "active" || len(idea.Pursuits) != 1 {
		t.Fatalf("idea.show = %+v", idea)
	}
	var pursuits []PursuitOut
	e.must("pursuit.list", nil, &pursuits)
	if len(pursuits) != 1 || pursuits[0].Slug != pursuit.Slug {
		t.Fatalf("pursuit.list = %+v", pursuits)
	}
	e.must("pursuit.show", map[string]any{"slug": pursuit.Slug}, &pursuit)
	e.must("pursuit.stop", map[string]any{"slug": pursuit.Slug, "reason": "not worth it", "person": "ann"}, &pursuit)
	if pursuit.State != "stopped" || pursuit.StopReason != "not worth it" || pursuit.EndedAt == "" {
		t.Fatalf("pursuit.stop = %+v", pursuit)
	}
	var evs []engine.EventOut
	e.must("engine.events", nil, &evs)
	kinds := []string{}
	for _, ev := range evs {
		if ev.Kind != "intent.done" { // the evidence poll's own
			kinds = append(kinds, ev.Kind)
		}
	}
	if got := strings.Join(kinds, " "); got != "pursuit.stopped pursuit.started idea.added idea.added" {
		t.Errorf("events = %s", got)
	}
}

func TestEmptyLists(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{"idea.list", "pursuit.list"} {
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
	e.badRequest("pursuit.show", map[string]any{"slug": "nope"}, `"nope"`)
	e.badRequest("pursuit.stop", map[string]any{"slug": "nope", "person": "ann"}, `"nope"`)
	e.badRequest("idea.add", map[string]any{"text": "x", "slug": "try-a-quiet-mode", "person": "ann"}, "try-a-quiet-mode")
	e.badRequest("idea.add", map[string]any{"text": "x", "person": "ann", "extra": 1}, "extra")
	for _, gone := range []string{"idea.shape", "bet.start", "bet.list"} {
		if ae, ok := e.call(gone, map[string]any{"slug": "try-a-quiet-mode", "person": "ann"}, nil).(*api.Error); !ok || ae.Code != api.CodeUnknownMethod {
			t.Errorf("%s: err = %v, want unknown_method", gone, ae)
		}
	}
	e.badRequest("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "person": "ann"}, `missing required "signal"`)
	blank := map[string]any{"claim": " ", "for": "", "against": "a", "window": "", "resolver": ""}
	e.badRequest("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "signal": blank, "person": "ann"},
		"the signal has problems: ", `empty part "Claim"`, `empty "For:" in Direction`, `empty part "Window"`, `empty part "Resolver"`)
	past := map[string]any{"claim": "c", "for": "f", "against": "a", "window": "2020-01-01", "resolver": "ann"}
	e.badRequest("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "signal": past, "person": "ann"}, "window 2020-01-01 is before today")
	e.badRequest("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "lots", "signal": goodSignal, "person": "ann"}, "lots")
	e.must("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "signal": goodSignal, "person": "ann"}, nil)
	e.badRequest("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "10k", "signal": goodSignal, "person": "ann"}, "active pursuit")
	e.must("pursuit.stop", map[string]any{"slug": "try-a-quiet-mode", "person": "ann"}, nil)
	e.badRequest("pursuit.stop", map[string]any{"slug": "try-a-quiet-mode", "person": "ann"}, "not active")
	err := e.c.Call(context.Background(), "idea.list", map[string]any{"repo": "some/repo"}, nil)
	if ae, ok := err.(*api.Error); !ok || ae.Code != api.CodeBadRequest {
		t.Errorf("a repo that is not absolute: %v", err)
	}
}

func TestDescribeShowsEachMethod(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{"idea.add", "idea.list", "idea.show", "pursuit.start", "pursuit.list", "pursuit.show", "pursuit.stop", "plan.set", "plan.show", "pursuit.work", "pursuit.spend", "evidence.list"} {
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
	if len(list) < 12+2 {
		t.Errorf("api.list has %d methods", len(list))
	}
}

func TestPersonOnly(t *testing.T) {
	for m, want := range map[string]bool{"pursuit.start": true, "pursuit.stop": true, "pursuit.list": false, "idea.add": false, "plan.set": false} {
		if PersonOnly(m) != want {
			t.Errorf("PersonOnly(%s) = %v", m, !want)
		}
	}
	if got := Refusal("pursuit.start", "Claude Code"); got != "A person must start this pursuit: physa is running inside a Claude Code session." {
		t.Errorf("Refusal = %q", got)
	}
	if got := Refusal("pursuit.stop", "Claude Code"); !strings.HasPrefix(got, "A person must stop this pursuit:") {
		t.Errorf("Refusal = %q", got)
	}
}

const twoChanges = `{"changes":[
 {"name":"first-step","capability":"alpha","what":"Do the first.","acceptance":["failure case: a bad input exits 2"],"must_not":[],"after":[]},
 {"name":"second-step","capability":"beta","what":"Do the second.","acceptance":["no failure case: it only reads"],"must_not":[],"after":["first-step"]}]}`

// planEnv is an env whose repo holds a map of two capabilities, a first
// commit, and an active pursuit of 100k on an idea, with ann as the person.
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
	e.must("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "100k", "signal": goodSignal, "person": "ann"}, nil)
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
	e.must("plan.set", map[string]any{"pursuit": "try-a-quiet-mode", "person": "ann", "plan": jsonRaw(twoChanges)}, &out)
	return out
}

func TestPlanSetAndShow(t *testing.T) {
	e := planEnv(t)
	got := e.setPlan()
	if got.N != 1 || got.Replans != 0 || len(got.Changes) != 2 || got.Changes[1].After[0] != "first-step" {
		t.Fatalf("plan.set = %+v", got)
	}
	var shown PlanOut
	e.must("plan.show", map[string]any{"pursuit": "try-a-quiet-mode", "history": true}, &shown)
	if shown.N != 1 || len(shown.History) != 1 || strings.Join(shown.History[0].Changes, ",") != "first-step,second-step" {
		t.Fatalf("plan.show = %+v", shown)
	}
	e.badRequest("plan.show", map[string]any{"pursuit": "nope"}, `"nope"`)
}

func TestBadPlanAndReason(t *testing.T) {
	e := planEnv(t)
	bad := `{"changes":[
 {"name":"a","capability":"alpha, beta","what":"x","acceptance":["failure case: y"],"after":["b"]},
 {"name":"b","capability":"alpha","what":"x","acceptance":["failure case: y"],"after":["a"]}]}`
	e.badRequest("plan.set", map[string]any{"pursuit": "try-a-quiet-mode", "person": "ann", "plan": jsonRaw(bad)},
		"the plan has problems:", "names two capabilities", "a -> b -> a")
	e.setPlan()
	e.badRequest("plan.set", map[string]any{"pursuit": "try-a-quiet-mode", "person": "ann", "plan": jsonRaw(twoChanges)}, "needs a reason")
	var out PlanOut
	e.must("plan.set", map[string]any{"pursuit": "try-a-quiet-mode", "person": "ann", "reason": "missed a step", "plan": jsonRaw(twoChanges)}, &out)
	if out.N != 2 || out.Replans != 1 || out.Reason != "missed a step" {
		t.Fatalf("second revision = %+v", out)
	}
}

func TestSpendBreakerAndNeeds(t *testing.T) {
	e := planEnv(t)
	e.setPlan()
	e.must("pursuit.work", map[string]any{"pursuit": "try-a-quiet-mode", "change": "first-step", "session": "s-1"}, nil)
	var sp SpendOut
	e.must("pursuit.spend", map[string]any{"pursuit": "try-a-quiet-mode", "change": "first-step", "session": "s-1", "tokens": 79000}, &sp)
	if sp.Warned || sp.Broke || sp.Spent != 79000 {
		t.Fatalf("79k = %+v", sp)
	}
	e.must("pursuit.spend", map[string]any{"pursuit": "try-a-quiet-mode", "change": "first-step", "session": "s-1", "tokens": 80000}, &sp)
	if !sp.Warned || sp.Broke {
		t.Fatalf("80k = %+v", sp)
	}
	e.must("pursuit.spend", map[string]any{"pursuit": "try-a-quiet-mode", "change": "first-step", "session": "s-1", "tokens": 80000}, &sp)
	if sp.Warned || sp.Spent != 80000 {
		t.Fatalf("repeat = %+v", sp)
	}
	e.must("pursuit.spend", map[string]any{"pursuit": "try-a-quiet-mode", "change": "first-step", "session": "s-1", "tokens": 100000}, &sp)
	if !sp.Broke || sp.State != "stopped" || sp.Appetite != 100000 {
		t.Fatalf("100k = %+v", sp)
	}
	var pursuit PursuitOut
	e.must("pursuit.show", map[string]any{"slug": "try-a-quiet-mode"}, &pursuit)
	if pursuit.State != "stopped" || !strings.HasPrefix(pursuit.StopReason, "circuit breaker: spent") || len(pursuit.NeedsYou) != 2 || pursuit.Appetite != 100000 {
		t.Fatalf("pursuit.show = %+v", pursuit)
	}
	if p := pursuit.Progress; p == nil || p.Spent != 100000 || len(p.PerChange) != 1 || p.PerChange[0].Change != "first-step" || len(p.InWork) != 1 {
		t.Fatalf("progress = %+v", pursuit.Progress)
	}
	e.badRequest("pursuit.spend", map[string]any{"pursuit": "try-a-quiet-mode", "change": "nope", "session": "s-1", "tokens": 1}, "nope")
}

func TestShowLandsChangesAndFinishesPursuit(t *testing.T) {
	e := planEnv(t)
	e.setPlan()
	e.git("commit", "-q", "--allow-empty", "-m", "feat: first\n\nChange: try-a-quiet-mode/first-step")
	var pursuit PursuitOut
	e.must("pursuit.show", map[string]any{"slug": "try-a-quiet-mode"}, &pursuit)
	if pursuit.State != "active" || len(pursuit.Progress.Landed) != 1 || pursuit.Progress.Landed[0].Change != "first-step" || len(pursuit.Progress.Landed[0].Commit) != 40 {
		t.Fatalf("one landed = %+v", pursuit)
	}
	e.git("commit", "-q", "--allow-empty", "-m", "feat: second\n\nChange: try-a-quiet-mode/second-step")
	e.must("pursuit.show", map[string]any{"slug": "try-a-quiet-mode"}, &pursuit)
	if pursuit.State != "done" || len(pursuit.Progress.Landed) != 2 || pursuit.EndedAt == "" {
		t.Fatalf("both landed = %+v", pursuit)
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
	e.must("pursuit.start", map[string]any{"slug": "try-a-quiet-mode", "appetite": "100k", "signal": goodSignal, "person": "ann"}, nil)
	all := e.facts(2, nil)
	if all[0].Kind != "landed" || all[0].Source != "vcs" || len(all[0].Commit) != 8 || all[0].At == "" {
		t.Fatalf("a fact = %+v", all[0])
	}
	if got := e.facts(1, map[string]any{"capability": "alpha"}); got[0].Pursuit != "try-a-quiet-mode" || got[0].Change != "first-step" || got[0].Capability != "alpha" {
		t.Errorf("capability filter = %+v", got)
	}
	e.facts(1, map[string]any{"pursuit": "try-a-quiet-mode"})
	e.facts(0, map[string]any{"pursuit": "no-such-pursuit"})
	e.facts(1, map[string]any{"limit": 1})
	e.git("revert", "--no-edit", "HEAD")
	// A new pursuit wakes the poll, so the revert is seen at once, not in 5 minutes.
	e.must("idea.add", map[string]any{"text": "Second", "slug": "second", "person": "ann"}, nil)
	e.must("pursuit.start", map[string]any{"slug": "second", "appetite": "100k", "signal": goodSignal, "person": "ann"}, nil)
	if got := e.facts(4, nil); len(got) != 4 {
		t.Fatalf("after a revert: %+v", got)
	}
	var pursuit PursuitOut
	e.must("pursuit.show", map[string]any{"slug": "try-a-quiet-mode"}, &pursuit)
	if len(pursuit.Evidence) == 0 {
		t.Fatalf("pursuit.show carries no evidence: %+v", pursuit)
	}
	for _, f := range pursuit.Evidence {
		if f.Pursuit != "try-a-quiet-mode" {
			t.Errorf("pursuit.show evidence for another pursuit: %+v", f)
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
