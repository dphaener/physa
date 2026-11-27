package initrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// prForge lists PR 3 as merged and holds at most one PR of its own,
// recording each it opens and each body it writes.
type prForge struct {
	mergedFake
	mu      sync.Mutex
	open    bool
	created []string // "head→base: title"
	bodies  []string
	edits   int
}

func (f *prForge) OpenPR(context.Context, string) (int, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return 7, "https://example.com/pr/7", f.open, nil
}

func (f *prForge) CreatePR(_ context.Context, head, base, title, body string) (int, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open = true
	f.created = append(f.created, head+"→"+base+": "+title)
	f.bodies = append(f.bodies, body)
	return 7, "https://example.com/pr/7", nil
}

func (f *prForge) EditPR(context.Context, int, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits++
	return nil
}

// The recovering replies, one per capability. delivery's holds a rule with
// no Because: line, which the check drops after its retries.
const (
	deliveryReply = "````markdown\n# Delivery\n\n## Rules\n\n" +
		"### Changes ship in order\n`ship-in-order` · #3\nChanges reach users in the order merged.\n" +
		"Because: a reviewer asked for it in #3.\n\n" +
		"### Changes ship fast\n`ship-fast` · #3\nChanges reach users within a day.\n````\n\n" +
		"```glossary\n**Delivery**: A change reaching users.\n```\n"
	searchReply = "````markdown\n# Search\n\n## Rules\n\n" +
		"### Kept things are found\n`find-kept` · #3\nPeople find what they kept by its words.\n" +
		"Because: #3 asked for it.\n````\n\n```glossary\n**Search**: Finding what was kept.\n```\n"
)

// discoveryMarker is a line of the discovery skill, which only a discovery
// session's prompt holds.
const discoveryMarker = "You find the sources a product's map is built from"

// routedAgent answers discovery with discover (naming nothing when empty),
// proposing with twoReply and each capability's recovering with its reply,
// joining a resumed session to its capability. It records each session it
// started, by "discover", "propose" or the capability, and calls first,
// when set, before its first start.
type routedAgent struct {
	mu        sync.Mutex
	replies   map[string]string
	sessions  map[string]string // session id → capability
	started   []string          // "discover", "propose" or the capability
	first     func()
	propose   int64          // the input tokens proposing uses; zero means 10,000
	discover  string         // discovery's reply
	discovers adapter.Tokens // the tokens discovery uses; zero means 1,000 in, 10 out
	recovers  int64          // the input tokens each recovering session uses; zero means 10,000
	readOnly  bool           // every discovery session was read-only
	priced    bool           // each result carries its cost, at $4 a million input
	writes1h  bool           // with priced, uncached input is written to a 1-hour cache, at 2× input
}

func newRoutedAgent() *routedAgent {
	return &routedAgent{replies: map[string]string{"delivery": deliveryReply, "search": searchReply}, sessions: map[string]string{}}
}

func (a *routedAgent) Name() string { return "fake" }

func (a *routedAgent) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.started) == 0 && a.first != nil {
		a.first()
	}
	name := a.sessions[s.SessionID]
	if !s.Resume {
		name = "propose"
		for c := range a.replies {
			if strings.Contains(s.Prompt, "\nname: "+c+"\n") {
				name = c
			}
		}
		if strings.Contains(s.Prompt, discoveryMarker) {
			name = "discover"
			a.readOnly = (len(a.started) == 0 || a.readOnly) && s.ReadOnly && !s.PromptOnly
		}
		a.sessions[s.SessionID] = name
		a.started = append(a.started, name)
	}
	text := twoReply
	t := adapter.Tokens{Input: 10_000, CachedInput: 5, Output: 20}
	switch {
	case name == "discover":
		text, t = "```json\n{\"sources\":[]}\n```\n", adapter.Tokens{Input: 1_000, Output: 10}
		if a.discover != "" {
			text = a.discover
		}
		if a.discovers != (adapter.Tokens{}) {
			t = a.discovers
		}
	case name != "propose":
		text = a.replies[name]
		if a.recovers != 0 {
			t.Input = a.recovers
		}
	case a.propose != 0:
		t.Input = a.propose
	}
	res := adapter.Result{Text: text, Tokens: t, SessionID: s.SessionID}
	if a.priced {
		res.Model = "claude-opus-5-5"
		c := adapter.ModelCost{Model: res.Model, Input: t.Input - t.CachedInput, CacheRead: t.CachedInput, Output: t.Output}
		c.MicroUSD = 4*c.Input + c.CacheRead/5 + 20*c.Output // $4, $0.20 and $20 a million
		if a.writes1h {
			c.CacheWrite1h, c.Input = c.Input, 0
			c.MicroUSD += 4 * c.CacheWrite1h // $8 a million: twice input
		}
		res.Cost = adapter.Cost{c}
	}
	return fakeSession{f: &fakeAgent{reports: []adapter.Tokens{t}, result: res}, usage: s.Usage}, nil
}

