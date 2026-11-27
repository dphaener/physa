package initrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
	"github.com/dphaener/physa/internal/walk"
)

// mergedFake is a forge that lists the merged PRs it was given.
type mergedFake struct{ prs []forge.PR }

func (mergedFake) Name() string                                { return "fake" }
func (mergedFake) PRExists(context.Context, int) (bool, error) { return false, nil }
func (mergedFake) ChangePR(context.Context, string) (forge.PR, bool, error) {
	return forge.PR{}, false, nil
}
func (m mergedFake) MergedPRs(context.Context, time.Time, int) ([]forge.PR, bool, error) {
	return m.prs, false, nil
}

// fakeAgent replays scripted usage and text, and records its starts.
type fakeAgent struct {
	reports []adapter.Tokens
	result  adapter.Result
	starts  int
}

func (f *fakeAgent) Name() string { return "fake" }
func (f *fakeAgent) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	f.starts++
	return fakeSession{f: f, usage: s.Usage}, nil
}

type fakeSession struct {
	f     *fakeAgent
	usage func(adapter.Tokens)
}

func (s fakeSession) Wait() (adapter.Result, error) {
	for _, t := range s.f.reports {
		if s.usage != nil {
			s.usage(t)
		}
	}
	return s.f.result, nil
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo is a repo with a few commits, made in the working directory of the
// test.
func newRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	write(t, dir, "AGENTS.md", "rules\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	for _, msg := range []string{"feat: ok\n\nCapability: a", "feat: bare", "feat: two"} {
		gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	}
	t.Chdir(dir)
	return dir
}

// testEnv is an Env on the repo in the working directory, with its own store,
// the given forge and agent (none installed when nil), and the real clock.
func testEnv(t *testing.T, f forge.Forge, a adapter.Adapter) Env {
	t.Helper()
	sd := t.TempDir()
	return Env{
		Store: func(context.Context, string) (*store.Store, error) { return store.Open(context.Background(), sd) },
		Repo:  func(_ context.Context, dir string) (vcs.Repo, error) { return git.Open(context.Background(), dir) },
		Forge: func(string) forge.Forge { return f },
		Agent: func(string) (adapter.Adapter, error) {
			if a == nil {
				return nil, adapter.ErrNotInstalled
			}
			return a, nil
		},
		Now:    time.Now,
		Writer: func(_ context.Context, d string) (vcs.Writer, error) { return git.OpenWriter(context.Background(), d) },
		Spend:  filepath.Join(sd, "spend.json"),
	}
}

// runInit runs "physa init" with args through init's declared command and
// Run, returning Run's own code (command.BadUsage for a bad line) and its
// output.
func runInit(env Env, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := runInitTo(&stdout, &stderr, env, args...)
	return code, stdout.String(), stderr.String()
}

func runInitTo(stdout, stderr io.Writer, env Env, args ...string) int {
	return execute(args, stdout, stderr, func(f Flags, pos, words []string, stdout, stderr io.Writer) int {
		return Run(context.Background(), f, pos, words, stdout, stderr, env)
	})
}

func storedGather(t *testing.T, env Env) store.Gather {
	t.Helper()
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g, ok, err := st.Gathered(context.Background())
	if err != nil || !ok {
		t.Fatalf("gathered: ok %v, err %v", ok, err)
	}
	return g
}

func oneAgent() *fakeAgent {
	return &fakeAgent{
		reports: []adapter.Tokens{{Input: 100, Output: 20}},
		result:  adapter.Result{Text: "```json\n{\"sources\":[]}\n```\n", Tokens: adapter.Tokens{Input: 100, Output: 20}},
	}
}

func onePR() forge.Forge {
	return mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}
}

func TestGatherJSON(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "README.md", "hello\n")
	env := testEnv(t, onePR(), nil)
	code, out, errOut := runInit(env, "--gather", "--json")
	if code != 0 || errOut != "physa init: gather, with the discovery agent; init has no appetite and no circuit breaker\n" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var sum gather.Summary
	if err := json.Unmarshal([]byte(out), &sum); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if sum.Docs.Count < 2 || sum.History.Changes < 4 || sum.PRs.Count != 1 || !sum.PRs.Available {
		t.Errorf("summary %+v", sum)
	}
	if d := sum.Discovery; d.Started || !strings.HasPrefix(d.Why, "no agent: ") {
		t.Errorf("with no agent installed, discovery %+v", d)
	}
	g := storedGather(t, env)
	if len(g.Docs) != sum.Docs.Count || len(g.Changes) != sum.History.Changes || len(g.PRs) != 1 {
		t.Errorf("store holds %d docs, %d changes, %d PRs; summary %+v", len(g.Docs), len(g.Changes), len(g.PRs), sum)
	}
}

