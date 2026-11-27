package initrun

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/proc"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
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

// gitEnv is the environment of every git a test runs, its own and the one
// init opens: the test process's, kept off the person's and the system's
// config, with a fixed identity, and never finding a repo above a test's own.
var gitEnv = slices.Concat(os.Environ(), []string{
	"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
	"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
	"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	"GIT_CEILING_DIRECTORIES=/",
})

// gitIn runs git in dir with gitEnv and returns its output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir, cmd.Env = dir, gitEnv
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

// newRepo is a repo with a few commits, in a temp directory of its own.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	write(t, dir, "AGENTS.md", "rules\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	for _, msg := range []string{"feat: ok\n\nCapability: a", "feat: bare", "feat: two"} {
		gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	}
	return dir
}

// testEnv is an Env on the repo at dir, its process state that directory and
// gitEnv, with its own store, the given forge and agent (none installed when
// nil), the real clock, and git opened in the state's environment.
func testEnv(t *testing.T, dir string, f forge.Forge, a adapter.Adapter) Env {
	t.Helper()
	sd := t.TempDir()
	st := proc.State{Dir: dir, Wd: dir, Env: gitEnv}
	return Env{
		Proc:  st,
		Store: func(context.Context, string) (*store.Store, error) { return store.Open(context.Background(), sd) },
		Repo: func(_ context.Context, d string) (vcs.Repo, error) {
			return git.Open(context.Background(), d, st.Env)
		},
		Forge: func(string) forge.Forge { return f },
		Agent: func(string) (adapter.Adapter, error) {
			if a == nil {
				return nil, adapter.ErrNotInstalled
			}
			return a, nil
		},
		Now: time.Now,
		Writer: func(_ context.Context, d string) (vcs.Writer, error) {
			return git.OpenWriter(context.Background(), d, st.Env)
		},
		Spend: filepath.Join(sd, "spend.json"),
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
	st, err := env.Store(context.Background(), env.Proc.Dir)
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
		result:  adapter.Result{Text: proposeReply, Tokens: adapter.Tokens{Input: 100, Output: 20}},
	}
}

func onePR() forge.Forge {
	return mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}
}

func TestGatherJSON(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	write(t, dir, "README.md", "hello\n")
	env := testEnv(t, dir, onePR(), nil)
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
	t.Parallel()
	dir := newRepo(t)
	env := testEnv(t, dir, mergedFake{prs: []forge.PR{{Number: 3, MergedAt: time.Now()}, {Number: 4, MergedAt: time.Now()}}}, nil)
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
	t.Parallel()
	dir := newRepo(t)
	code, _, _ := runInit(testEnv(t, dir, nil, nil), "--gather", "--since", "yesterday")
	if code != command.BadUsage {
		t.Errorf("code %d", code)
	}
}

