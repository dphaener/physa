package initbranch

import (
	"context"
	"errors"
	"fmt"
	"github.com/dphaener/physa/internal/spend"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/agentspointer"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// gitEnv is the environment of every git a test runs, its own and the one
// the writer opens: the test process's, with an identity, no signing and
// main as the first branch, given as command-line config, so no commit
// reads the machine's config.
var gitEnv = append(os.Environ(),
	"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=4",
	"GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=W",
	"GIT_CONFIG_KEY_1=user.email", "GIT_CONFIG_VALUE_1=w@example.com",
	"GIT_CONFIG_KEY_2=commit.gpgsign", "GIT_CONFIG_VALUE_2=false",
	"GIT_CONFIG_KEY_3=init.defaultBranch", "GIT_CONFIG_VALUE_3=main")

// gitCmd is git with args, run in dir with gitEnv.
func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = dir, gitEnv
	return cmd
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmd(dir, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ref returns what ref names in dir, or "" when it names nothing.
func ref(dir, name string) string {
	out, err := gitCmd(dir, "rev-parse", "--verify", "--quiet", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fixture is the person's clone dir of the bare remote bare, with no map
// and, when agents is not nil, an AGENTS.md holding it. The person has an
// uncommitted edit and an untracked file, so a write that touched their
// working copy or index would show.
type fixture struct {
	t         *testing.T
	dir, bare string
	first     string // the first change
	forge     *fakeForge
}

func newFixture(t *testing.T, agents *string) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir(), bare: t.TempDir(), forge: &fakeForge{}}
	gitIn(t, f.bare, "init", "-q", "--bare", "-b", "main")
	gitIn(t, f.dir, "init", "-q", "-b", "main")
	writeFile(t, f.dir, "docs/vision.md", "vision\n")
	writeFile(t, f.dir, "main.go", "package main\n")
	if agents != nil {
		writeFile(t, f.dir, "AGENTS.md", *agents)
	}
	gitIn(t, f.dir, "add", ".")
	gitIn(t, f.dir, "commit", "-q", "-m", "feat: Start")
	f.first = gitIn(t, f.dir, "rev-parse", "HEAD")
	gitIn(t, f.dir, "remote", "add", "origin", f.bare)
	gitIn(t, f.dir, "push", "-q", "origin", "main")
	gitIn(t, f.dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	writeFile(t, f.dir, "main.go", "package main // edited\n")
	gitIn(t, f.dir, "add", "main.go")
	writeFile(t, f.dir, "scratch.txt", "mine\n")
	return f
}

func (f *fixture) writer() vcs.Writer {
	f.t.Helper()
	w, err := git.OpenWriter(context.Background(), f.dir, gitEnv)
	if err != nil {
		f.t.Fatal(err)
	}
	return w
}

func (f *fixture) state() string {
	return gitIn(f.t, f.dir, "status", "--porcelain=v2", "--branch", "--untracked-files=all") + "|" +
		gitIn(f.t, f.dir, "rev-parse", "HEAD") + "|" + gitIn(f.t, f.dir, "diff", "--cached")
}

// run runs w, by default the person's own Writer, and checks that their
// working copy, index and HEAD are as they were and no working copy is left.
func (f *fixture) run(m Map, o Options, w vcs.Writer) (Outcome, error) {
	f.t.Helper()
	if w == nil {
		w = f.writer()
	}
	before := f.state()
	out, err := Run(context.Background(), w, m, o)
	if after := f.state(); after != before {
		f.t.Errorf("the working copy changed:\nbefore %s\nafter  %s", before, after)
	}
	if list := gitIn(f.t, f.dir, "worktree", "list"); strings.Count(list, "\n") != 0 {
		f.t.Errorf("working copies left:\n%s", list)
	}
	return out, err
}

func (f *fixture) opts(push, pr bool) Options {
	return Options{Push: push, PR: pr, PRs: f.forge}
}

// fakeForge holds at most one PR, recording every call that reads or
// writes it.
type fakeForge struct {
	open   bool
	number int
	body   string
	calls  []string
}

func (f *fakeForge) OpenPR(_ context.Context, head string) (int, string, bool, error) {
	f.calls = append(f.calls, "list "+head)
	return f.number, f.url(), f.open, nil
}
func (f *fakeForge) CreatePR(_ context.Context, head, base, title, body string) (int, string, error) {
	f.calls = append(f.calls, "create "+head+" "+base+" "+title)
	f.open, f.body = true, body
	f.number++
	return f.number, f.url(), nil
}
func (f *fakeForge) EditPR(_ context.Context, n int, body string) error {
	f.calls = append(f.calls, fmt.Sprintf("edit %d", n))
	f.body = body
	return nil
}
func (f *fakeForge) url() string { return fmt.Sprintf("https://example.com/pr/%d", f.number) }

var _ forge.PRs = (*fakeForge)(nil)

// unavailable is a forge that cannot answer.
type unavailable struct{}

func (unavailable) OpenPR(context.Context, string) (int, string, bool, error) {
	return 0, "", false, &forge.Error{Class: forge.ErrUnavailable, Reason: "gh is not installed"}
}
func (unavailable) CreatePR(context.Context, string, string, string, string) (int, string, error) {
	return 0, "", errors.New("not called")
}
func (unavailable) EditPR(context.Context, int, string) error { return errors.New("not called") }

// racingWriter stands for someone else pushing the remote's physa/init just
// before this run pushes.
type racingWriter struct {
	vcs.Writer
	race func()
}

func (r *racingWriter) Push(ctx context.Context, branch, id, expect string) error {
	r.race()
	return r.Writer.Push(ctx, branch, id, expect)
}

const deliveryFile = "# Delivery\n\n## Rules\n\n### Sends are queued\n`queued-sends` · from docs/vision.md\n" +
	"Every send goes through the queue.\nBecause: sends survive a restart.\n\n## Questions\n\n" +
	"### Should ordering be per customer?\n`should-ordering-be-per-customer` · raised by physa · 2026-11-21\n" +
	"Nothing settled it.\n"

// sample is a map of two capabilities: delivery recovered, billing not yet.
func sample(f *fixture) Map {
	return Map{
		ProductName:    "Woodshop",
		ProductPromise: "Woodworkers plan their cuts and waste less wood.",
		ProductQuestions: []Question{
			{Question: "Should ordering be per customer?", SuggestedBy: []string{f.first[:7], "#3", "not a ref"}},
		},
		Capabilities: []Capability{
			{Name: "delivery", Promise: "Webhooks reach customers at least once.", File: "---\npromise: Webhooks reach customers at least once.\n---\n\n" + deliveryFile, Status: "recovered",
				Dropped: []Drop{{What: "decision", Value: "Six attempts", Reason: "a decision needs a date"}}},
			{Name: "billing", Promise: "Customers pay for what they use.", Status: "not yet recovered", Reason: "the run stopped before reaching it"},
		},
		Terms:    "**Cut**: One pass of the saw.",
		Sessions: []string{"s-propose", "s-delivery", "", "s-propose"},
		Date:     time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC),
		Summary: Summary{
			Dropped: []Drop{{What: "capability", Value: "misc", Reason: "the name is not lowercase words joined by single hyphens"}},
			Tokens: []Tokens{{Phase: "propose", Input: 1000, CachedInput: 200, Output: 300, Weighted: spend.Spend{Tokens: 2310, MicroUSD: 9240}},
				{Phase: "recover", Input: 4000, CachedInput: 0, Output: 700, Weighted: spend.Spend{Tokens: 4700, Unpriced: true}}},
			Hook: Hook{State: HookInstalled, Detail: ".git/hooks/prepare-commit-msg"},
		},
	}
}

var mapPaths = []string{".physa/README.md", ".physa/capabilities/billing/index.md", ".physa/capabilities/delivery/index.md",
	".physa/capabilities/delivery/questions.md", ".physa/capabilities/delivery/rules.md", ".physa/glossary.md",
	".physa/product/index.md", ".physa/product/questions.md"}

func show(t *testing.T, dir, rev, p string) string {
	t.Helper()
	out, err := gitCmd(dir, "show", rev+":"+p).Output()
	if err != nil {
		t.Fatalf("git show %s:%s: %v", rev, p, err)
	}
	return string(out)
}

func TestWritesExactlyTheMapFiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	out, err := f.run(sample(f), Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	diff := gitIn(t, f.dir, "diff", "--name-status", "main.."+Branch)
	t.Logf("git diff --name-status main..%s:\n%s", Branch, diff)
	var lines []string
	for _, p := range mapPaths {
		lines = append(lines, "A\t"+p)
	}
	want := strings.Join(append(lines, "A\tAGENTS.md"), "\n")
	if diff != want {
		t.Errorf("diff =\n%s\nwant\n%s", diff, want)
	}
	if wantFiles := append(append([]string{}, mapPaths...), "AGENTS.md"); !reflect.DeepEqual(out.Files, wantFiles) {
		t.Errorf("Files = %v, want %v", out.Files, wantFiles)
	}
	if got := gitIn(t, f.dir, "rev-list", "--count", "main.."+Branch); got != "1" {
		t.Errorf("%s holds %s changes past main, want 1", Branch, got)
	}
	if out.Pushed || out.PR.State != PRNotAsked || ref(f.bare, Branch) != "" {
		t.Errorf("pushed or PR without asking: %+v", out)
	}
	tip := ref(f.dir, Branch)
	checks := map[string]string{
		".physa/README.md":   mapfolder.Readme,
		"AGENTS.md":          agentspointer.Pointer + "\n",
		".physa/glossary.md": "# Glossary\n\n**Cut**: One pass of the saw.\n",
		".physa/capabilities/billing/index.md": "---\npromise: Customers pay for what they use.\n---\n\n# Billing\n\n" +
			"Customers pay for what they use.\n",
		".physa/product/index.md": "---\npromise: Woodworkers plan their cuts and waste less wood.\n---\n\n# Woodshop\n\n" +
			"Woodworkers plan their cuts and waste less wood.\n\n- [Questions](questions.md)\n",
		".physa/product/questions.md": "# Woodshop · Questions\n\n## Contents\n\n" +
			"- [Should ordering be per customer?](#should-ordering-be-per-customer)\n\n## Entries\n\n" +
			"### Should ordering be per customer?\n`should-ordering-be-per-customer-2` · raised by physa · 2026-11-21 · " + f.first[:7] + " #3\n" + questionBody + "\n",
	}
	for p, want := range checks {
		if got := show(t, f.dir, tip, p); got != want {
			t.Errorf("%s =\n%q\nwant\n%q", p, got, want)
		}
	}
	msg := gitIn(t, f.dir, "log", "-1", "--format=%B", Branch)
	wantMsg := "docs(map): Add the map recovered by physa init\n\nInit proposed 2 capabilities from the repo's docs, history and PRs:\n" +
		"1 recovered, 1 not yet recovered and 0 failed.\nThose not recovered hold their promise only, to be filled from future PRs.\n\n" +
		"Session: s-propose\nSession: s-delivery"
	if msg != wantMsg {
		t.Errorf("message =\n%s\nwant\n%s", msg, wantMsg)
	}
	if strings.Contains(msg, "Capability:") {
		t.Error("init's commit carries a Capability: trailer")
	}
}

func TestBranchTreePassesTheMapCheck(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	if _, err := f.run(sample(f), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "check")
	gitIn(t, f.dir, "worktree", "add", "-q", "--detach", dir, Branch)
	defer gitIn(t, f.dir, "worktree", "remove", "--force", dir)
	res, err := mapcheck.CheckWith(t.Context(), dir, mapcheck.Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) {
		return git.Open(context.Background(), d, gitEnv)
	}})
	if err != nil || len(res.Problems) > 0 || res.Capabilities != 2 {
		t.Fatalf("check = %+v, %v", res, err)
	}
}

func TestMapFailingTheCheckWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	m := sample(f)
	m.Capabilities[0].File = "---\npromise: Webhooks reach customers at least once.\n---\n\n# Delivery\n\n## Rules\n\n### Sends are queued\nno meta line here\n"
	out, err := f.run(m, f.opts(true, true), nil)
	if err == nil {
		t.Fatal("a failing map was written")
	}
	t.Logf("error: %v", err)
	if !strings.Contains(err.Error(), "fails the map check") || !strings.Contains(err.Error(), ".physa/capabilities/delivery/rules.md:") {
		t.Errorf("error does not list the problem: %v", err)
	}
	branches := gitIn(t, f.dir, "branch", "--list", Branch)
	t.Logf("git branch --list %s: %q", Branch, branches)
	if branches != "" || ref(f.bare, Branch) != "" {
		t.Errorf("a branch was made: %q, remote %q", branches, ref(f.bare, Branch))
	}
	t.Logf("PR calls: %v", f.forge.calls)
	if out.Commit != nil || out.Pushed || len(f.forge.calls) != 0 {
		t.Errorf("something was done: %+v, calls %v", out, f.forge.calls)
	}
}

func TestPushOpensThePRThenRefreshesIt(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	out, err := f.run(sample(f), f.opts(true, true), nil)
	if err != nil {
		t.Fatal(err)
	}
	diff := gitIn(t, f.bare, "diff", "--name-status", "main.."+Branch)
	t.Logf("git diff --name-status main..%s in the bare remote:\n%s", Branch, diff)
	for _, line := range strings.Split(diff, "\n") {
		p := strings.TrimPrefix(line, "A\t")
		if p != "AGENTS.md" && !strings.HasPrefix(p, ".physa/") {
			t.Errorf("the remote's branch holds %q", line)
		}
	}
	if !out.Pushed || ref(f.bare, Branch) != out.Commit.ID || ref(f.dir, Branch) != out.Commit.ID {
		t.Errorf("not pushed and set: %+v", out)
	}
	if out.PR.State != PROpened || out.PR.Number != 1 {
		t.Errorf("PR = %+v", out.PR)
	}
	wantCalls := []string{"list " + Branch, "create " + Branch + " main " + out.Commit.Subject}
	if !reflect.DeepEqual(f.forge.calls, wantCalls) || f.forge.body != out.Body {
		t.Errorf("calls = %v, body set %v", f.forge.calls, f.forge.body == out.Body)
	}
	first := out.Commit.ID

	m := sample(f)
	m.Capabilities[1].File = "---\npromise: Customers pay for what they use.\n---\n\n# Billing\n\n## Rules\n\n### Invoices are monthly\n`monthly-invoices` · from docs/vision.md\nOne invoice a month.\nBecause: customers asked.\n"
	m.Capabilities[1].Status = "recovered"
	out, err = f.run(m, f.opts(true, true), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.PR.State != PRRefreshed || out.PR.Number != 1 {
		t.Errorf("second PR = %+v", out.PR)
	}
	if got := f.forge.calls[2:]; !reflect.DeepEqual(got, []string{"list " + Branch, "edit 1"}) {
		t.Errorf("second calls = %v", got)
	}
	if !strings.Contains(f.forge.body, "- rule: Invoices are monthly (from docs/vision.md)") {
		t.Errorf("the refreshed body misses billing's rule:\n%s", f.forge.body)
	}
	if got := gitIn(t, f.bare, "rev-list", "--count", "main.."+Branch); got != "1" || ref(f.bare, Branch) == first {
		t.Errorf("the branch was not replaced by one fresh change: %s past main", got)
	}
}

func TestLostLeaseIsAnErrorNamingTheBranch(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, f.dir, "clone", "-q", f.bare, other)
	w := &racingWriter{Writer: f.writer(), race: func() {
		gitIn(t, other, "checkout", "-q", "-b", Branch)
		gitIn(t, other, "commit", "-q", "--allow-empty", "-m", "someone else")
		gitIn(t, other, "push", "-q", "origin", Branch)
	}}
	out, err := f.run(sample(f), f.opts(true, true), w)
	if !errors.Is(err, vcs.ErrLease) || !strings.Contains(err.Error(), Branch) {
		t.Fatalf("err = %v, want a lost lease naming %s", err, Branch)
	}
	if out.Pushed || ref(f.dir, Branch) != "" || len(f.forge.calls) != 0 {
		t.Errorf("the run went on after the lease was lost: %+v, calls %v", out, f.forge.calls)
	}
	if ref(f.bare, Branch) != ref(other, "HEAD") {
		t.Error("the other push was replaced")
	}
}

