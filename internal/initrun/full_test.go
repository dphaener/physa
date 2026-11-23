package initrun

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/mapcheck"
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

func (f *prForge) OpenPR(string) (int, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return 7, "https://example.com/pr/7", f.open, nil
}

func (f *prForge) CreatePR(head, base, title, body string) (int, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open = true
	f.created = append(f.created, head+"→"+base+": "+title)
	f.bodies = append(f.bodies, body)
	return 7, "https://example.com/pr/7", nil
}

func (f *prForge) EditPR(int, string) error {
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
	readOnly  bool           // every discovery session was read-only
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
	case a.propose != 0:
		t.Input = a.propose
	}
	return fakeSession{f: &fakeAgent{reports: []adapter.Tokens{t}, result: adapter.Result{Text: text, Tokens: t, SessionID: s.SessionID}}, usage: s.Usage}, nil
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
	env.Writer = git.OpenWriter
	env.Hooks = git.OpenHooks
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
	res, err := mapcheck.CheckWith(clone, mapcheck.Options{OpenRepo: func(d string) (vcs.Repo, error) { return git.Open(d) }})
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
		"delivery: rule \"Changes ship fast (ship-fast)\"", "| propose | 10000 | 5 | 20 |", "| recover |", "| total |",
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

func TestFullPlanLineComesFirst(t *testing.T) {
	remoteRepo(t)
	var stderr bytes.Buffer
	a := newRoutedAgent()
	var seen string
	a.first = func() { seen = stderr.String() }
	code := Run([]string{"--appetite", "100000"}, &bytes.Buffer{}, &stderr, fullEnv(t, onePR(), a))
	if code != 0 {
		t.Fatalf("code %d, stderr %s", code, stderr.String())
	}
	want := "physa init: appetite 100000 tokens: gather up to 25000 (the discovery agent); propose up to 50000; recover the rest, about 25000, shared by its sessions; check and write 0.\n"
	if !strings.HasPrefix(seen, want) {
		t.Errorf("before the first session, stderr was %q", seen)
	}
	if len(a.started) == 0 || a.started[0] != "discover" || !a.readOnly {
		t.Errorf("sessions %v (discovery read-only: %v); want discovery first, read-only", a.started, a.readOnly)
	}
	if after := strings.TrimPrefix(stderr.String(), seen); !strings.Contains(after, "physa init: propose's prompt is about ") ||
		!strings.Contains(after, " tokens plus 16000 for the reply.") {
		t.Errorf("after gathering, stderr was %q", after)
	}
	if out := gitIn(t, ".", "branch", "--list", "physa/init"); out != "" {
		t.Errorf("without --write a branch was made: %q", out)
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
				Cap     int64
			}
		}
		Proposal, Recovery json.RawMessage
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
	if d := doc.Gather.Discovery; doc.Gather.Version != 2 || !d.Started || d.Tokens.Input != 1_000 || d.Cap != 75_000 {
		t.Errorf("gather %+v", doc.Gather)
	}
	if len(doc.Proposal) == 0 || len(doc.Recovery) == 0 || doc.Write.Branch != "physa/init" ||
		doc.Write.Commit.Subject == "" || doc.Write.Pushed || doc.Write.PR.State != "not asked" {
		t.Errorf("document %s", out)
	}
}

// storedRow returns the stored recovery of capability c.
func storedRow(t *testing.T, env Env, c string) store.Recovered {
	t.Helper()
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.Recovered()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Capability == c {
			return r
		}
	}
	return store.Recovered{}
}