func TestOutsideARepo(t *testing.T) {
	t.Parallel()
	code, _, errOut := runInit(testEnv(t, t.TempDir(), nil, nil), "--gather")
	if code != 2 || !strings.HasPrefix(errOut, "physa init: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

// proposeReply is a session's text with one capability shown by PR 3.
const proposeReply = "Thinking.\n```json\n" +
	`{"product":{"promise":"Teams keep what they learn about their product."},` +
	`"capabilities":[{"name":"delivery","promise":"Changes reach users in order.","shown_by":["#3"],"questions":[]}],` +
	`"questions":[]}` + "\n```\n"

// gatheredEnv is an Env on a repo with a gather in its store, gathered with
// no agent installed, ready to propose from with agent a.
func gatheredEnv(t *testing.T, a adapter.Adapter) Env {
	t.Helper()
	dir := newRepo(t)
	env := testEnv(t, dir, onePR(), nil)
	if code, _, errOut := runInit(env, "--gather"); code != 0 {
		t.Fatalf("gather: %d %q", code, errOut)
	}
	env.Agent = func(string) (adapter.Adapter, error) { return a, nil }
	return env
}

func TestProposeNoGather(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	f := &fakeAgent{}
	code, out, errOut := runInit(testEnv(t, dir, nil, f), "--propose")
	if code != 2 || out != "" || f.starts != 0 ||
		!strings.Contains(errOut, "physa init: nothing gathered yet; run physa init --gather first") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestProposeStoresAndPrintsMarkdown(t *testing.T) {
	t.Parallel()
	f := oneAgent()
	env := gatheredEnv(t, f)
	code, out, errOut := runInit(env, "--propose")
	if code != 0 || f.starts != 1 || !strings.Contains(out, "delivery") || !strings.Contains(errOut, "propose reads its sources whole") {
		t.Fatalf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("stdout is JSON, not markdown:\n%s", out)
	}
	if !strings.Contains(out, "Coverage: the capabilities cite 1 of 1 gathered PRs;") || strings.Contains(errOut, "coverage is low") {
		t.Errorf("the proposal's coverage:\n%s\n%s", out, errOut)
	}
	st, err := env.Store(context.Background(), env.Proc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, ok, err := initrecord.Of(st).Proposed(context.Background())
	if err != nil || !ok || !strings.Contains(string(p.Body), "delivery") {
		t.Errorf("proposed: ok %v, err %v, body %s", ok, err, p.Body)
	}
}

func TestProposeJSON(t *testing.T) {
	t.Parallel()
	env := gatheredEnv(t, oneAgent())
	code, out, errOut := runInit(env, "--propose", "--json")
	var v map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// TestProposeHasNoCircuitBreaker checks that --propose alone is unbounded:
// a session far over any old appetite still proposes.
func TestProposeHasNoCircuitBreaker(t *testing.T) {
	t.Parallel()
	env := gatheredEnv(t, &fakeAgent{
		reports: []adapter.Tokens{{Input: 900_000, Output: 20}},
		result:  adapter.Result{Text: proposeReply, Tokens: adapter.Tokens{Input: 900_000, Output: 20}},
	})
	if code, out, errOut := runInit(env, "--propose"); code != 0 || !strings.Contains(out, "delivery") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestAppetiteIsBadUsage(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	for _, args := range [][]string{{"--appetite", "1000"}, {"--gather", "--appetite", "1000"}, {"--propose", "--appetite=1000"}, {"--recover", "--appetite", "1000"}} {
		if code, _, _ := runInit(testEnv(t, dir, nil, nil), args...); code != command.BadUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

func TestProposeWithGatherIsBadUsage(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	if code, _, _ := runInit(testEnv(t, dir, nil, nil), "--propose", "--gather"); code != command.BadUsage {
		t.Errorf("code %d", code)
	}
}

// twoReply proposes two capabilities, each shown by PR 3.
const twoReply = "Thinking.\n```json\n" +
	`{"product":{"promise":"Teams keep what they learn about their product."},` +
	`"capabilities":[{"name":"delivery","promise":"Changes reach users in order.","shown_by":["#3"],"questions":[]},` +
	`{"name":"search","promise":"People find what they kept.","shown_by":["#3"],"questions":[]}],` +
	`"questions":[]}` + "\n```\n"

// recoverReply is a session's answer: a capability text with one rule
// anchored to PR 3, and one term.
const recoverReply = "The sources show this.\n\n````markdown\n# Delivery\n\n## Rules\n\n" +
	"### Changes ship in order\n`ship-in-order` · #3\nChanges reach users in the order merged.\n" +
	"Because: a reviewer asked for it in #3.\n````\n\n```glossary\n**Delivery**: A change reaching users.\n```\n"

// proposedEnv is an Env with a gather and a two-capability proposal in its
// store, and a's agent for what follows.
func proposedEnv(t *testing.T, a adapter.Adapter) Env {
	t.Helper()
	env := gatheredEnv(t, &fakeAgent{result: adapter.Result{Text: twoReply}})
	if code, _, errOut := runInit(env, "--propose"); code != 0 {
		t.Fatalf("propose: %d %q", code, errOut)
	}
	env.Agent = func(string) (adapter.Adapter, error) { return a, nil }
	return env
}

func recoverAgent(in int64) *fakeAgent {
	t := adapter.Tokens{Input: in, Output: 20}
	return &fakeAgent{reports: []adapter.Tokens{t}, result: adapter.Result{Text: recoverReply, Tokens: t}}
}

func TestRecoverNoProposal(t *testing.T) {
	t.Parallel()
	f := &fakeAgent{}
	env := gatheredEnv(t, f)
	code, out, errOut := runInit(env, "--recover")
	if code != 2 || out != "" || f.starts != 0 ||
		!strings.Contains(errOut, "physa init: nothing proposed yet; run physa init --propose first") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestRecoverUnknownOnly(t *testing.T) {
	t.Parallel()
	f := recoverAgent(100)
	env := proposedEnv(t, f)
	code, out, errOut := runInit(env, "--recover", "--only", "nope")
	if code != 2 || out != "" || f.starts != 0 || !strings.Contains(errOut, `no capability "nope"`) {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestRecoverBadCombinationsAreBadUsage(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	for _, args := range [][]string{{"--recover", "--gather"}, {"--recover", "--propose"}, {"--only", "x"}} {
		if code, _, _ := runInit(testEnv(t, dir, nil, nil), args...); code != command.BadUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

func TestRecoverText(t *testing.T) {
	t.Parallel()
	f := recoverAgent(100)
	env := proposedEnv(t, f)
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
	st, err := env.Store(context.Background(), env.Proc.Dir)
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
	t.Parallel()
	env := proposedEnv(t, recoverAgent(100))
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
	t.Parallel()
	f := recoverAgent(9_000_000)
	env := proposedEnv(t, f)
	code, out, errOut := runInit(env, "--recover", "--only", "delivery")
	if code != 0 || f.starts != 1 || !strings.Contains(out, "## delivery: recovered") || strings.Contains(errOut, "appetite") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestRecoverFailedExitsTwo(t *testing.T) {
	t.Parallel()
	f := &fakeAgent{result: adapter.Result{Text: "no block here"}}
	env := proposedEnv(t, f)
	code, out, _ := runInit(env, "--recover", "--only", "delivery")
	if code != 2 || !strings.Contains(out, "## delivery: failed") {
		t.Errorf("code %d, stdout %q", code, out)
	}
}

func TestStoredMapGivesOneSessionPerShareAndTheMerge(t *testing.T) {
	t.Parallel()
	env := proposedEnv(t, nil)
	st, err := env.Store(context.Background(), env.Proc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	prop, ok, err := initrecord.Of(st).Proposed(context.Background())
	if err != nil || !ok {
		t.Fatalf("proposed: ok %v, err %v", ok, err)
	}
	m, err := storedMap(t.Context(), initrecord.Of(st), "repo", env)
	if err != nil || len(m.Sessions) != 1 || m.Sessions[0] != prop.SessionID {
		t.Fatalf("one session: %v, %v; want [%s]", m.Sessions, err, prop.SessionID)
	}
	var s mapping.Stored
	if err := json.Unmarshal(prop.Body, &s); err != nil {
		t.Fatal(err)
	}
	s.Sessions = []string{"share-1", "share-2", "merge"}
	if prop.Body, err = json.Marshal(s); err != nil {
		t.Fatal(err)
	}
	if err := initrecord.Of(st).PutProposal(context.Background(), prop); err != nil {
		t.Fatal(err)
	}
	if m, err = storedMap(t.Context(), initrecord.Of(st), "repo", env); err != nil || !reflect.DeepEqual(m.Sessions, s.Sessions) {
		t.Errorf("in shares: %v, %v; want %v", m.Sessions, err, s.Sessions)
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
	t.Parallel()
	dir := newRepo(t)
	code, _, errOut := runInit(testEnv(t, dir, downForge{}, nil), "--gather")
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
	t.Parallel()
	dir := newRepo(t)
	a := warnAgent{oneAgent()}
	var stderr bytes.Buffer
	env := testEnv(t, dir, onePR(), a)
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
	t.Parallel()
	env, _ := placeEnv(t)
	if code, _, errOut := runInit(env, "--place"); code != 0 {
		t.Fatalf("place: %d %s", code, errOut)
	}
	st, err := env.Store(context.Background(), env.Proc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rec := initrecord.Of(st)
	w, ok, err := rec.Walk(context.Background())
	if err != nil || !ok {
		t.Fatalf("walk: %v, %v", ok, err)
	}
	// A walk only: no proposal is stored, and the map still lists the walk's.
	m, err := storedMap(t.Context(), rec, "repo", env)
	if err != nil || len(m.Capabilities) != 2 || m.Capabilities[0].Name != "zz-cap-marker" || m.ProductPromise != "Not yet stated." {
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