func TestCheckedOutBranchIsRefused(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	gitIn(t, f.dir, "branch", Branch, f.first)
	dir := filepath.Join(t.TempDir(), "wt")
	gitIn(t, f.dir, "worktree", "add", "-q", dir, Branch)
	defer gitIn(t, f.dir, "worktree", "remove", "--force", dir)
	_, err := Run(t.Context(), f.writer(), sample(f), Options{})
	if err == nil || !strings.Contains(err.Error(), "checked out") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if ref(f.dir, Branch) != f.first {
		t.Error("the checked-out branch moved")
	}
}

func TestExistingMapIsRefused(t *testing.T) {
	t.Parallel()
	for _, p := range mapfolder.Products {
		t.Run(p, func(t *testing.T) {
			f := newFixture(t, nil)
			other := filepath.Join(t.TempDir(), "other")
			gitIn(t, f.dir, "clone", "-q", f.bare, other)
			writeFile(t, other, p, "---\npromise: P.\n---\n\n# P\n\nP.\n")
			gitIn(t, other, "add", ".")
			gitIn(t, other, "commit", "-q", "-m", "docs(map): Add a map")
			gitIn(t, other, "push", "-q", "origin", "main")
			_, err := f.run(sample(f), f.opts(true, false), nil)
			if err == nil || !strings.Contains(err.Error(), "the repo already has a map ("+p+" on origin/main)") || !strings.Contains(err.Error(), "physa map learn") {
				t.Fatalf("err = %v, want the existing map refused, naming %s", err, p)
			}
			if ref(f.dir, Branch) != "" || ref(f.bare, Branch) != "" {
				t.Error("a branch was written")
			}
		})
	}
}