func TestFullSmallAppetiteThenRecoverWrite(t *testing.T) {
	dir, _ := remoteRepo(t)
	a := newRoutedAgent()
	a.propose = 25_600
	env := fullEnv(t, onePR(), a)
	// Discovery uses 1,010 and proposing 25,620 of 52,410; recovering's
	// 25,780 holds one capability's reserve (its prompt and 12,000 for the
	// reply) and no more.
	code, out, errOut := runInit(env, "--write", "--appetite", "52410")
	if code != 1 || !strings.Contains(out, "Not yet recovered") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	var first, missing string
	for _, c := range []string{"delivery", "search"} {
		if storedRow(t, env, c).File != "" {
			first = c
		} else {
			missing = c
		}
	}
	if first == "" || missing == "" {
		t.Fatalf("want one recovered and one not; got first %q, missing %q", first, missing)
	}
	if files := strings.TrimSpace(gitIn(t, dir, "ls-tree", "--name-only", "physa/init:.physa/capabilities/"+missing)); files != "index.md" {
		t.Errorf("%s's folder holds %q, want index.md alone", missing, files)
	}
	file := gitIn(t, dir, "show", "physa/init:.physa/capabilities/"+missing+"/index.md")
	if strings.Contains(file, "rules.md") || !strings.Contains(file, "promise: ") {
		t.Errorf("%s is not promise-only:\n%s", missing, file)
	}
	earlier := storedRow(t, env, first).File
	code, out, errOut = runInit(env, "--recover", "--only", missing, "--write")
	if code != 0 {
		t.Fatalf("recover --write: code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if got := storedRow(t, env, first).File; got != earlier {
		t.Errorf("the earlier file of %s was not kept:\n%s", first, got)
	}
	for _, c := range []string{first, missing} {
		if f := gitIn(t, dir, "show", "physa/init:.physa/capabilities/"+c+"/rules.md"); !strings.Contains(f, "\n### ") {
			t.Errorf("rewritten %s holds no rules:\n%s", c, f)
		}
	}
	if !strings.Contains(out, "Recovered 1, not yet recovered 0") || !strings.Contains(out, "Wrote physa/init at ") {
		t.Errorf("stdout %s", out)
	}
	if n := strings.Count(gitIn(t, dir, "log", "--format=%H", "main..physa/init"), "\n"); n != 1 {
		t.Errorf("physa/init holds %d changes past main, want one", n)
	}
}

func TestFullBreakerWritesNothing(t *testing.T) {
	remoteRepo(t)
	f := &fakeAgent{reports: []adapter.Tokens{{Input: 900_000}}, result: adapter.Result{Text: twoReply}}
	code, _, errOut := runInit(fullEnv(t, onePR(), f), "--write")
	if code != 1 || !strings.Contains(errOut, "the circuit breaker stopped proposing") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
	if out := gitIn(t, ".", "branch", "--list", "physa/init"); out != "" {
		t.Errorf("a branch was made: %q", out)
	}
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
	for _, args := range [][]string{{"--push"}, {"--write", "--propose"}, {"--answers", "a.txt", "--recover"}, {"--hooks"}} {
		if code, _, _ := runInit(testEnv(t, nil, nil), args...); code != cli.BadUsage {
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
			st, err := env.Store(".")
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := st.Gathered(); ok {
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
	code, out, _ := runInit(env, "--gather", "--json", "--agent", "x", "--appetite", "1000")
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

// fullDoc is the parts of a full run's --json document the spend tests read.
type fullDoc struct {
	Gather struct {
		Docs struct {
			Files []struct{ Path, Kind, Reason string }
		}
		Discovery struct {
			Started bool
			Why     string
			Added   int
			Tokens  adapter.Tokens
			Cap     int64
		}
	}
	Recovery struct {
		Appetite int64
		Used     adapter.Tokens
	}
}

func TestFullDiscoverySpendComesOffRecovery(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.discover = "Found one.\n```json\n" + `{"sources":[{"path":"scratch.txt","kind":"overview","reason":"the person's notes"}]}` + "\n```\n"
	a.discovers = adapter.Tokens{Input: 2_000, Output: 30}
	code, out, errOut := runInit(fullEnv(t, onePR(), a), "--json", "--appetite", "200000")
	var doc fullDoc
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	d := doc.Gather.Discovery
	if !d.Started || d.Added != 1 || d.Tokens.Input != 2_000 || d.Tokens.Output != 30 || d.Cap != 50_000 {
		t.Errorf("discovery %+v", d)
	}
	if !slices.ContainsFunc(doc.Gather.Docs.Files, func(f struct{ Path, Kind, Reason string }) bool {
		return f.Path == "scratch.txt" && f.Kind == "overview" && f.Reason == "the person's notes"
	}) {
		t.Errorf("gathered docs %+v", doc.Gather.Docs.Files)
	}
	// Proposing uses 10,020; recovering gets what discovery and proposing left.
	if want := int64(200_000 - 2_030 - 10_020); doc.Recovery.Appetite != want {
		t.Errorf("recovery's appetite %d, want %d", doc.Recovery.Appetite, want)
	}
}

func TestFullStoppedDiscoveryStillStopsAtTheAppetite(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.discovers = adapter.Tokens{Input: 27_000, Output: 20} // over its cap of 25,000
	code, out, errOut := runInit(fullEnv(t, onePR(), a), "--json", "--appetite", "100000")
	var doc fullDoc
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	d := doc.Gather.Discovery
	if !d.Started || d.Added != 0 || d.Why != "stopped at the token cap" || d.Cap != 25_000 {
		t.Errorf("discovery %+v", d)
	}
	discovered := d.Tokens.Input + d.Tokens.Output
	if want := 100_000 - discovered - 10_020; doc.Recovery.Appetite != want {
		t.Errorf("recovery's appetite %d, want %d", doc.Recovery.Appetite, want)
	}
	if total := discovered + 10_020 + doc.Recovery.Used.Input + doc.Recovery.Used.Output; total > 100_000 {
		t.Errorf("init spent %d of an appetite of 100000", total)
	}
}

func TestGatherCapsAndChoosesTheDiscoveryAgent(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "docs/notes/why.md", "# Why\n")
	a := newRoutedAgent()
	a.discover = "```json\n" + `{"sources":[{"path":"docs/notes/why.md","kind":"overview","reason":"why the product exists"}]}` + "\n```\n"
	a.discovers = adapter.Tokens{Input: 500, Output: 10}
	env := testEnv(t, onePR(), a)
	var names []string
	env.Agent = func(name string) (adapter.Adapter, error) { names = append(names, name); return a, nil }
	code, out, errOut := runInit(env, "--gather", "--agent", "x", "--appetite", "25000", "--json")
	var sum gather.Summary
	if code != 0 || json.Unmarshal([]byte(out), &sum) != nil {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if !strings.HasPrefix(errOut, "physa init: gather up to 25000 tokens (the discovery agent)\n") || !slices.Equal(names, []string{"x"}) ||
		!slices.Equal(a.started, []string{"discover"}) || !a.readOnly {
		t.Errorf("stderr %q, agents %v, sessions %v", errOut, names, a.started)
	}
	if d := sum.Discovery; !d.Started || d.Added != 1 || d.Cap != 25000 || d.Tokens.Input != 500 {
		t.Errorf("discovery %+v", d)
	}
	if g := storedGather(t, env); !slices.ContainsFunc(g.Docs, func(d store.Doc) bool { return d.Path == "docs/notes/why.md" && d.Reason == "why the product exists" }) {
		t.Errorf("stored docs %+v", g.Docs)
	}
}

func TestGatherCapBelowTheFloorStartsNoAgent(t *testing.T) {
	newRepo(t)
	env := testEnv(t, onePR(), nil)
	env.Agent = noAgent(t)
	code, out, errOut := runInit(env, "--gather", "--json", "--appetite", "1000")
	var sum gather.Summary
	if code != 0 || json.Unmarshal([]byte(out), &sum) != nil {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if d := sum.Discovery; d.Started || d.Why != "the cap of 1000 tokens is too small to start discovery" {
		t.Errorf("discovery %+v", d)
	}
}
