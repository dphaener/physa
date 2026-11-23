package initrun

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// mergedFake is a forge that lists the merged PRs it was given.
type mergedFake struct{ prs []forge.PR }

func (mergedFake) Name() string                            { return "fake" }
func (mergedFake) PRExists(int) (bool, error)              { return false, nil }
func (mergedFake) ChangePR(string) (forge.PR, bool, error) { return forge.PR{}, false, nil }
func (m mergedFake) MergedPRs(time.Time, int) ([]forge.PR, bool, error) {
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
		s.usage(t)
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
		Store: func(string) (*store.Store, error) { return store.Open(sd) },
		Repo:  func(dir string) (vcs.Repo, error) { return git.Open(dir) },
		Forge: func(string) forge.Forge { return f },
		Agent: func(string) (adapter.Adapter, error) {
			if a == nil {
				return nil, adapter.ErrNotInstalled
			}
			return a, nil
		},
		Now:    time.Now,
		Writer: git.OpenWriter,
	}
}

func runInit(env Env, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr, env)
	return code, stdout.String(), stderr.String()
}

func storedGather(t *testing.T, env Env) store.Gather {
	t.Helper()
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g, ok, err := st.Gathered()
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
	dir := newRepo(t)
	write(t, dir, "README.md", "hello\n")
	env := testEnv(t, onePR(), nil)
	code, out, errOut := runInit(env, "--gather", "--json")
	if code != 0 || errOut != "physa init: gather up to 100000 tokens (the discovery agent)\n" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var sum gather.Summary
	if err := json.Unmarshal([]byte(out), &sum); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if sum.Docs.Count < 2 || sum.History.Changes < 4 || sum.PRs.Count != 1 || !sum.PRs.Available {
		t.Errorf("summary %+v", sum)
	}
	if d := sum.Discovery; d.Started || !strings.HasPrefix(d.Why, "no agent: ") || d.Cap != 100_000 {
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
	if code != cli.BadUsage {
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

// proposeReply is a session's text with one capability shown by PR 3.
const proposeReply = "Thinking.\n```json\n" +
	`{"product":{"promise":"Teams keep what they learn about their product."},` +
	`"capabilities":[{"name":"delivery","promise":"Changes reach users in order.","shown_by":["#3"],"questions":[]}],` +
	`"questions":[]}` + "\n```\n"

// gatheredEnv is an Env on a repo with a gather in its store, gathered with
// no agent installed, ready to propose from with agent a.
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

func TestProposeNoGather(t *testing.T) {
	newRepo(t)
	f := &fakeAgent{}
	code, out, errOut := runInit(testEnv(t, nil, f), "--propose")
	if code != 2 || out != "" || f.starts != 0 ||
		!strings.Contains(errOut, "physa init: nothing gathered yet; run physa init --gather first") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestProposeStoresAndPrintsMarkdown(t *testing.T) {
	f := oneAgent()
	env := gatheredEnv(t, f)
	code, out, errOut := runInit(env, "--propose", "--appetite", "100000")
	if code != 0 || f.starts != 1 || !strings.Contains(out, "delivery") || !strings.Contains(errOut, "appetite 100000 tokens") {
		t.Fatalf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("stdout is JSON, not markdown:\n%s", out)
	}
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, ok, err := st.Proposed()
	if err != nil || !ok || !strings.Contains(string(p.Body), "delivery") {
		t.Errorf("proposed: ok %v, err %v, body %s", ok, err, p.Body)
	}
}

func TestProposeJSON(t *testing.T) {
	env := gatheredEnv(t, oneAgent())
	code, out, errOut := runInit(env, "--propose", "--json")
	var v map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestProposeBreakerStops(t *testing.T) {
	env := gatheredEnv(t, &fakeAgent{
		reports: []adapter.Tokens{{Input: 900_000, Output: 20}},
		result:  adapter.Result{Text: proposeReply, Tokens: adapter.Tokens{Input: 900_000, Output: 20}},
	})
	code, _, errOut := runInit(env, "--propose", "--appetite", "100000")
	if code != 1 || !strings.Contains(errOut, "physa init: the circuit breaker stopped the session at 100000 tokens; nothing was proposed") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestProposeWithGatherIsBadUsage(t *testing.T) {
	newRepo(t)
	if code, _, _ := runInit(testEnv(t, nil, nil), "--propose", "--gather"); code != cli.BadUsage {
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
	f := &fakeAgent{}
	env := gatheredEnv(t, f)
	code, out, errOut := runInit(env, "--recover")
	if code != 2 || out != "" || f.starts != 0 ||
		!strings.Contains(errOut, "physa init: nothing proposed yet; run physa init --propose first") {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestRecoverUnknownOnly(t *testing.T) {
	f := recoverAgent(100)
	env := proposedEnv(t, f)
	code, out, errOut := runInit(env, "--recover", "--only", "nope")
	if code != 2 || out != "" || f.starts != 0 || !strings.Contains(errOut, `no capability "nope"`) {
		t.Errorf("code %d, starts %d, stdout %q, stderr %q", code, f.starts, out, errOut)
	}
}

func TestRecoverBadCombinationsAreBadUsage(t *testing.T) {
	newRepo(t)
	for _, args := range [][]string{{"--recover", "--gather"}, {"--recover", "--propose"}, {"--only", "x"}} {
		if code, _, _ := runInit(testEnv(t, nil, nil), args...); code != cli.BadUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

func TestRecoverText(t *testing.T) {
	f := recoverAgent(100)
	env := proposedEnv(t, f)
	code, out, errOut := runInit(env, "--recover", "--only", "delivery", "--appetite", "100000")
	if code != 0 || f.starts != 1 {
		t.Fatalf("code %d, starts %d, stderr %q", code, f.starts, errOut)
	}
	for _, want := range []string{
		"Recovered 1, not yet recovered 0, failed 0; used 120 of 100000 tokens.",
		"## delivery: recovered", "Kept 1 rule, 0 decisions, 0 questions, 1 term.", "Attempts: 1; session ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "physa init: delivery: recovered") {
		t.Errorf("stderr %q", errOut)
	}
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rs, err := st.Recovered()
	if err != nil || len(rs) != 1 || rs[0].Capability != "delivery" || !strings.Contains(rs[0].File, "ship-in-order") {
		t.Errorf("stored %+v, err %v", rs, err)
	}
}

func TestRecoverJSON(t *testing.T) {
	env := proposedEnv(t, recoverAgent(100))
	code, out, errOut := runInit(env, "--recover", "--only=delivery", "--json")
	var rep struct {
		Appetite     int64
		Capabilities []struct{ Name, Status, File, Terms string }
	}
	if code != 0 || json.Unmarshal([]byte(out), &rep) != nil || len(rep.Capabilities) != 1 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	c := rep.Capabilities[0]
	if rep.Appetite != 300000 || c.Status != "recovered" || !strings.Contains(c.File, "ship-in-order") || !strings.Contains(c.Terms, "Delivery") {
		t.Errorf("report %+v", rep)
	}
}

func TestRecoverSmallAppetiteRefusesBeforeAnySession(t *testing.T) {
	env := proposedEnv(t, recoverAgent(10_000))
	code, out, errOut := runInit(env, "--recover", "--appetite", "25000")
	if code != 1 || out != "" || !strings.Contains(errOut, "physa init: stopped before recovering: recovering 2 capabilities needs an appetite of about ") ||
		!strings.Contains(errOut, " and has 25,000; no session started, and nothing was spent on recovering. The proposal is stored: physa init --recover --appetite ") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	code, out, errOut = runInit(env, "--recover", "--only", "search,delivery", "--agent", "fake", "--appetite", "1000", "--write")
	if code != 1 || out != "" || !regexp.MustCompile(`The proposal is stored: physa init --recover --only search,delivery --appetite \d+ --agent fake --write continues\.\n$`).MatchString(errOut) {
		t.Errorf("with --only: code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestRecoverFailedExitsTwo(t *testing.T) {
	f := &fakeAgent{result: adapter.Result{Text: "no block here"}}
	env := proposedEnv(t, f)
	code, out, _ := runInit(env, "--recover", "--only", "delivery")
	if code != 2 || !strings.Contains(out, "## delivery: failed") {
		t.Errorf("code %d, stdout %q", code, out)
	}
}