func TestPRWithoutForgeIsReady(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	out, err := f.run(sample(f), Options{Push: true, PR: true, ForgeReason: "no remote on a forge"}, nil)
	if err != nil || out.PR != (PR{State: PRReady, Reason: "no remote on a forge"}) {
		t.Fatalf("PR = %+v, %v", out.PR, err)
	}
	out, err = f.run(sample(f), Options{Push: true, PR: true, PRs: unavailable{}}, nil)
	if err != nil || out.PR != (PR{State: PRReady, Reason: "gh is not installed"}) {
		t.Fatalf("PR = %+v, %v", out.PR, err)
	}
}

func TestPRNeedsPush(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	if _, err := f.run(sample(f), Options{PR: true, PRs: f.forge}, nil); err == nil {
		t.Fatal("a PR without a push was allowed")
	}
}

func TestNewCodebaseHasNoCapabilities(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	m := Map{ProductName: "Woodshop", ProductPromise: "Woodworkers plan their cuts.", Terms: "**Cut**: One pass of the saw."}
	out, err := f.run(m, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Commit.Subject != "docs(map): Start the map for a new codebase" {
		t.Errorf("subject = %q", out.Commit.Subject)
	}
	files := gitIn(t, f.dir, "diff", "--name-only", "main.."+Branch)
	if files != ".physa/README.md\n.physa/glossary.md\n.physa/product/index.md\nAGENTS.md" {
		t.Errorf("files =\n%s", files)
	}
	want := "---\npromise: Woodworkers plan their cuts.\n---\n\n# Woodshop\n\nWoodworkers plan their cuts.\n"
	if got := show(t, f.dir, Branch, ".physa/product/index.md"); got != want {
		t.Errorf("product index.md =\n%q\nwant\n%q", got, want)
	}
	checkBranch(t, f, 0)
	if msg := gitIn(t, f.dir, "log", "-1", "--format=%B", Branch); strings.Contains(msg, "Session:") || strings.Contains(msg, "Capability:") {
		t.Errorf("message carries trailers:\n%s", msg)
	}
	if !strings.Contains(out.Body, "## Capabilities proposed\n\nNone yet.\n") {
		t.Errorf("body:\n%s", out.Body)
	}
}

func TestInvalidMapIsRefusedBeforeAnyCopy(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	for _, m := range []Map{
		{ProductPromise: "P."},
		{ProductName: "W", ProductPromise: "P.", Capabilities: []Capability{{Name: "../x", Promise: "P."}}},
		{ProductName: "W", ProductPromise: "P.", Capabilities: []Capability{{Name: "a", Promise: "P."}, {Name: "a", Promise: "Q."}}},
	} {
		if _, err := f.run(m, Options{}, nil); err == nil {
			t.Errorf("%+v was written", m)
		}
	}
}

func TestBodySummarisesTheRun(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	m := sample(f)
	out, err := f.run(m, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := Body(m)
	if body != out.Body {
		t.Errorf("Body(m) differs from the run's body:\n%s\n---\n%s", body, out.Body)
	}
	for _, want := range []string{
		"- `delivery`: Webhooks reach customers at least once.\n- `billing`: Customers pay for what they use.\n",
		"### delivery\n\n- rule: Sends are queued (from docs/vision.md)\n",
		"### billing\n\nNot yet recovered: the run stopped before reaching it. Its folder holds its promise only",
		"- product: Should ordering be per customer? (" + f.first[:7] + " #3)\n- delivery: Should ordering be per customer?\n",
		"- proposal: capability \"misc\": the name is not lowercase words joined by single hyphens\n",
		"- delivery: decision \"Six attempts\": a decision needs a date\n",
		"| propose | 1000 | 200 | 300 | 2.3k | about $0.01 |\n| recover | 4000 | 0 | 700 | 4.7k | none: no price yet |\n| total | 5000 | 200 | 1000 | 7k | none: no price yet |\n",
		"Installed at .git/hooks/prepare-commit-msg",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body misses %q:\n%s", want, body)
		}
	}
	for _, h := range []struct {
		hook Hook
		want string
	}{
		{Hook{}, "Not asked for."},
		{Hook{State: HookAlready}, "Already installed"},
		{Hook{State: HookRefused, Detail: "core.hooksPath is set to .husky"}, "Not installed: core.hooksPath is set to .husky"},
	} {
		if got := hookLine(h.hook); !strings.HasPrefix(got, h.want) {
			t.Errorf("hookLine(%+v) = %q, want %q", h.hook, got, h.want)
		}
	}
}

func TestBodySkillSection(t *testing.T) {
	t.Parallel()
	const line = "Claude Code has no Physa skill on this account: run `physa skill setup` to install it, so its sessions learn Physa's commands and terms."
	f := newFixture(t, nil)
	m := sample(f)
	m.Summary.Skill = line
	body := Body(m)
	want := "\n## The Session: hook\n\n" + hookLine(m.Summary.Hook) + "\n\n## Physa's skill\n\n" + line + "\n"
	if !strings.HasSuffix(body, want) {
		t.Errorf("body does not end with the skill section:\n%s", body)
	}
}

func TestBodyWithoutSkillHasNoSection(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	m := sample(f)
	if body := Body(m); strings.Contains(body, "Physa's skill") {
		t.Errorf("body with an empty Skill names the skill:\n%s", body)
	}
}

func TestHasMap(t *testing.T) {
	t.Parallel()
	for _, p := range mapfolder.Products {
		t.Run(p, func(t *testing.T) {
			f := newFixture(t, nil)
			if err := HasMap(t.Context(), f.writer()); err != nil {
				t.Fatalf("HasMap without a map = %v", err)
			}
			writeFile(t, f.dir, p, "---\npromise: P.\n---\n\n# P\n\nP.\n")
			gitIn(t, f.dir, "add", ".")
			gitIn(t, f.dir, "commit", "-q", "-m", "docs(map): Add a map")
			gitIn(t, f.dir, "push", "-q", "origin", "main")
			err := HasMap(t.Context(), f.writer())
			t.Logf("HasMap: %v", err)
			if err == nil || !strings.Contains(err.Error(), "the repo already has a map ("+p+" on ") || !strings.Contains(err.Error(), "run physa map learn") {
				t.Fatalf("HasMap with a map = %v, want the refusal naming %s", err, p)
			}
		})
	}
}

// checkBranch checks the tree of the branch with the map check, which must
// find no problem, and the number of capabilities it reads.
func checkBranch(t *testing.T, f *fixture, capabilities int) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "check")
	gitIn(t, f.dir, "worktree", "add", "-q", "--detach", dir, Branch)
	defer gitIn(t, f.dir, "worktree", "remove", "--force", dir)
	res, err := mapcheck.CheckWith(t.Context(), dir, mapcheck.Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) {
		return git.Open(context.Background(), d, gitEnv)
	}})
	if err != nil || len(res.Problems) > 0 || res.Capabilities != capabilities {
		t.Fatalf("check = %+v, %v", res, err)
	}
}