func TestGatherReplaces(t *testing.T) {
	newRepo(t)
	env := testEnv(t, mergedFake{prs: []forge.PR{{Number: 3, MergedAt: time.Now()}, {Number: 4, MergedAt: time.Now()}}}, nil)
	if code, _, errOut := runInit(env, "--gather"); code != 0 {
		t.Fatalf("first run: %d %q", code, errOut)
	}
	if n := len(storedGather(t, env).PRs); n != 2 {
		t.Fatalf("first run stored %d PRs", n)
	}
	env.Forge = func(string) forge.Forge { return mergedFake{prs: []forge.PR{{Number: 9, MergedAt: time.Now()}}} }
	code, out, errOut := runInit(env, "--gather")
	if code != 0 || !strings.Contains(out, "Gathered into") {
		t.Fatalf("second run: %d %q %q", code, out, errOut)
	}
	g := storedGather(t, env)
	if len(g.PRs) != 1 || g.PRs[0].Number != 9 {
		t.Errorf("second run left %+v", g.PRs)
	}
}

func TestBadSinceIsBadUsage(t *testing.T) {
	newRepo(t)
	code, _, _ := runInit(testEnv(t, nil, nil), "--gather", "--since", "yesterday")
	if code != command.BadUsage {
		t.Errorf("code %d", code)
	}
}