// remoteRepo is the person's clone of a local bare remote, with AGENTS.md,
// a few changes, an uncommitted edit and an untracked file, made the test's
// working directory. It returns the clone and the bare remote.
func remoteRepo(t *testing.T) (dir, bare string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[user]\n\tname = W\n\temail = w@example.com\n[commit]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, bare = t.TempDir(), t.TempDir()
	gitIn(t, bare, "init", "-q", "--bare")
	gitIn(t, dir, "init", "-q")
	write(t, dir, "AGENTS.md", "Our rules.\n")
	write(t, dir, "main.go", "package main\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: Start")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "feat: More")
	gitIn(t, dir, "remote", "add", "origin", bare)
	gitIn(t, dir, "push", "-q", "origin", "main")
	gitIn(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	write(t, dir, "main.go", "package main // edited\n")
	gitIn(t, dir, "add", "main.go")
	write(t, dir, "scratch.txt", "mine\n")
	t.Chdir(dir)
	return dir, bare
}

// fullEnv is an Env on the repo in the working directory with its own
// store, forge f and agent a, and the git writer and hooks.
func fullEnv(t *testing.T, f forge.Forge, a adapter.Adapter) Env {
	env := testEnv(t, f, a)
	env.Writer = func(_ context.Context, d string) (vcs.Writer, error) { return git.OpenWriter(context.Background(), d) }
	env.Hooks = func(_ context.Context, d string) (vcs.Hooks, error) { return git.OpenHooks(context.Background(), d) }
	env.Stdin = strings.NewReader("")
	env.IsTerminal = func() bool { return false }
	return env
}

// personState is the person's working copy, index and HEAD.
func personState(t *testing.T, dir string) string {
	return gitIn(t, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=all") + "|" +
		gitIn(t, dir, "rev-parse", "HEAD") + "|" + gitIn(t, dir, "diff", "--cached")
}

// changed lists the files ref changes from main in dir.
func changed(t *testing.T, dir, ref string) []string {
	return strings.Fields(gitIn(t, dir, "diff", "--name-only", "main", ref))
}

// checkTree checks out ref of the repo at dir in a fresh clone and runs the
// map check on it.
func checkTree(t *testing.T, dir, ref string) {
	t.Helper()
	clone := t.TempDir()
	gitIn(t, clone, "clone", "-q", "--branch", ref, dir, ".")
	res, err := mapcheck.CheckWith(t.Context(), clone, mapcheck.Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) { return git.Open(context.Background(), d) }})
	if err != nil || len(res.Problems) > 0 {
		t.Errorf("map check on %s: %v %v", ref, err, res.Problems)
	}
}

func TestFullWritePushPR(t *testing.T) {
	dir, bare := remoteRepo(t)
	before := personState(t, dir)
	f := &prForge{mergedFake: mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}}
	a := newRoutedAgent()
	code, out, errOut := runInit(fullEnv(t, f, a), "--write", "--push", "--pr")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	want := []string{".physa/README.md", ".physa/capabilities/delivery/index.md", ".physa/capabilities/delivery/rules.md",
		".physa/capabilities/search/index.md", ".physa/capabilities/search/rules.md", ".physa/glossary.md", ".physa/product/index.md", "AGENTS.md"}
	if got := changed(t, bare, "physa/init"); !slices.Equal(got, want) {
		t.Errorf("the remote's physa/init changes %v, want %v", got, want)
	}
	checkTree(t, bare, "physa/init")
	if after := personState(t, dir); after != before {
		t.Errorf("the person's working copy changed:\nbefore %s\nafter  %s", before, after)
	}
	if len(f.created) != 1 || f.created[0] != "physa/init→main: docs(map): Add the map recovered by physa init" || f.edits != 0 {
		t.Fatalf("PRs created %v, edits %d", f.created, f.edits)
	}
	for _, s := range []string{"`delivery`: Changes reach users in order.", "`search`: People find what they kept.",
		"delivery: rule \"Changes ship fast (ship-fast)\"", "| propose | 10000 | 5 | 20 | 10k | none: no price yet |", "| recover |", "| total |",
		"| Weighted | Cost estimate |",
		"## The Session: hook\n\nNot asked for."} {
		if !strings.Contains(f.bodies[0], s) {
			t.Errorf("PR body lacks %q:\n%s", s, f.bodies[0])
		}
	}
	for _, s := range []string{"Gathered into", "## delivery: recovered", "Wrote physa/init at ", "Pushed: true", "PR: opened #7", "## Tokens spent"} {
		if !strings.Contains(out, s) {
			t.Errorf("stdout lacks %q:\n%s", s, out)
		}
	}
	msg := gitIn(t, bare, "log", "-1", "--format=%B", "physa/init")
	if strings.Count(msg, "Session: ") != 3 || strings.Contains(msg, "Capability:") {
		t.Errorf("commit message:\n%s", msg)
	}
}

// noPricesLine is the full run's first line on a machine that knows no
// prices yet.
const noPricesLine = "physa init: no appetite and no circuit breaker: init runs to the end. Spend is shown in tokens; no prices are known yet, so they count raw and no cost is shown.\n"