func TestWriteDropsWhatTheCheckWouldReject(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	m := sample(f)
	m.ProductQuestions = append(m.ProductQuestions,
		Question{Question: "Should ordering be per customer?"},
		Question{Question: "Is <html> allowed in *notes*?"})
	m.Capabilities[0].File = "---\npromise: Webhooks reach customers at least once.\n---\n\n# Delivery\n\n## Rules\n\n" +
		"### Sends are *queued*\n`queued-sends` · from docs/vision.md\nEvery send goes through the queue.\nBecause: sends survive a restart.\n\n" +
		"### Sends retry\n`sends-retry` · from docs/vision.md\nA failed send is sent again.\nBecause: networks fail.\n\n" +
		"## Decisions\n\n### Six attempts\n`six-attempts` · decided by W, choosing an option · 2026-11-20 · from docs/vision.md\nA send is tried six times.\nBecause: six covers an hour.\n\n" +
		"### Six attempts\n`six-tries` · decided by W, choosing an option · 2026-11-20 · from docs/vision.md\nA send is tried six times, again.\nBecause: six covers an hour.\n"
	m.Capabilities[1].File = "---\npromise: Customers pay for what they use.\n---\n\n# Billing\n\nOne paragraph.\n\nAnd a second.\n\n## Rules\n"
	m.Capabilities[1].Status = "recovered"
	out, err := f.run(m, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkBranch(t, f, 2)
	t.Logf("body:\n%s", out.Body)
	for _, want := range []string{
		"- delivery: rule \"Sends are *queued*\": heading",
		"- delivery: decision \"Six attempts\": its heading is the same as, or has the anchor of,",
		"- billing: capability \"billing\": its recovered text does not split into a folder (line ",
		"- proposal: question \"Should ordering be per customer?\": its heading is the same as, or has the anchor of,",
		"- product: Is html allowed in notes?",
	} {
		if !strings.Contains(out.Body, want) {
			t.Errorf("body misses %q", want)
		}
	}
	if body := Body(m); body != out.Body {
		t.Errorf("Body(m) differs from the run's body:\n%s", body)
	}
	if len(m.Capabilities[0].Dropped) != 1 || len(m.Summary.Dropped) != 1 {
		t.Errorf("the caller's map changed: %+v", m)
	}
	want := "---\npromise: Customers pay for what they use.\n---\n\n# Billing\n\nCustomers pay for what they use.\n"
	if got := show(t, f.dir, Branch, ".physa/capabilities/billing/index.md"); got != want {
		t.Errorf("billing index.md =\n%q\nwant\n%q", got, want)
	}
	if got := show(t, f.dir, Branch, ".physa/capabilities/delivery/rules.md"); strings.Contains(got, "queued") || !strings.Contains(got, "### Sends retry") {
		t.Errorf("delivery rules.md:\n%s", got)
	}
}

func TestWriteOverviewFromTheText(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	m := sample(f)
	m.Capabilities[0].File = strings.Replace(m.Capabilities[0].File, "# Delivery\n", "# Delivery\n\nWebhooks are queued and sent again until they land.\n", 1)
	if _, err := f.run(m, Options{}, nil); err != nil {
		t.Fatal(err)
	}
	checkBranch(t, f, 2)
	got := show(t, f.dir, Branch, ".physa/capabilities/delivery/index.md")
	if !strings.Contains(got, "# Delivery\n\nWebhooks are queued and sent again until they land.\n\n- [Rules](rules.md)\n- [Questions](questions.md)\n") {
		t.Errorf("delivery index.md:\n%s", got)
	}
}

// TestReadmeMatchesTheMapsOwn keeps init's README text the same as Physa's
// own .physa/README.md, so the map describes the layout it is written in.
func TestReadmeMatchesTheMapsOwn(t *testing.T) {
	t.Parallel()
	own, err := os.ReadFile("../../.physa/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(own) != mapfolder.Readme {
		t.Errorf(".physa/README.md differs from mapfolder.Readme:\n%s", own)
	}
}

const productRule1 = "### Plans come first\n`plans-first` · from docs/vision.md\nEvery cut is planned before the saw runs.\nBecause: waste starts at the saw.\n"
const productRule2 = "### Offcuts are kept\n`offcuts-kept` · from docs/vision.md\nOffcuts go to the shelf.\nBecause: they save the next cut.\n"

func TestProductRulesAreWrittenAndPassTheCheck(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	m := sample(f)
	m.ProductRules = []string{productRule1, productRule2}
	if _, err := f.run(m, Options{}, nil); err != nil {
		t.Fatal(err)
	}
	got := show(t, f.dir, Branch, ".physa/product/rules.md")
	t.Logf(".physa/product/rules.md:\n%s", got)
	for _, want := range []string{"### Plans come first\n`plans-first`", "### Offcuts are kept\n`offcuts-kept`", "Because: they save the next cut."} {
		if !strings.Contains(got, want) {
			t.Errorf("rules.md lacks %q", want)
		}
	}
	dir := filepath.Join(t.TempDir(), "check")
	gitIn(t, f.dir, "worktree", "add", "-q", "--detach", dir, Branch)
	defer gitIn(t, f.dir, "worktree", "remove", "--force", dir)
	res, err := mapcheck.CheckWith(t.Context(), dir, mapcheck.Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) {
		return git.Open(context.Background(), d, gitEnv)
	}})
	if err != nil || len(res.Problems) > 0 {
		t.Fatalf("check = %+v, %v", res, err)
	}
}

func TestProductRulesSlugsAreTaken(t *testing.T) {
	t.Parallel()
	m := Map{ProductName: "Woodshop", ProductPromise: "Woodworkers plan.",
		ProductRules:     []string{"### Plans first\n`plans-first` · from docs/vision.md\nBody.\nBecause: reasons.\n"},
		ProductQuestions: []Question{{Question: "Plans first"}}}
	files, _ := mapFiles(m)
	q := files[".physa/product/questions.md"]
	if !strings.Contains(q, "`plans-first-2`") || strings.Contains(q, "`plans-first` ·") {
		t.Errorf("question slug collides with a rule's:\n%s", q)
	}
}

func TestNoProductRulesWritesNoRulesFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil)
	if _, err := f.run(sample(f), Options{}, nil); err != nil {
		t.Fatal(err)
	}
	out, err := gitCmd(f.dir, "cat-file", "-e", Branch+":.physa/product/rules.md").CombinedOutput()
	t.Logf("cat-file: %v %s", err, out)
	if err == nil {
		t.Error("a rules file was written with no product rules")
	}
}
