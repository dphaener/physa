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

// routedAgent answers proposing with twoReply and each capability's
// recovering with its reply, joining a resumed session to its capability.
// It records each capability whose session it started, and calls first,
// when set, before its first start.
type routedAgent struct {
	mu       sync.Mutex
	replies  map[string]string
	sessions map[string]string // session id → capability
	started  []string          // "propose" or the capability
	first    func()
	propose  int64 // the input tokens proposing uses; zero means 10,000
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
		a.sessions[s.SessionID] = name
		a.started = append(a.started, name)
	}
	text := twoReply
	if name != "propose" {
		text = a.replies[name]
	}
	t := adapter.Tokens{Input: 10_000, CachedInput: 5, Output: 20}
	if name == "propose" && a.propose != 0 {
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
	want := []string{".physa/README.md", ".physa/capabilities/delivery.md", ".physa/capabilities/search.md", ".physa/glossary.md", ".physa/product.md", "AGENTS.md"}
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
	if !strings.Contains(seen, "physa init: appetite 100000 tokens: gather 0 (no agent); propose up to 50000 (its prompt about ") ||
		!strings.Contains(seen, "; recover the rest, about ") || !strings.Contains(seen, "check and write 0.") {
		t.Errorf("before the first session, stderr was %q", seen)
	}
	if out := gitIn(t, ".", "branch", "--list", "physa/init"); out != "" {
		t.Errorf("without --write a branch was made: %q", out)
	}
}

func TestFullJSONIsOneDocument(t *testing.T) {
	remoteRepo(t)
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write", "--json")
	var doc struct {
		Gather, Proposal, Recovery json.RawMessage
		Write                      struct {
			Branch string
			Commit struct{ Subject string }
			Pushed bool
			PR     struct{ State string }
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if len(doc.Gather) == 0 || len(doc.Proposal) == 0 || len(doc.Recovery) == 0 || doc.Write.Branch != "physa/init" ||
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
	a.propose = 24_000
	env := fullEnv(t, onePR(), a)
	// Proposing uses 24,020 of 50,100; recovering's 26,080 holds one
	// capability's reserve (its prompt and 12,000 for the reply) and no more.
	code, out, errOut := runInit(env, "--write", "--appetite", "50100")
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
	file := gitIn(t, dir, "show", "physa/init:.physa/capabilities/"+missing+".md")
	if strings.Contains(file, "## Rules") || !strings.Contains(file, "promise: ") {
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
		if f := gitIn(t, dir, "show", "physa/init:.physa/capabilities/"+c+".md"); !strings.Contains(f, "## Rules") {
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
	env := fullEnv(t, nil, nil)
	env.Agent = noAgent(t)
	code, out, errOut := runInit(env, "--write", "--answers", answers)
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	want := []string{".physa/README.md", ".physa/glossary.md", ".physa/product.md", "AGENTS.md"}
	if got := changed(t, dir, "physa/init"); !slices.Equal(got, want) {
		t.Errorf("physa/init changes %v, want %v", got, want)
	}
	if p := gitIn(t, dir, "show", "physa/init:.physa/product.md"); !strings.Contains(p, "promise: Lathe turns sketches into cut lists.") || !strings.Contains(p, "# Lathe") {
		t.Errorf("product.md:\n%s", p)
	}
	if g := gitIn(t, dir, "show", "physa/init:.physa/glossary.md"); !strings.Contains(g, "**Cut list**: The parts to cut, with their sizes") {
		t.Errorf("glossary.md:\n%s", g)
	}
	checkTree(t, dir, "physa/init")
	if !strings.Contains(out, "A new codebase") || !strings.Contains(out, "Wrote physa/init at ") {
		t.Errorf("stdout %s", out)
	}
}

func TestNewCodebaseAsksOnATerminal(t *testing.T) {
	newCodebaseRepo(t)
	env := fullEnv(t, nil, nil)
	env.Agent = noAgent(t)
	env.IsTerminal = func() bool { return true }
	env.Stdin = strings.NewReader("\nLathe turns sketches into cut lists.\n\n")
	code, out, errOut := runInit(env)
	if code != 0 || !strings.Contains(errOut, "1. What is the product called?") ||
		!strings.Contains(out, "Lathe turns sketches into cut lists.") || !strings.Contains(out, "Nothing is written without --write.") {
		t.Errorf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
}

func TestNewCodebaseWithoutAnswers(t *testing.T) {
	newCodebaseRepo(t)
	env := fullEnv(t, nil, nil)
	env.Agent = noAgent(t)
	code, _, errOut := runInit(env, "--write")
	if code != 2 || !strings.Contains(errOut, "physa init: this looks like a new codebase (") ||
		!strings.Contains(errOut, "answer its questions on a terminal or with --answers FILE") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
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

// mappedRepo is remoteRepo with a map committed to its default branch.
func mappedRepo(t *testing.T) {
	t.Helper()
	dir, _ := remoteRepo(t)
	write(t, dir, ".physa/product.md", "---\npromise: P.\n---\n\n# P\n")
	gitIn(t, dir, "add", ".physa/product.md")
	gitIn(t, dir, "commit", "-q", "-m", "docs(map): Add a map")
	gitIn(t, dir, "push", "-q", "origin", "main")
}

func TestMappedRepoIsRefusedBeforeAnythingIsSpent(t *testing.T) {
	for _, args := range [][]string{nil, {"--propose"}, {"--recover"}} {
		mappedRepo(t)
		a := newRoutedAgent()
		env := fullEnv(t, onePR(), a)
		code, out, errOut := runInit(env, args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "the repo already has a map") || !strings.Contains(errOut, "physa map learn") {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errOut)
		}
		if len(a.started) != 0 {
			t.Errorf("%v: %d agent sessions ran", args, len(a.started))
		}
		st, err := env.Store(".")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := st.Gathered(); ok {
			t.Errorf("%v: a gather was stored", args)
		}
		st.Close()
	}
}

func TestMappedRepoStillGathers(t *testing.T) {
	mappedRepo(t)
	env := fullEnv(t, onePR(), newRoutedAgent())
	if code, out, errOut := runInit(env, "--gather"); code != 0 || !strings.Contains(out, "Gathered into") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
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
	dir := trunkRepo(t)
	write(t, dir, ".physa/product.md", "---\npromise: P.\n---\n\n# P\n")
	a := newRoutedAgent()
	for _, args := range [][]string{nil, {"--propose"}, {"--recover"}} {
		code, out, errOut := runInit(testEnv(t, nil, a), args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "(.physa/product.md in the working tree)") || !strings.Contains(errOut, "physa map learn") {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	if len(a.started) != 0 {
		t.Errorf("%d agent sessions ran", len(a.started))
	}
}