func TestFullPlanLineComesFirst(t *testing.T) {
	remoteRepo(t)
	var stderr bytes.Buffer
	a := newRoutedAgent()
	var seen string
	a.first = func() { seen = stderr.String() }
	code := runInitTo(&bytes.Buffer{}, &stderr, fullEnv(t, onePR(), a))
	if code != 0 {
		t.Fatalf("code %d, stderr %s", code, stderr.String())
	}
	if !strings.HasPrefix(seen, noPricesLine) {
		t.Errorf("before the first session, stderr was %q", seen)
	}
	if len(a.started) == 0 || a.started[0] != "discover" || !a.readOnly {
		t.Errorf("sessions %v (discovery read-only: %v); want discovery first, read-only", a.started, a.readOnly)
	}
	after := strings.TrimPrefix(stderr.String(), seen)
	if !strings.Contains(after, "physa init: this run spent ") || regexp.MustCompile(`(?i)estimat`).MatchString(stderr.String()) {
		t.Errorf("after gathering, stderr lacks the spend line or names an estimate:\n%s", after)
	}
	if strings.Contains(stderr.String(), "$") || strings.Contains(stderr.String(), "appetite of") {
		t.Errorf("with no prices a cost was shown, or an appetite named:\n%s", stderr.String())
	}
	if out := gitIn(t, ".", "branch", "--list", "physa/init"); out != "" {
		t.Errorf("without --write a branch was made: %q", out)
	}
}

// readFails is a Stdin that fails the test when it is read.
type readFails struct{ t *testing.T }

func (r readFails) Read([]byte) (int, error) {
	r.t.Error("init read the person's input")
	return 0, io.EOF
}

// TestBareRunAsksNothingAboutCost checks that a bare run on a terminal prints
// no estimate and asks no question: it proposes and recovers without being
// asked, and the next run's first line names what a token is worth.
func TestBareRunAsksNothingAboutCost(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.priced = true
	env := fullEnv(t, onePR(), a)
	env.IsTerminal = func() bool { return true }
	env.Stdin = readFails{t}
	code, out, errOut := runInit(env, "--write")
	if code != 0 || !strings.Contains(out, "## delivery: recovered") || !slices.Contains(a.started, "propose") {
		t.Fatalf("code %d, sessions %v\nstdout %s\nstderr %s", code, a.started, out, errOut)
	}
	if regexp.MustCompile(`(?i)estimat`).MatchString(errOut) || regexp.MustCompile(`(?m)\?$`).MatchString(errOut) {
		t.Errorf("stderr holds an estimate or a question:\n%s", errOut)
	}
	if !strings.HasPrefix(errOut, noPricesLine) {
		t.Errorf("stderr starts %q", errOut)
	}
	// The first run's prices are kept for the next one, whose first line
	// names what a token is worth.
	a2 := newRoutedAgent()
	env.Agent = func(string) (adapter.Adapter, error) { return a2, nil }
	_, _, errOut = runInit(env, "--write")
	if !strings.Contains(errOut, "physa init: no appetite and no circuit breaker: init runs to the end. Spend is shown in tokens weighted by price, each worth an input token of claude-opus-5-5 ($4.00 a million), with about what they cost beside them.\n") {
		t.Errorf("the second run's first line names no unit:\n%s", errOut)
	}
}