func TestOutsideARepo(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", "/")
	code, _, errOut := runInit(testEnv(t, nil, nil), "--gather")
	if code != 2 || !strings.HasPrefix(errOut, "physa init: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

// gatheredEnv is an Env on a repo with a gather in its store, gathered with
// no agent installed, with agent a for what follows.
func gatheredEnv(t *testing.T, a adapter.Adapter) Env {
	t.Helper()
	newRepo(t)
	env := testEnv(t, onePR(), nil)
	if code, _, errOut := runInit(env, "--gather"); code != 0 {
		t.Fatalf("gather: %d %q", code, errOut)
	}
	env.Agent = func(string) (adapter.Adapter, error) { return a, nil }
	return env
}

func TestAppetiteIsBadUsage(t *testing.T) {
	newRepo(t)
	for _, args := range [][]string{{"--appetite", "1000"}, {"--gather", "--appetite", "1000"}, {"--areas", "--appetite=1000"}, {"--recover", "--appetite", "1000"}} {
		if code, _, _ := runInit(testEnv(t, nil, nil), args...); code != command.BadUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

// recoverReply is a session's answer: a capability text with one rule
// anchored to PR 3, and one term.
const recoverReply = "The sources show this.\n\n````markdown\n# Delivery\n\n## Rules\n\n" +
	"### Changes ship in order\n`ship-in-order` · #3\nChanges reach users in the order merged.\n" +
	"Because: a reviewer asked for it in #3.\n````\n\n```glossary\n**Delivery**: A change reaching users.\n```\n"

// walkedEnv is areasEnv with the areas, the walk of the capabilities delivery
// and search and their placements in its store, and a's agent for what
// follows.
func walkedEnv(t *testing.T, a adapter.Adapter) Env {
	t.Helper()
	env := areasEnv(t, newRoutedAgent())
	for _, phase := range []string{"--areas", "--walk", "--place"} {
		if code, _, errOut := runInit(env, phase); code != 0 {
			t.Fatalf("%s: %d %q", phase, code, errOut)
		}
	}
	env.Agent = func(string) (adapter.Adapter, error) { return a, nil }
	return env
}

func recoverAgent(in int64) *fakeAgent {
	t := adapter.Tokens{Input: in, Output: 20}
	return &fakeAgent{reports: []adapter.Tokens{t}, result: adapter.Result{Text: recoverReply, Tokens: t}}
}

// TestRecoverWithoutAWalk checks that --recover on a gather with no walk and
// placements stored starts no session and says what to run first.
func TestRecoverWithoutAWalk(t *testing.T) {
	f := &fakeAgent{}
	env := gatheredEnv(t, f)
	code, out, errOut := runInit(env, "--recover")
	if code != 2 || out != "" || f.starts != 0 || errOut != "physa init: "+recovery.ErrNoWalk.Error()+"\n" {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestRecoverUnknownOnly(t *testing.T) {
	f := recoverAgent(100)
	env := walkedEnv(t, f)
	code, out, errOut := runInit(env, "--recover", "--only", "nope")
	if code != 2 || out != "" || f.starts != 0 || !strings.Contains(errOut, `no capability "nope"`) {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestRecoverBadCombinationsAreBadUsage(t *testing.T) {
	newRepo(t)
	for _, args := range [][]string{{"--recover", "--gather"}, {"--recover", "--areas"}, {"--only", "x"}} {
		if code, _, _ := runInit(testEnv(t, nil, nil), args...); code != command.BadUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

func TestRecoverText(t *testing.T) {
	f := recoverAgent(100)
	env := walkedEnv(t, f)
	code, out, errOut := runInit(env, "--recover", "--only", "delivery")
	if code != 0 || f.starts != 1 {
		t.Fatalf("code %d, starts %d, stderr %q", code, f.starts, errOut)
	}
	for _, want := range []string{
		"Recovered 1, not yet recovered 0, failed 0; used 120 tokens.",
		"## delivery: recovered", "Kept 1 rule, 0 decisions, 0 questions, 1 term.", "Attempts: 1; session ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "physa init: delivery: recovered") {
		t.Errorf("stderr %q", errOut)
	}
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rs, err := initrecord.Of(st).Recovered(context.Background())
	if err != nil || len(rs) != 1 || rs[0].Capability != "delivery" || !strings.Contains(rs[0].File, "ship-in-order") {
		t.Errorf("stored %+v, err %v", rs, err)
	}
}

func TestRecoverJSON(t *testing.T) {
	env := walkedEnv(t, recoverAgent(100))
	code, out, errOut := runInit(env, "--recover", "--only=delivery", "--json")
	var rep struct {
		Capabilities []struct{ Name, Status, File, Terms string }
	}
	if code != 0 || json.Unmarshal([]byte(out), &rep) != nil || len(rep.Capabilities) != 1 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	c := rep.Capabilities[0]
	if c.Status != "recovered" || !strings.Contains(c.File, "ship-in-order") || !strings.Contains(c.Terms, "Delivery") {
		t.Errorf("report %+v", rep)
	}
}

// TestRecoverHasNoAppetite checks that --recover alone starts every session
// however large the prompts, with no refusal.
func TestRecoverHasNoAppetite(t *testing.T) {
	f := recoverAgent(9_000_000)
	env := walkedEnv(t, f)
	code, out, errOut := runInit(env, "--recover", "--only", "delivery")
	if code != 0 || f.starts != 1 || !strings.Contains(out, "## delivery: recovered") || strings.Contains(errOut, "appetite") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestRecoverFailedExitsTwo(t *testing.T) {
	f := &fakeAgent{result: adapter.Result{Text: "no block here"}}
	env := walkedEnv(t, f)
	code, out, _ := runInit(env, "--recover", "--only", "delivery")
	if code != 2 || !strings.Contains(out, "## delivery: failed") {
		t.Errorf("code %d, stdout %q", code, out)
	}
}

// TestStoredMapNamesTheAreasAndWalkSessionsBeforeTheRecoveries checks that
// the map's sessions, which become the commit's Session: trailers, are the
// stored areas' and the stored walk's, then each recovery's, and that its
// product is the one the stored areas state.
func TestStoredMapNamesTheAreasAndWalkSessionsBeforeTheRecoveries(t *testing.T) {
	a := newRoutedAgent()
	env := walkedEnv(t, a)
	if code, _, errOut := runInit(env, "--recover"); code != 0 {
		t.Fatalf("recover: %d %s", code, errOut)
	}
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rec := initrecord.Of(st)
	ar, _, errA := areas.Load(t.Context(), rec)
	wr, _, errW := walk.Load(t.Context(), rec)
	rows, errR := rec.Recovered(t.Context())
	if err := errors.Join(errA, errW, errR); err != nil || len(ar.Sessions) != 2 || len(wr.Sessions) != 2 || len(rows) != 2 {
		t.Fatalf("stored sessions: areas %v, walk %v, %d recovered: %v", ar.Sessions, wr.Sessions, len(rows), err)
	}
	want := slices.Concat(ar.Sessions, wr.Sessions)
	byName := map[string]string{}
	for _, r := range rows {
		byName[r.Capability] = r.SessionID
	}
	for _, c := range wr.Capabilities {
		want = append(want, byName[c.Name])
	}
	m, err := storedMap(t.Context(), rec, "some-repo", env)
	if err != nil || !slices.Equal(m.Sessions, want) || slices.Contains(want, "") {
		t.Errorf("sessions %v, %v; want %v", m.Sessions, err, want)
	}
	if m.ProductName != "Keeper" || m.ProductPromise != "Teams keep what they learn about their product." {
		t.Errorf("product %q: %q", m.ProductName, m.ProductPromise)
	}
}

// downForge is a forge that cannot list PRs, as one without auth.
type downForge struct{ mergedFake }

func (downForge) MergedPRs(context.Context, time.Time, int) ([]forge.PR, bool, error) {
	return nil, false, &forge.Error{Class: forge.ErrUnauthenticated, Reason: "gh is not logged in"}
}

// TestGatherWarnsWhenThePRsAreMissing checks that a gather the forge gave
// no PRs says on stderr that the map will be thin, and still gathers.
func TestGatherWarnsWhenThePRsAreMissing(t *testing.T) {
	newRepo(t)
	code, _, errOut := runInit(testEnv(t, downForge{}, nil), "--gather")
	if code != 0 || !strings.Contains(errOut, "physa init: warning: no PRs were gathered (gh is not logged in), so the map comes from history and docs alone and will be thin.") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

// warnAgent is a fake agent that warns of its setup.
type warnAgent struct{ *fakeAgent }

func (warnAgent) Warnings() []string { return []string{"KEY is set"} }

// TestAgentWarningsComeBeforeAnySession checks that what the agent warns
// of is said before its first session starts, and that init goes on.
func TestAgentWarningsComeBeforeAnySession(t *testing.T) {
	newRepo(t)
	a := warnAgent{oneAgent()}
	var stderr bytes.Buffer
	env := testEnv(t, onePR(), a)
	starts := 0
	env.Agent = func(string) (adapter.Adapter, error) {
		return startSpy{warnAgent: a, before: func() {
			if starts++; starts == 1 && !strings.Contains(stderr.String(), "physa init: warning: KEY is set\n") {
				t.Errorf("a session started before the warning: %q", stderr.String())
			}
		}}, nil
	}
	if code := runInitTo(io.Discard, &stderr, env, "--gather"); code != 0 || starts == 0 {
		t.Errorf("code %d, %d sessions, stderr %q", code, starts, stderr.String())
	}
}

// startSpy calls before as each session starts.
type startSpy struct {
	warnAgent
	before func()
}

func (s startSpy) Start(ctx context.Context, spec adapter.Spec) (adapter.Session, error) {
	s.before()
	return s.warnAgent.Start(ctx, spec)
}

func TestStoredMapWritesTheWalksCapabilitiesAndSkipsRowsOfAnEarlierSource(t *testing.T) {
	env, _ := placeEnv(t)
	if code, _, errOut := runInit(env, "--place"); code != 0 {
		t.Fatalf("place: %d %s", code, errOut)
	}
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rec := initrecord.Of(st)
	w, ok, err := rec.Walk(context.Background())
	if err != nil || !ok {
		t.Fatalf("walk: %v, %v", ok, err)
	}
	// A walk with nothing recovered: the map lists the walk's capabilities, and
	// the areas stated no product, so it is named after the folder.
	m, err := storedMap(t.Context(), rec, "some-repo", env)
	if err != nil || len(m.Capabilities) != 2 || m.Capabilities[0].Name != "zz-cap-marker" || m.ProductPromise != "Not yet stated." || m.ProductName != "Some Repo" {
		t.Fatalf("walk only: %+v, %v", m, err)
	}
	rows := []initrecord.Recovered{
		{Capability: "zz-cap-marker", ProposalAt: w.At, Status: recovery.StatusRecovered, File: "# Marker\n", Terms: "term"},
		{Capability: "zz-cap-second", ProposalAt: w.At.Add(-time.Hour), Status: recovery.StatusRecovered, File: "# Stale\n"},
		{Capability: "delivery", ProposalAt: w.At, Status: recovery.StatusRecovered, File: "# Delivery\n"},
	}
	if err := rec.PutRecovered(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	if m, err = storedMap(t.Context(), rec, "repo", env); err != nil || len(m.Capabilities) != 2 {
		t.Fatalf("with rows: %+v, %v", m, err)
	}
	if c := m.Capabilities[0]; c.File != "# Marker\n" || c.Status != recovery.StatusRecovered {
		t.Errorf("walk capability: %+v", c)
	}
	if c := m.Capabilities[1]; c.File != "" || c.Status != recovery.StatusNotYet {
		t.Errorf("a row of an earlier source was written: %+v", c)
	}
	if m.Terms != "term" {
		t.Errorf("terms %q", m.Terms)
	}
}
