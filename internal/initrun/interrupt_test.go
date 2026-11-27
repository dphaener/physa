package initrun

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/recovery"
)

// fourNames are the capabilities fourWalk names, each owning the source file
// of its name in fourRepo.
var fourNames = []string{"alpha", "beta", "gamma", "delta"}

// fourAreas puts fourRepo's source files in one area, and fourWalk names a
// capability for each.
var fourAreas, fourWalk = func() (string, string) {
	var files, caps []string
	for _, n := range fourNames {
		files = append(files, fmt.Sprintf("%q", n+".go"))
		caps = append(caps, fmt.Sprintf(`{"name":%q,"promise":"People rely on %s.","sources":[%q],"tests":[],"docs":[]}`, n, n, n+".go"))
	}
	return "```json\n" + `{"product":{"name":"Letters","promise":"People rely on their letters."},"areas":[{"name":"letters","promise":"People rely on letters.","sources":[` +
			strings.Join(files, ",") + `]}]}` + "\n```",
		"```json\n" + `{"capabilities":[` + strings.Join(caps, ",") + `],"shared":[]}` + "\n```"
}()

// fourRepo is newRepo with a source file for each of fourNames, committed.
func fourRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	for _, n := range fourNames {
		write(t, dir, n+".go", "package main\n")
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: Add the letters")
	return dir
}

// capReply is capability n's recovering answer: one rule anchored to PR 3 and
// one term, each its own, so that nothing is a duplicate across capabilities.
func capReply(n string) string {
	title := strings.ToUpper(n[:1]) + n[1:]
	return "````markdown\n# " + title + "\n\n## Rules\n\n### " + title + " holds\n`" + n + "-holds` · #3\nPeople rely on " + n + ".\n" +
		"Because: #3 asked for it.\n````\n\n```glossary\n**" + title + "**: What " + n + " is.\n```\n"
}

// cutAgent discovers nothing, answers the areas phase with fourAreas and the
// walk with fourWalk, recovers each capability with capReply and changes
// nothing in consolidation, recording each session it starts. When cut is
// "recover" it cancels the run's ctx as the third recovering session starts
// and holds that session until the ctx is done, ending it as the agent does
// when stopped; the two started before it answer once the cancel came. When
// cut is "walk" or "consolidate" it does the same to that phase's first
// session; when it is empty it cuts nothing.
type cutAgent struct {
	mu       sync.Mutex
	cut      string
	cancel   context.CancelFunc
	released chan struct{}
	started  []string // "discover", "areas", "walk", the capability or "consolidate"
	after    []string // sessions started once the ctx was done
}

func newCutAgent(cut string, cancel context.CancelFunc) *cutAgent {
	return &cutAgent{cut: cut, cancel: cancel, released: make(chan struct{})}
}

func (a *cutAgent) Name() string { return "fake" }

func (a *cutAgent) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	name, text := sessionKind(s), ""
	switch name {
	case "discover":
		text = "```json\n{\"sources\":[]}\n```\n"
	case "split":
		name, text = "areas", areasSplit
	case "areas":
		text = fourAreas
	case "walk":
		text = fourWalk
	case "consolidate":
		text = consolidateNothing(s.System)
	default:
		text = capReply(name)
	}
	if ctx.Err() != nil {
		a.after = append(a.after, name)
	}
	a.started = append(a.started, name)
	res := adapter.Result{Text: text, Tokens: adapter.Tokens{Input: 1_000, Output: 10}, SessionID: s.SessionID}
	sess := &cutSession{res: res}
	recovering := 0
	for _, n := range a.started {
		if slices.Contains(fourNames, n) {
			recovering++
		}
	}
	isCap := slices.Contains(fourNames, name)
	switch {
	case a.cut == name && !isCap && ctx.Err() == nil, a.cut == "recover" && isCap && recovering == 3:
		a.cancel()
		close(a.released)
		sess.stopped = ctx
	case a.cut == "recover" && isCap && recovering < 3:
		sess.wait = a.released
	}
	return sess, nil
}

// cutSession answers res, once wait is closed when set; when stopped is set
// it waits for that ctx to be done and fails as a stopped session does.
type cutSession struct {
	res     adapter.Result
	wait    chan struct{}
	stopped context.Context
}

