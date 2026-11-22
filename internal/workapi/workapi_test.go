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
)

const goodPitch = "# Try a quiet mode\n## Problem\nToo noisy.\n## Appetite\n200k\n## Solution\nA flag.\n## Scope\nCLI only.\n## Signal\nFewer complaints.\n## Rabbit holes\nColour themes.\n## No-gos\nNo config file.\n"

type env struct {
	t    *testing.T
	c    *apiclient.Client
	repo string
}

// newEnv runs an engine with the eight methods in this process, on a short
// temporary directory, with a new git repo, and dials it.
func newEnv(t *testing.T) *env {
	t.Helper()
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
		done <- engine.Run(ctx, engine.Options{Dir: dir, Idle: time.Minute, Version: "test", Methods: Methods})
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
		kinds = append(kinds, ev.Kind)
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
	for _, m := range []string{"idea.add", "idea.list", "idea.show", "idea.shape", "bet.start", "bet.list", "bet.show", "bet.stop"} {
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
	if len(list) < 8+4 {
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