// TestFullTableAgreesWithTheRunAt1HourWrites checks that the PR body's
// table shows what the meter counted from the adapter's dollars: with one-turn
// sessions whose input is all 1-hour cache writes, at twice input, its
// weighted total is about twice the raw input and matches the end-of-run line.
func TestFullTableAgreesWithTheRunAt1HourWrites(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.priced, a.writes1h = true, true
	env := fullEnv(t, onePR(), a)
	code, out, errOut := runInit(env, "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	m := regexp.MustCompile(`physa init: this run spent ([0-9.]+[kM]?) tokens \((about \$[0-9.]+)\)\.`).FindStringSubmatch(errOut)
	if m == nil {
		t.Fatalf("no end-of-run line:\n%s", errOut)
	}
	if total := regexp.MustCompile(`\| total \| \d+ \| \d+ \| \d+ \| ([^ ]+) \| ([^|]+) \|`).FindStringSubmatch(out); total == nil || total[1] != m[1] || strings.TrimSpace(total[2]) != m[2] {
		t.Errorf("the table's total %q does not match the run's %q:\n%s", total, m[1:], out)
	}
	for _, phase := range []string{"| discover |", "| propose |", "| recover |"} {
		if !strings.Contains(out, phase) {
			t.Errorf("the table has no %s row:\n%s", phase, out)
		}
	}
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := initrecord.Of(st).Spending(context.Background())
	var w, in int64
	for _, p := range rows {
		w, in = w+p.Spend.Tokens, in+p.Raw.Input
	}
	if err != nil || in == 0 || float64(w)/float64(in) < 1.95 || float64(w)/float64(in) > 2.1 {
		t.Errorf("weighted %d for %d raw input (%v); want about twice", w, in, err)
	}
}

// TestFullWithPricesShowsCost checks the PR body's weighted tokens and cost
// estimate once the agent reports what its sessions cost.
func TestFullWithPricesShowsCost(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.priced = true
	code, out, errOut := runInit(fullEnv(t, onePR(), a), "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	// Proposing: 9,995 uncached in, 5 cached, 20 out at $4/M input: $0.04.
	for _, want := range []string{"| propose | 10000 | 5 | 20 | 10k | about $0.04 |", "| total |", "physa init: this run spent "} {
		if !strings.Contains(out+errOut, want) {
			t.Errorf("output lacks %q:\n%s\n%s", want, out, errOut)
		}
	}
	if !regexp.MustCompile(`physa init: this run spent [0-9.]+k tokens \(about \$0\.[0-9]{2}\)\.`).MatchString(errOut) {
		t.Errorf("stderr:\n%s", errOut)
	}
}

func TestFullJSONIsOneDocument(t *testing.T) {
	remoteRepo(t)
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write", "--json")
	var doc struct {
		Gather struct {
			Version   int
			Discovery struct {
				Started bool
				Tokens  adapter.Tokens
			}
		}
		Proposal, Recovery json.RawMessage
		Coverage           struct{ PRs, Cited int }
		Write              struct {
			Branch string
			Commit struct{ Subject string }
			Pushed bool
			PR     struct{ State string }
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if d := doc.Gather.Discovery; doc.Gather.Version != 3 || !d.Started || d.Tokens.Input != 1_000 {
		t.Errorf("gather %+v", doc.Gather)
	}
	if c := doc.Coverage; c.PRs != 1 || c.Cited > 1 {
		t.Errorf("coverage %+v", c)
	}
	if len(doc.Proposal) == 0 || len(doc.Recovery) == 0 || doc.Write.Branch != "physa/init" ||
		doc.Write.Commit.Subject == "" || doc.Write.Pushed || doc.Write.PR.State != "not asked" {
		t.Errorf("document %s", out)
	}
}

// storedRow returns the stored recovery of capability c.
func storedRow(t *testing.T, env Env, c string) initrecord.Recovered {
	t.Helper()
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := initrecord.Of(st).Recovered(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Capability == c {
			return r
		}
	}
	return initrecord.Recovered{}
}

// newCodebaseRepo is a repo of two changes and a README only, with a local
// bare remote, made the working directory.
func newCodebaseRepo(t *testing.T) string {
	t.Helper()
	dir, _ := remoteRepo(t)
	gitIn(t, dir, "rm", "-q", "--cached", "AGENTS.md")
	if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "README.md", "# Lathe\n")
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "docs: Readme")
	gitIn(t, dir, "push", "-q", "origin", "main")
	return dir
}

// onlyDiscovered fails the test unless a started discovery's session alone.
func onlyDiscovered(t *testing.T, a *routedAgent) {
	t.Helper()
	if !slices.Equal(a.started, []string{"discover"}) {
		t.Errorf("sessions %v, want discovery's alone", a.started)
	}
}

// noAgent fails the test when an agent is resolved.
func noAgent(t *testing.T) func(string) (adapter.Adapter, error) {
	return func(string) (adapter.Adapter, error) {
		t.Error("an agent was resolved")
		return nil, os.ErrInvalid
	}
}

func TestNewCodebaseAnswersWrite(t *testing.T) {
	dir := newCodebaseRepo(t)
	answers := filepath.Join(t.TempDir(), "answers.txt")
	write(t, filepath.Dir(answers), "answers.txt", "# mine\n1: Lathe\n2: Lathe turns sketches into cut lists.\n3: Cut list: The parts to cut, with their sizes\n")
	a := newRoutedAgent()
	env := fullEnv(t, nil, a)
	code, out, errOut := runInit(env, "--write", "--answers", answers)
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	want := []string{".physa/README.md", ".physa/glossary.md", ".physa/product/index.md", "AGENTS.md"}
	if got := changed(t, dir, "physa/init"); !slices.Equal(got, want) {
		t.Errorf("physa/init changes %v, want %v", got, want)
	}
	if p := gitIn(t, dir, "show", "physa/init:.physa/product/index.md"); strings.TrimSpace(p) != "---\npromise: Lathe turns sketches into cut lists.\n---\n\n# Lathe\n\nLathe turns sketches into cut lists." {
		t.Errorf("product/index.md:\n%s", p)
	}
	if g := gitIn(t, dir, "show", "physa/init:.physa/glossary.md"); !strings.Contains(g, "**Cut list**: The parts to cut, with their sizes") {
		t.Errorf("glossary.md:\n%s", g)
	}
	checkTree(t, dir, "physa/init")
	if !strings.Contains(out, "A new codebase") || !strings.Contains(out, "- .physa/product/index.md: Lathe") || !strings.Contains(out, "Wrote physa/init at ") {
		t.Errorf("stdout %s", out)
	}
	onlyDiscovered(t, a)
}

func TestNewCodebaseAsksOnATerminal(t *testing.T) {
	newCodebaseRepo(t)
	a := newRoutedAgent()
	env := fullEnv(t, nil, a)
	env.IsTerminal = func() bool { return true }
	env.Stdin = strings.NewReader("\nLathe turns sketches into cut lists.\n\n")
	code, out, errOut := runInit(env)
	if code != 0 || !strings.Contains(errOut, "1. What is the product called?") ||
		!strings.Contains(out, "Lathe turns sketches into cut lists.") || !strings.Contains(out, "Nothing is written without --write.") {
		t.Errorf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	onlyDiscovered(t, a)
}

func TestNewCodebaseWithoutAnswers(t *testing.T) {
	newCodebaseRepo(t)
	a := newRoutedAgent()
	env := fullEnv(t, nil, a)
	code, _, errOut := runInit(env, "--write")
	if code != 2 || !strings.Contains(errOut, "physa init: this looks like a new codebase (") ||
		!strings.Contains(errOut, "answer its questions on a terminal or with --answers FILE") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
	onlyDiscovered(t, a)
}

func TestFullHooksKeepsOtherHooks(t *testing.T) {
	dir, _ := remoteRepo(t)
	pre := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(pre, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write", "--hooks")
	if code != 0 || !strings.Contains(out, "Hook: installed: ") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if info, err := os.Stat(filepath.Join(dir, ".git", "hooks", "prepare-commit-msg")); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("prepare-commit-msg: %v %v", info, err)
	}
	if info, err := os.Stat(pre); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("pre-commit: %v %v", info, err)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "--get", "core.hooksPath").Output(); err == nil {
		t.Errorf("core.hooksPath was set: %q", out)
	}
	if !strings.Contains(out, "## The Session: hook\n\nInstalled at ") {
		t.Errorf("the body does not say the hook was installed:\n%s", out)
	}
}

func TestFullHooksRefusedKeepsBranch(t *testing.T) {
	dir, _ := remoteRepo(t)
	gitIn(t, dir, "config", "core.hooksPath", "elsewhere")
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write", "--hooks")
	if code != 2 || !strings.Contains(errOut, "physa init: the branch was written, but the hook was not installed: core.hooksPath is already") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if gitIn(t, dir, "branch", "--list", "physa/init") == "" {
		t.Error("the branch was not written")
	}
	if !strings.Contains(out, "Not installed: core.hooksPath is already") {
		t.Errorf("the body does not say the hook was refused:\n%s", out)
	}
}

func TestFullBadCombinationsAreBadUsage(t *testing.T) {
	newRepo(t)
	for _, args := range [][]string{{"--push"}, {"--write", "--propose"}, {"--answers", "a.txt", "--recover"}, {"--hooks"}, {"--yes", "--gather"}, {"--appetite", "300000"}} {
		if code, _, _ := runInit(testEnv(t, nil, nil), args...); code != command.BadUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

// mapLayouts are a product of each layout, by its path: the single file of
// old and the folder's index.md.
var mapLayouts = map[string]string{
	".physa/product.md":       "---\npromise: P.\n---\n\n# P\n",
	".physa/product/index.md": "---\npromise: P.\n---\n\n# P\n\nP.\n",
}

// mappedRepo is remoteRepo with a map's product at p committed to its
// default branch.
func mappedRepo(t *testing.T, p string) {
	t.Helper()
	dir, _ := remoteRepo(t)
	write(t, dir, p, mapLayouts[p])
	gitIn(t, dir, "add", p)
	gitIn(t, dir, "commit", "-q", "-m", "docs(map): Add a map")
	gitIn(t, dir, "push", "-q", "origin", "main")
}

func TestMappedRepoIsRefusedBeforeAnythingIsSpent(t *testing.T) {
	for p := range mapLayouts {
		for _, args := range [][]string{nil, {"--propose"}, {"--recover"}} {
			mappedRepo(t, p)
			a := newRoutedAgent()
			env := fullEnv(t, onePR(), a)
			code, out, errOut := runInit(env, args...)
			t.Logf("%s %v: %s", p, args, strings.TrimSpace(errOut))
			if code != 2 || out != "" || !strings.Contains(errOut, "the repo already has a map ("+p+" on origin/main)") || !strings.Contains(errOut, "physa map learn") {
				t.Errorf("%s %v: code %d, stdout %q, stderr %q", p, args, code, out, errOut)
			}
			if len(a.started) != 0 {
				t.Errorf("%s %v: %d agent sessions ran", p, args, len(a.started))
			}
			st, err := env.Store(context.Background(), ".")
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := st.Gathered(context.Background()); ok {
				t.Errorf("%s %v: a gather was stored", p, args)
			}
			st.Close()
		}
	}
}

func TestMappedRepoStillGathers(t *testing.T) {
	mappedRepo(t, ".physa/product/index.md")
	env := fullEnv(t, onePR(), nil)
	env.Agent = noAgent(t)
	if code, out, errOut := runInit(env, "--gather"); code != 0 || !strings.Contains(out, "Gathered into") ||
		!strings.Contains(out, "Discovery: not started: the repo has a map") ||
		errOut != "physa init: gather starts no agent (discovery not started: the repo has a map)\n" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	code, out, _ := runInit(env, "--gather", "--json", "--agent", "x")
	var sum gather.Summary
	if code != 0 || json.Unmarshal([]byte(out), &sum) != nil {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if d := sum.Discovery; d.Started || d.Why != "discovery not started: the repo has a map" || d.Tokens != (adapter.Tokens{}) || sum.Docs.Count != 1 {
		t.Errorf("summary %+v", sum)
	}
	storedGather(t, env)
}

// trunkRepo is newRepo on a branch named trunk with no remote, so no default
// branch can be found.
func trunkRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	gitIn(t, dir, "branch", "-m", "trunk")
	return dir
}

func TestNoDefaultBranchSkipsTheEarlyCheck(t *testing.T) {
	trunkRepo(t)
	code, _, errOut := runInit(testEnv(t, nil, nil), "--propose")
	if code != 2 || strings.Contains(errOut, "already has a map") || !strings.Contains(errOut, "nothing gathered yet") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestNoDefaultBranchRefusesAMapInTheWorkingTree(t *testing.T) {
	for p, text := range mapLayouts {
		dir := trunkRepo(t)
		write(t, dir, p, text)
		a := newRoutedAgent()
		for _, args := range [][]string{nil, {"--propose"}, {"--recover"}} {
			code, out, errOut := runInit(testEnv(t, nil, a), args...)
			t.Logf("%s %v: %s", p, args, strings.TrimSpace(errOut))
			if code != 2 || out != "" || !strings.Contains(errOut, "("+p+" in the working tree)") || !strings.Contains(errOut, "physa map learn") {
				t.Errorf("%s %v: code %d, stdout %q, stderr %q", p, args, code, out, errOut)
			}
		}
		if len(a.started) != 0 {
			t.Errorf("%s: %d agent sessions ran", p, len(a.started))
		}
	}
}

func TestGatherChoosesTheDiscoveryAgent(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "docs/notes/why.md", "# Why\n")
	a := newRoutedAgent()
	a.discover = "```json\n" + `{"sources":[{"path":"docs/notes/why.md","kind":"overview","reason":"why the product exists"}]}` + "\n```\n"
	a.discovers = adapter.Tokens{Input: 500, Output: 10}
	env := testEnv(t, onePR(), a)
	var names []string
	env.Agent = func(name string) (adapter.Adapter, error) { names = append(names, name); return a, nil }
	code, out, errOut := runInit(env, "--gather", "--agent", "x", "--json")
	var sum gather.Summary
	if code != 0 || json.Unmarshal([]byte(out), &sum) != nil {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if !strings.HasPrefix(errOut, "physa init: gather, with the discovery agent; init has no appetite and no circuit breaker\n") || !slices.Equal(names, []string{"x"}) ||
		!slices.Equal(a.started, []string{"discover"}) || !a.readOnly {
		t.Errorf("stderr %q, agents %v, sessions %v", errOut, names, a.started)
	}
	if d := sum.Discovery; !d.Started || d.Added != 1 || d.Tokens.Input != 500 {
		t.Errorf("discovery %+v", d)
	}
	if g := storedGather(t, env); !slices.ContainsFunc(g.Docs, func(d store.Doc) bool { return d.Path == "docs/notes/why.md" && d.Reason == "why the product exists" }) {
		t.Errorf("stored docs %+v", g.Docs)
	}
}

// mergeMarker is a line of the merge skill, which only the merge's prompt
// holds.
const mergeMarker = "You merge what several sessions proposed"

// windowedAgent is a routedAgent that states its window, and records which
// propose sessions it started read a share or merged, with their ids: those
// that are not discovery's, a recovering session's (whose prompt names its
// capability) or resumed.
type windowedAgent struct {
	*routedAgent
	window  int
	propose []string // "share" or "merge", in order
	ids     []string
}

// Window is the window the test sets while propose reads its shares, and a
// whole one once the merge has run: recovering's skill is larger than the
// room a share leaves, and only propose is meant to be cramped here.
func (w *windowedAgent) Window() int {
	if slices.Contains(w.propose, "merge") {
		return 1_000_000
	}
	return w.window
}

func (w *windowedAgent) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	if !s.Resume && !strings.Contains(s.Prompt, discoveryMarker) && !strings.Contains(s.Prompt, "\nname: delivery\n") && !strings.Contains(s.Prompt, "\nname: search\n") {
		kind := "share"
		if strings.Contains(s.Prompt, mergeMarker) {
			kind = "merge"
		}
		w.propose, w.ids = append(w.propose, kind), append(w.ids, s.SessionID)
	}
	return w.routedAgent.Start(ctx, s)
}

// shareWindow leaves one share of propose room for the skill and 6,000
// estimated tokens.
var shareWindow = reading.Tokens(mapping.Prompt(reading.Input{})) + 6_000 + mapping.ReplyReserve

// bigPRs is a forge that lists PRs 3, 4 and 5 as merged, each with a body of
// about tokens estimated tokens.
func bigPRs(tokens int) forge.Forge {
	var prs []forge.PR
	for n := 3; n <= 5; n++ {
		body := strings.Repeat(fmt.Sprintf("PR %d says why. ", n), tokens*23/10/len("PR 3 says why. "))
		prs = append(prs, forge.PR{Number: n, Title: fmt.Sprintf("Change %d", n), Body: body, MergedAt: time.Now().Add(-time.Duration(n) * time.Hour)})
	}
	return mergedFake{prs: prs}
}

// TestFullProposesInSharesWhenOneSessionCannotHoldTheSources checks that a
// bare run whose sources are more than one session of the agent holds
// estimates every share and the merge, proposes in shares and a merge
// before recovering, and writes a Session: trailer for each of them.
func TestFullProposesInSharesWhenOneSessionCannotHoldTheSources(t *testing.T) {
	remoteRepo(t)
	a := &windowedAgent{routedAgent: newRoutedAgent(), window: shareWindow}
	code, out, errOut := runInit(fullEnv(t, bigPRs(4_000), a), "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if want := []string{"share", "share", "share", "merge"}; !slices.Equal(a.propose, want) {
		t.Fatalf("propose sessions %v, want %v", a.propose, want)
	}
	if got := a.started[:5]; !slices.Equal(got, []string{"discover", "propose", "propose", "propose", "propose"}) || len(a.started) != 7 {
		t.Errorf("sessions %v: want discovery, the shares and the merge, then recovering", a.started)
	}
	for _, want := range []string{"physa init: propose reads share 1 of 3 whole, ", "physa init: propose reads share 3 of 3 whole, ", "physa init: propose merges the proposals of its 3 shares"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "propose reads its sources whole") || regexp.MustCompile(`(?i)estimat`).MatchString(errOut) {
		t.Errorf("stderr names one session or an estimate:\n%s", errOut)
	}
	for _, want := range []string{"in 3 shares and a merge", "Share 1 of 3, about ", "The merge read only the shares' proposals", "## delivery: recovered", "Wrote physa/init at "} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	msg := gitIn(t, ".", "log", "-1", "--format=%B", "physa/init")
	if n := strings.Count(msg, "Session: "); n != 6 {
		t.Errorf("%d Session: trailers, want the 3 shares', the merge's and 2 recoveries':\n%s", n, msg)
	}
	for _, id := range a.ids {
		if !strings.Contains(msg, "Session: "+id+"\n") {
			t.Errorf("no Session: trailer for propose's session %s:\n%s", id, msg)
		}
	}
}

// TestFullOneSessionWithinTheWindowIsUnchanged checks that an agent whose
// window holds the whole prompt proposes in one session, as before.
func TestFullOneSessionWithinTheWindowIsUnchanged(t *testing.T) {
	remoteRepo(t)
	a := &windowedAgent{routedAgent: newRoutedAgent(), window: 1_000_000}
	code, out, errOut := runInit(fullEnv(t, bigPRs(4_000), a))
	if code != 0 || !slices.Equal(a.propose, []string{"share"}) || !strings.Contains(errOut, "propose reads its sources whole") ||
		strings.Contains(errOut, "shares") || strings.Contains(out, "Share 1 of") {
		t.Errorf("code %d, propose sessions %v\nstdout %s\nstderr %s", code, a.propose, out, errOut)
	}
}

// TestFullStopsOnASourceLargerThanAShare checks that a PR larger than an
// empty share stops a bare run with exit 1 before any propose session, and
// --propose alone the same way, naming the PR and how to leave it out.
func TestFullStopsOnASourceLargerThanAShare(t *testing.T) {
	remoteRepo(t)
	a := &windowedAgent{routedAgent: newRoutedAgent(), window: shareWindow}
	env := fullEnv(t, bigPRs(10_000), a)
	stop := regexp.MustCompile(`physa init: PR #3 takes about [0-9,]+ estimated tokens, and with the skill it is more than one share of propose holds \(about [0-9,]+ tokens\)\. ` +
		`Nothing is cut, so init stops before spending on propose\. To leave it out of the gather, gather again with --since a date after it merged on [0-9-]+ \(physa init --gather --since [0-9-]+\), or with a smaller --max-prs\.\n$`)
	code, out, errOut := runInit(env)
	t.Logf("bare run: exit %d\nstderr:\n%s", code, errOut)
	if code != 1 || !slices.Equal(a.started, []string{"discover"}) || len(a.propose) != 0 || !stop.MatchString(errOut) || strings.Contains(out, "# Proposed map") {
		t.Errorf("bare run: code %d, sessions %v\nstdout %s\nstderr %s", code, a.started, out, errOut)
	}
	code, out, errOut = runInit(env, "--propose")
	t.Logf("--propose: exit %d\nstderr:\n%s", code, errOut)
	if code != 1 || !slices.Equal(a.started, []string{"discover"}) || len(a.propose) != 0 || !stop.MatchString(errOut) || out != "" {
		t.Errorf("--propose: code %d, sessions %v\nstdout %s\nstderr %s", code, a.started, out, errOut)
	}
}

// skillLine is a line Env.Skill gives for an agent with no current Physa skill.
const skillLine = "Claude Code has no Physa skill on this account: run `physa skill setup` to install it, so its sessions learn Physa's commands and terms."

// skillSpy is an Env.Skill that gives line and counts the agents it was asked about.
type skillSpy struct {
	line   string
	agents []string
}

func (s *skillSpy) skill(agent string) string {
	s.agents = append(s.agents, agent)
	return s.line
}

// lastLine is the last line of out, without its newline.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestFullSkillLineIsSaidOnceLastAndInThePRBody(t *testing.T) {
	remoteRepo(t)
	f := &prForge{mergedFake: mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}}
	env := fullEnv(t, f, newRoutedAgent())
	spy := &skillSpy{line: skillLine}
	env.Skill = spy.skill
	code, out, errOut := runInit(env, "--write", "--push", "--pr", "--agent", "fake")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if !slices.Equal(spy.agents, []string{"fake"}) {
		t.Errorf("Skill asked about %v, want [fake] once", spy.agents)
	}
	if n := strings.Count(errOut, "physa skill setup"); n != 1 {
		t.Errorf("stderr names physa skill setup %d times, want 1:\n%s", n, errOut)
	}
	if got := lastLine(errOut); got != "physa init: "+skillLine {
		t.Errorf("stderr's last line %q, want the skill's line", got)
	}
	section := "\n## Physa's skill\n\n" + skillLine + "\n"
	if len(f.bodies) != 1 || !strings.HasSuffix(f.bodies[0], section) {
		t.Errorf("the PR body does not end with the skill's section:\n%v", f.bodies)
	}
	if !strings.Contains(out, section) {
		t.Errorf("the body on stdout lacks the skill's section:\n%s", out)
	}
}

func TestFullSkillLineIsInTheJSONBody(t *testing.T) {
	remoteRepo(t)
	env := fullEnv(t, onePR(), newRoutedAgent())
	env.Skill = (&skillSpy{line: skillLine}).skill
	code, out, errOut := runInit(env, "--write", "--json")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	var doc struct {
		Write struct{ Body string } `json:"write"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, out)
	}
	if !strings.HasSuffix(doc.Write.Body, "\n## Physa's skill\n\n"+skillLine+"\n") {
		t.Errorf("--json's body lacks the skill's section:\n%s", doc.Write.Body)
	}
	if got := lastLine(errOut); got != "physa init: "+skillLine {
		t.Errorf("stderr's last line %q, want the skill's line", got)
	}
}

func TestFullSkillCurrentOrNilSaysNothing(t *testing.T) {
	for name, skill := range map[string]func(string) string{"empty": (&skillSpy{}).skill, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			remoteRepo(t)
			f := &prForge{mergedFake: mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}}
			env := fullEnv(t, f, newRoutedAgent())
			env.Skill = skill
			code, out, errOut := runInit(env, "--write", "--push", "--pr")
			if code != 0 || len(f.bodies) != 1 {
				t.Fatalf("code %d, bodies %d\nstdout %s\nstderr %s", code, len(f.bodies), out, errOut)
			}
			for where, s := range map[string]string{"stdout": out, "stderr": errOut, "PR body": f.bodies[0]} {
				if strings.Contains(s, "Physa's skill") || strings.Contains(s, "physa skill") {
					t.Errorf("%s names the skill:\n%s", where, s)
				}
			}
		})
	}
}

func TestNewCodebaseSkillLineIsInTheBody(t *testing.T) {
	newCodebaseRepo(t)
	answers := filepath.Join(t.TempDir(), "answers.txt")
	write(t, filepath.Dir(answers), "answers.txt", "1: Lathe\n2: Lathe turns sketches into cut lists.\n3: Cut list: The parts to cut\n")
	env := fullEnv(t, nil, newRoutedAgent())
	spy := &skillSpy{line: skillLine}
	env.Skill = spy.skill
	code, out, errOut := runInit(env, "--write", "--answers", answers)
	if code != 0 || len(spy.agents) != 1 {
		t.Fatalf("code %d, asked %v\nstdout %s\nstderr %s", code, spy.agents, out, errOut)
	}
	if !strings.HasSuffix(out, "\n## Physa's skill\n\n"+skillLine+"\n") {
		t.Errorf("the body on stdout does not end with the skill's section:\n%s", out)
	}
	if strings.Count(errOut, "physa skill setup") != 1 || lastLine(errOut) != "physa init: "+skillLine {
		t.Errorf("stderr:\n%s", errOut)
	}
}

func TestFullSkillLineKeepsTheExitCode(t *testing.T) {
	cases := map[string]struct {
		repo  func(t *testing.T)
		forge forge.Forge
		agent func() adapter.Adapter
		args  []string
	}{
		"stopped before proposing": {func(t *testing.T) { remoteRepo(t) }, bigPRs(10_000),
			func() adapter.Adapter { return &windowedAgent{routedAgent: newRoutedAgent(), window: shareWindow} }, []string{"--write"}},
		"a new codebase unanswered": {func(t *testing.T) { newCodebaseRepo(t) }, nil, func() adapter.Adapter { return newRoutedAgent() }, []string{"--write"}},
		"a hook refused": {func(t *testing.T) {
			dir, _ := remoteRepo(t)
			gitIn(t, dir, "config", "core.hooksPath", "elsewhere")
		}, onePR(), func() adapter.Adapter { return newRoutedAgent() }, []string{"--write", "--hooks"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			codes := map[string]int{}
			for _, line := range []string{"", skillLine} {
				c.repo(t)
				env := fullEnv(t, c.forge, c.agent())
				env.Skill = (&skillSpy{line: line}).skill
				code, _, errOut := runInit(env, c.args...)
				codes[line] = code
				if said := strings.Contains(errOut, "physa skill setup"); said != (line != "") {
					t.Errorf("line %q: stderr names the skill %v:\n%s", line, said, errOut)
				}
			}
			if codes[""] != codes[skillLine] || codes[""] == 0 {
				t.Errorf("exit codes %v, want the same non-zero code with and without the line", codes)
			}
		})
	}
}

func TestSkillIsNotAskedOutsideAFullRun(t *testing.T) {
	never := func(string) string { t.Error("Skill was asked"); return skillLine }
	newRepo(t)
	env := testEnv(t, onePR(), nil)
	env.Skill = never
	if code, _, errOut := runInit(env, "--gather"); code != 0 || strings.Contains(errOut, "physa skill") {
		t.Errorf("--gather: code %d, stderr %s", code, errOut)
	}
	if code, _, errOut := runInit(env, "--push"); code != command.BadUsage || strings.Contains(errOut, "physa skill") {
		t.Errorf("a bad line: code %d, stderr %s", code, errOut)
	}
	for _, args := range [][]string{nil, {"--propose"}, {"--recover"}} {
		mappedRepo(t, ".physa/product/index.md")
		env := fullEnv(t, onePR(), newRoutedAgent())
		env.Skill = never
		if code, _, errOut := runInit(env, args...); code != 2 || strings.Contains(errOut, "physa skill") {
			t.Errorf("a mapped repo %v: code %d, stderr %s", args, code, errOut)
		}
	}
}