func (s *cutSession) Wait() (adapter.Result, error) {
	if s.stopped != nil {
		<-s.stopped.Done()
		return adapter.Result{Tokens: adapter.Tokens{Input: 500}}, fmt.Errorf("%w: %w", adapter.ErrFailed, s.stopped.Err())
	}
	if s.wait != nil {
		<-s.wait
	}
	return s.res, nil
}

// runInitCtx is runInit with the run's ctx.
func runInitCtx(ctx context.Context, env Env, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := execute(args, &stdout, &stderr, func(f Flags, pos, words []string, stdout, stderr io.Writer) int {
		return Run(ctx, f, pos, words, stdout, stderr, env)
	})
	return code, stdout.String(), stderr.String()
}

// fourWalkedEnv is an Env on fourRepo with a gather, the areas, the walk of
// fourNames and their placements stored.
func fourWalkedEnv(t *testing.T) Env {
	t.Helper()
	fourRepo(t)
	env := testEnv(t, onePR(), newCutAgent("", nil))
	for _, phase := range []string{"--gather", "--areas", "--walk", "--place"} {
		if code, _, errOut := runInit(env, phase); code != 0 {
			t.Fatalf("%s: %d %q", phase, code, errOut)
		}
	}
	return env
}

// TestInterruptedRecoverKeepsWhatWasRecovered checks acceptance 3 and 4 of
// an interrupt: a --recover whose ctx is cancelled as its third session
// starts exits 130, says it was interrupted, that the 2 capabilities
// recovered are kept and that physa init --recover continues, keeps those two
// in init's record as recovered and the other two as not yet, starts no
// session after the cancel, and says nothing of the cap when one is set. A
// second --recover starts only the two left.
func TestInterruptedRecoverKeepsWhatWasRecovered(t *testing.T) {
	for _, extra := range [][]string{nil, {"--cap", "10m"}} {
		t.Run(strings.Join(append([]string{"recover"}, extra...), " "), func(t *testing.T) {
			env := fourWalkedEnv(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			a := newCutAgent("recover", cancel)
			env.Agent = func(string) (adapter.Adapter, error) { return a, nil }
			code, out, errOut := runInitCtx(ctx, env, append([]string{"--recover"}, extra...)...)
			t.Logf("first run, sessions %v:\n%s", a.started, errOut)
			if code != 130 {
				t.Fatalf("code %d, want 130\nstdout %s\nstderr %s", code, out, errOut)
			}
			// the cap typed still applies to the run that continues
			want := "physa init: interrupted: nothing more was started and sessions running were stopped; 2 capabilities recovered in this run are kept. To continue, run " + strings.Join(append([]string{"physa init --recover"}, extra...), " ") + ".\n"
			if !strings.HasSuffix(errOut, want) {
				t.Errorf("stderr does not end with %q:\n%s", want, errOut)
			}
			for _, not := range []string{"context canceled", "failed", "the cap of", "of its cap"} {
				if strings.Contains(errOut, not) {
					t.Errorf("stderr says %q:\n%s", not, errOut)
				}
			}
			if len(a.after) != 0 || len(a.started) != 3 {
				t.Errorf("sessions %v, of them after the cancel %v; want 3, none after", a.started, a.after)
			}
			rows := recoveredRows(t, env)
			recovered := a.started[:2]
			for _, n := range fourNames {
				want := recovery.StatusNotYet
				if slices.Contains(recovered, n) {
					want = recovery.StatusRecovered
				}
				if rows[n] != want {
					t.Errorf("%s stored %q, want %q (all %v)", n, rows[n], want, rows)
				}
			}

			b := newRoutedAgent()
			for _, n := range fourNames {
				b.replies[n] = capReply(n)
			}
			env.Agent = func(string) (adapter.Adapter, error) { return b, nil }
			code, out, errOut = runInit(env, "--recover")
			rest := slices.DeleteFunc(slices.Clone(fourNames), func(n string) bool { return slices.Contains(recovered, n) })
			got := slices.Sorted(slices.Values(b.started))
			if code != 0 || !slices.Equal(got, slices.Sorted(slices.Values(rest))) {
				t.Fatalf("second run: code %d, sessions %v, want %v\nstdout %s\nstderr %s", code, b.started, rest, out, errOut)
			}
			for n, s := range recoveredRows(t, env) {
				if s != recovery.StatusRecovered {
					t.Errorf("after the second run %s is %q", n, s)
				}
			}
		})
	}
}

// TestInterruptedWalkStoresNothing checks that a --walk whose ctx is
// cancelled while a session walks exits 130, says nothing of the walk was
// stored and that physa init --walk continues, and stores no walk.
func TestInterruptedWalkStoresNothing(t *testing.T) {
	fourRepo(t)
	env := testEnv(t, onePR(), newCutAgent("", nil))
	for _, phase := range []string{"--gather", "--areas"} {
		if code, _, errOut := runInit(env, phase); code != 0 {
			t.Fatalf("%s: %d %q", phase, code, errOut)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a := newCutAgent("walk", cancel)
	env.Agent = func(string) (adapter.Adapter, error) { return a, nil }
	code, out, errOut := runInitCtx(ctx, env, "--walk")
	want := "physa init: interrupted: nothing more was started and sessions running were stopped; nothing of the walk was stored. To continue, run physa init --walk.\n"
	if code != 130 || out != "" || errOut[strings.LastIndex(errOut[:len(errOut)-1], "\n")+1:] != want {
		t.Fatalf("code %d\nstdout %s\nstderr %s\nwant last line %q", code, out, errOut, want)
	}
	if strings.Contains(errOut, "context canceled") {
		t.Errorf("stderr:\n%s", errOut)
	}
	if ar, wk, _, _, _ := stored(t, env); !ar || wk {
		t.Errorf("areas stored %v, walk stored %v; want the areas kept and no walk", ar, wk)
	}
}

// TestInterruptedBareRunWhileRecoveringContinuesWithRecover checks that a
// bare run interrupted while recovering gives physa init --recover, with the
// words of the line --recover takes, then --consolidate, to continue, and
// exits 130.
func TestInterruptedBareRunWhileRecoveringContinuesWithRecover(t *testing.T) {
	fourRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a := newCutAgent("recover", cancel)
	env := testEnv(t, onePR(), a)
	code, out, errOut := runInitCtx(ctx, env, "--since", "2025-01-01", "--json")
	if code != 130 || len(a.after) != 0 {
		t.Fatalf("code %d, sessions %v, after the cancel %v\nstdout %s\nstderr %s", code, a.started, a.after, out, errOut)
	}
	if !strings.HasSuffix(errOut, "2 capabilities recovered in this run are kept. To continue, run physa init --recover --json, then physa init --consolidate --json.\n") {
		t.Errorf("stderr:\n%s", errOut)
	}
}

// TestInterruptedBareRunWhileConsolidatingContinuesWithConsolidate checks
// that a bare run interrupted while consolidating exits 130, keeps the four
// capabilities recovered, stores no consolidation, writes nothing though
// --write was typed, and gives physa init --consolidate with the write flag
// and the cap typed to continue.
func TestInterruptedBareRunWhileConsolidatingContinuesWithConsolidate(t *testing.T) {
	fourRepo(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a := newCutAgent("consolidate", cancel)
	env := testEnv(t, onePR(), a)
	code, out, errOut := runInitCtx(ctx, env, "--write", "--cap", "10m")
	t.Logf("sessions %v:\n%s", a.started, errOut)
	if code != 130 || len(a.after) != 0 || a.started[len(a.started)-1] != "consolidate" {
		t.Fatalf("code %d, sessions %v, after the cancel %v\nstdout %s\nstderr %s", code, a.started, a.after, out, errOut)
	}
	want := "physa init: interrupted: nothing more was started and sessions running were stopped; nothing of the consolidation was stored. To continue, run physa init --consolidate --write --cap 10m.\n"
	if !strings.HasSuffix(errOut, want) {
		t.Errorf("stderr does not end with %q:\n%s", want, errOut)
	}
	for _, not := range []string{"context canceled", "could not run", "the cap of", "this run spent"} {
		if strings.Contains(errOut, not) {
			t.Errorf("stderr says %q:\n%s", not, errOut)
		}
	}
	if _, _, _, co, n := stored(t, env); co || n != 4 || branches(t) != "" || strings.Contains(out, "Wrote physa/init") {
		t.Errorf("consolidation %v, %d recovered, branches %q\nstdout %s", co, n, branches(t), out)
	}
}
