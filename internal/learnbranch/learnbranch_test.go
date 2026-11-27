package learnbranch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/propose"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// gitEnv is the environment of every git a test runs, its own and the ones
// the repo and writer open: the test process's, with an identity, no signing
// and main as the first branch, given as command-line config, so no commit
// reads the machine's config.
var gitEnv = append(os.Environ(),
	"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=4",
	"GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=W",
	"GIT_CONFIG_KEY_1=user.email", "GIT_CONFIG_VALUE_1=w@example.com",
	"GIT_CONFIG_KEY_2=commit.gpgsign", "GIT_CONFIG_VALUE_2=false",
	"GIT_CONFIG_KEY_3=init.defaultBranch", "GIT_CONFIG_VALUE_3=main")

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnv
	out, err := cmd.CombinedOutput()
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

// fixture is the person's clone dir of the bare remote bare, holding a map
// in its first change, and a second clone other standing for someone else.
type fixture struct {
	t                *testing.T
	dir, bare, other string
	first            string // the map's change
	forge            *fakeForge
}

// writeFolder writes text, a capability as a single text, as the folder dir
// under root, through mapfolder.Split.
func writeFolder(t *testing.T, root, dir, text string) {
	t.Helper()
	f, err := mapfolder.Split(text)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	if err := f.Write(root, dir); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir(), bare: t.TempDir(), forge: &fakeForge{prs: map[string]forge.PR{}, errFor: map[string]error{}}}
	gitIn(t, f.bare, "init", "-q", "--bare", "-b", "main")
	gitIn(t, f.dir, "init", "-q", "-b", "main")
	writeFile(t, f.dir, "docs/vision.md", "vision\n")
	writeFile(t, f.dir, ".physa/README.md", "map\n")
	writeFile(t, f.dir, ".physa/glossary.md", "# Glossary\n\n**Map**: the product's knowledge.\n")
	writeFolder(t, f.dir, ".physa/product", "---\npromise: A product.\n---\n# Product\n\n## Rules\n\n"+
		"### Cite sources\n`cite-sources` · from docs/vision.md\nEvery rule names its source.\nBecause: people check.\n")
	writeFolder(t, f.dir, ".physa/capabilities/delivery", "---\npromise: Webhooks reach customers at least once.\nstewards: [dana]\n---\n# Delivery\n\n"+
		"## Rules\n\n### Sends are queued\n`queued-sends` · from docs/vision.md\nEvery send goes through the queue.\nBecause: sends survive a restart.\n")
	gitIn(t, f.dir, "add", ".")
	gitIn(t, f.dir, "commit", "-q", "-m", "chore: Add the map")
	f.first = gitIn(t, f.dir, "rev-parse", "HEAD")
	gitIn(t, f.dir, "remote", "add", "origin", f.bare)
	gitIn(t, f.dir, "push", "-q", "origin", "main")
	gitIn(t, f.dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	f.other = filepath.Join(t.TempDir(), "other")
	gitIn(t, f.dir, "clone", "-q", f.bare, f.other)
	return f
}

// change commits an empty change on the person's main and pushes it.
func (f *fixture) change(subject string) string {
	f.t.Helper()
	gitIn(f.t, f.dir, "commit", "-q", "--allow-empty", "-m", subject)
	gitIn(f.t, f.dir, "push", "-q", "origin", "main")
	return gitIn(f.t, f.dir, "rev-parse", "HEAD")
}

// corrected gives change id a PR whose review came before its last commit.
func (f *fixture) corrected(id string) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	f.forge.prs[id] = forge.PR{Number: 7,
		Comments: []forge.Comment{{Author: "ana", At: at, Kind: forge.KindReview, Body: "retry this"}},
		Commits:  []forge.Commit{{ID: id, At: at.Add(time.Hour)}}}
}

func (f *fixture) opts(a *fakeAgent, push, pr bool) Options {
	return Options{Push: push, PR: pr, Forge: f.forge, PRs: f.forge,
		Session: propose.Session{
			Agent:    func() (adapter.Adapter, error) { return a, nil },
			Appetite: 40000,
			Check: propose.Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) {
				return git.Open(context.Background(), d, gitEnv)
			}},
		}}
}

// run runs w, by default the person's own Writer, and checks that their
// working copy is as it was and no other working copy is left.
func (f *fixture) run(o Options, w vcs.Writer) (Outcome, error) {
	f.t.Helper()
	if w == nil {
		w = f.writer()
	}
	before := f.state()
	out, err := Run(context.Background(), w, o)
	if after := f.state(); after != before {
		f.t.Errorf("the working copy changed:\nbefore %s\nafter  %s", before, after)
	}
	if list := gitIn(f.t, f.dir, "worktree", "list"); strings.Count(list, "\n") != 0 {
		f.t.Errorf("working copies left:\n%s", list)
	}
	return out, err
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
	return gitIn(f.t, f.dir, "status", "--porcelain=v2", "--branch", "--untracked-files=all") + "|" + gitIn(f.t, f.dir, "rev-parse", "HEAD")
}

// ref returns what ref names in dir, or "" when it names nothing.
func ref(dir, name string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", name)
	cmd.Env = gitEnv
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// message is the message of rev in dir, without its last newline.
func message(t *testing.T, dir, rev string) string {
	return gitIn(t, dir, "log", "-1", "--format=%B", rev)
}

// subjects are the subjects of rng in dir, newest first.
func subjects(t *testing.T, dir, rng string) []string {
	out := gitIn(t, dir, "log", "--format=%s", rng)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func short(id string) string { return id[:7] }

// rule is a session's proposal of a rule in delivery from a correction on id.
func rule(slug, id string) string {
	s := short(id)
	return fmt.Sprintf(`{"kind":"rule","capability":"delivery","section":"Rules","signal":{"kind":"correction","changes":[%q]},"changes":[%q],"entry":"### Rule %s\n`+"`%s`"+` · %s\nWhat a reviewer asks for is done before merge.\nBecause: the review said so."}`, s, s, slug, slug, s)
}

func agentFor(rules ...string) *fakeAgent {
	text := "```json\n{\"proposals\":[" + strings.Join(rules, ",") + "]}\n```\n"
	return &fakeAgent{result: adapter.Result{Text: text, Tokens: adapter.Tokens{Input: 100, Output: 20}}}
}

// fakeAgent replays scripted usage and text, and counts its sessions.
type fakeAgent struct {
	reports []adapter.Tokens
	result  adapter.Result
	starts  int
}

func (a *fakeAgent) Name() string { return "fake" }
func (a *fakeAgent) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	a.starts++
	return fakeSession{a: a, usage: s.Usage}, nil
}

type fakeSession struct {
	a     *fakeAgent
	usage func(adapter.Tokens)
}

func (s fakeSession) Wait() (adapter.Result, error) {
	for _, t := range s.a.reports {
		s.usage(t)
	}
	return s.a.result, nil
}

// fakeForge answers PRs of changes from prs and holds at most one PR of its
// own, recording every call that reads or writes it.
type fakeForge struct {
	prs    map[string]forge.PR
	errFor map[string]error
	open   bool
	number int
	body   string
	calls  []string
}

func (f *fakeForge) Name() string                                { return "fake" }
func (f *fakeForge) PRExists(context.Context, int) (bool, error) { return false, nil }
func (f *fakeForge) ChangePR(_ context.Context, id string) (forge.PR, bool, error) {
	if err := f.errFor[id]; err != nil {
		return forge.PR{}, false, err
	}
	pr, ok := f.prs[id]
	return pr, ok, nil
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

// racingWriter stands for someone else writing the remote's physa/learn
// just before each of the first races pushes.
type racingWriter struct {
	vcs.Writer
	races  int
	pushes int
	race   func(n int)
}

func (r *racingWriter) Push(ctx context.Context, branch, id, expect string) error {
	r.pushes++
	if r.pushes <= r.races {
		r.race(r.pushes)
	}
	return r.Writer.Push(ctx, branch, id, expect)
}

// otherLearns commits, in the other clone, a learning change on physa/learn
// adding file, and pushes it.
func (f *fixture) otherLearns(file, msg string) string {
	f.t.Helper()
	gitIn(f.t, f.other, "fetch", "-q", "origin")
	if ref(f.other, "refs/remotes/origin/"+Branch) != "" {
		gitIn(f.t, f.other, "checkout", "-q", "-B", Branch, "origin/"+Branch)
	} else {
		gitIn(f.t, f.other, "checkout", "-q", "-B", Branch, "origin/main")
	}
	writeFile(f.t, f.other, file, file+"\n")
	gitIn(f.t, f.other, "add", file)
	gitIn(f.t, f.other, "commit", "-q", "-m", msg)
	gitIn(f.t, f.other, "push", "-q", "origin", Branch)
	return gitIn(f.t, f.other, "rev-parse", "HEAD")
}

func wantMessage(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("message =\n%s\nwant\n%s", got, want)
	}
}

func text(t *testing.T, o Outcome) string {
	t.Helper()
	var b bytes.Buffer
	if err := WriteText(&b, o); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// learnOnce runs a first learning run with a correction on a new change, and
// returns that change.
func (f *fixture) learnOnce() string {
	f.t.Helper()
	c := f.change("feat: Retry sends")
	f.corrected(c)
	a := agentFor(rule("retry-on-review", c))
	out, err := f.run(f.opts(a, true, true), nil)
	if err != nil || out.Commit == nil || a.starts != 1 {
		f.t.Fatalf("first run: %v, %+v, %d sessions", err, out.Commit, a.starts)
	}
	return c
}

func TestLearningCommitCarriesTheDraftSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := f.change("feat: Retry sends")
	f.corrected(c)
	a := agentFor(rule("retry-on-review", c))
	a.result.SessionID = "11111111-2222-4333-8444-555555555555"
	if _, err := f.run(f.opts(a, true, true), nil); err != nil {
		t.Fatal(err)
	}
	line := "- rule retry-on-review in delivery, from correction on " + short(c)
	wantMessage(t, message(t, f.bare, Branch), "docs(map): Learn from 2 changes\n\n"+line+
		"\n\nLearned-through: "+c+"\nLearned: "+short(c)+"\nSession: "+a.result.SessionID+"\nCapability: learning")
}

func TestCorrectionIsCommittedPushedAndOpened(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := f.change("feat: Retry sends")
	f.corrected(c)
	a := agentFor(rule("retry-on-review", c))
	out, err := f.run(f.opts(a, true, true), nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.starts != 1 || out.Commit == nil || !out.Pushed {
		t.Fatalf("sessions %d, commit %+v, pushed %v", a.starts, out.Commit, out.Pushed)
	}
	line := "- rule retry-on-review in delivery, from correction on " + short(c)
	wantMessage(t, message(t, f.bare, Branch), "docs(map): Learn from 2 changes\n\n"+line+
		"\n\nLearned-through: "+c+"\nLearned: "+short(c)+"\nCapability: learning")
	if got := ref(f.bare, Branch); got != out.Commit.ID || ref(f.dir, "refs/heads/"+Branch) != got {
		t.Errorf("remote %s, local %s, commit %s", got, ref(f.dir, "refs/heads/"+Branch), out.Commit.ID)
	}
	if parent := ref(f.bare, Branch+"^"); parent != c {
		t.Errorf("parent %s, want the default branch's tip %s", parent, c)
	}
	if file := gitIn(t, f.bare, "show", Branch+":.physa/capabilities/delivery/rules.md"); !strings.Contains(file, "`retry-on-review` · "+short(c)) {
		t.Errorf("the entry is not on the branch:\n%s", file)
	}
	want := []string{"list physa/learn", "create physa/learn main " + prTitle}
	if !reflect.DeepEqual(f.forge.calls, want) {
		t.Errorf("forge calls %q, want %q", f.forge.calls, want)
	}
	if f.forge.body != prIntro+"\n\n"+line+"\n" {
		t.Errorf("PR body %q", f.forge.body)
	}
	if out.PR != (PR{State: PROpened, Number: 1, URL: "https://example.com/pr/1"}) {
		t.Errorf("PR %+v", out.PR)
	}
	got := text(t, out)
	for _, w := range []string{"Wrote " + out.Commit.Short + " to physa/learn: docs(map): Learn from 2 changes\n", "Pushed physa/learn.\n", "Opened PR #1: https://example.com/pr/1\n"} {
		if !strings.Contains(got, w) {
			t.Errorf("text lacks %q:\n%s", w, got)
		}
	}

	again := agentFor()
	tip := ref(f.bare, Branch)
	out, err = f.run(f.opts(again, true, true), nil)
	if err != nil || out.Commit != nil || out.Pushed || again.starts != 0 {
		t.Fatalf("second run: %v, commit %+v, pushed %v, sessions %d", err, out.Commit, out.Pushed, again.starts)
	}
	if ref(f.bare, Branch) != tip || len(f.forge.calls) != 2 || out.PR.State != PRNone {
		t.Errorf("second run moved the branch or the PR: %s, %q, %+v", ref(f.bare, Branch), f.forge.calls, out.PR)
	}
	if got := text(t, out); !strings.HasPrefix(got, "Nothing new to record on physa/learn.\n") {
		t.Errorf("text = %q", got)
	}
}

func TestTrailersOnlyCommitReplaysTheEarlierOne(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := f.learnOnce()
	line := "- rule retry-on-review in delivery, from correction on " + short(c)
	tidy := f.change("chore: Tidy")
	a := agentFor()
	out, err := f.run(f.opts(a, true, true), nil)
	if err != nil || out.Commit == nil || !out.Pushed || a.starts != 0 {
		t.Fatalf("%v, commit %+v, pushed %v, sessions %d", err, out.Commit, out.Pushed, a.starts)
	}
	wantMessage(t, message(t, f.bare, Branch), "docs(map): Learn from 1 change\n\n"+noProposals+
		"\n\nLearned-through: "+tidy+"\nCapability: learning")
	want := []string{"docs(map): Learn from 1 change", "docs(map): Learn from 2 changes"}
	if got := subjects(t, f.bare, "main.."+Branch); !reflect.DeepEqual(got, want) {
		t.Errorf("branch = %q, want %q (the earlier one replayed under the new one)", got, want)
	}
	if base := ref(f.bare, Branch+"~2"); base != tidy {
		t.Errorf("rebuilt on %s, want %s", base, tidy)
	}
	wantCalls := []string{"list physa/learn", "create physa/learn main " + prTitle, "list physa/learn", "edit 1"}
	if !reflect.DeepEqual(f.forge.calls, wantCalls) || out.PR.State != PRRefreshed {
		t.Errorf("forge calls %q, PR %+v", f.forge.calls, out.PR)
	}
	if f.forge.body != prIntro+"\n\n"+line+"\n" {
		t.Errorf("PR body %q", f.forge.body)
	}

	f.forge.open = false // the PR was closed
	f.change("chore: Tidy more")
	out, err = f.run(f.opts(a, true, true), nil)
	if err != nil || out.Commit == nil || !out.Pushed {
		t.Fatalf("%v, commit %+v", err, out.Commit)
	}
	if got := f.forge.calls[len(wantCalls):]; !reflect.DeepEqual(got, []string{"list physa/learn"}) || out.PR.State != PRNone {
		t.Errorf("a run with no proposals and no open PR called %q, PR %+v", got, out.PR)
	}
}

func TestSquashMergeNeitherRelearnsNorReplays(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.learnOnce()
	learned := message(t, f.bare, Branch)
	gitIn(t, f.other, "fetch", "-q", "origin")
	gitIn(t, f.other, "checkout", "-q", "main")
	gitIn(t, f.other, "reset", "-q", "--hard", "origin/main")
	gitIn(t, f.other, "merge", "-q", "--squash", "origin/"+Branch)
	gitIn(t, f.other, "commit", "-q", "-m", prTitle+" (#1)\n\n"+learned)
	gitIn(t, f.other, "push", "-q", "origin", "main", ":"+Branch)

	a := agentFor()
	out, err := f.run(f.opts(a, true, true), nil)
	if err != nil || out.Commit != nil || a.starts != 0 || out.Result.Changes != 0 {
		t.Fatalf("after the merge: %v, commit %+v, sessions %d, changes %d", err, out.Commit, a.starts, out.Result.Changes)
	}
	if ref(f.bare, Branch) != "" {
		t.Error("a run with nothing new pushed the branch again")
	}

	gitIn(t, f.other, "commit", "-q", "--allow-empty", "-m", "chore: After the merge")
	gitIn(t, f.other, "push", "-q", "origin", "main")
	after := gitIn(t, f.other, "rev-parse", "HEAD")
	out, err = f.run(f.opts(a, true, true), nil)
	if err != nil || out.Commit == nil || a.starts != 0 {
		t.Fatalf("%v, commit %+v, sessions %d", err, out.Commit, a.starts)
	}
	if got := subjects(t, f.bare, "main.."+Branch); len(got) != 1 {
		t.Errorf("branch = %q, want the one new commit (nothing replayed)", got)
	}
	wantMessage(t, message(t, f.bare, Branch), "docs(map): Learn from 1 change\n\n"+noProposals+
		"\n\nLearned-through: "+after+"\nCapability: learning")
}

// twoCorrections is a fixture with corrections on two changes and an agent
// proposing a rule for each.
func twoCorrections(t *testing.T) (*fixture, string, string, *fakeAgent) {
	f := newFixture(t)
	retry := f.change("feat: Retry sends")
	sign := f.change("feat: Sign payloads")
	f.corrected(retry)
	f.corrected(sign)
	return f, retry, sign, agentFor(rule("retry-on-review", retry), rule("sign-on-review", sign))
}

func TestLeaseLostOnceIsRedoneOnce(t *testing.T) {
	t.Parallel()
	f, retry, sign, a := twoCorrections(t)
	otherLine := "- rule signed-payloads in delivery, from correction on " + short(sign)
	rw := &racingWriter{Writer: f.writer(), races: 1, race: func(int) {
		f.otherLearns("other.txt", "docs(map): Learn from 1 change\n\n"+otherLine+
			"\n\nLearned-through: "+f.first+"\nLearned: "+short(sign)+"\nCapability: learning")
	}}
	out, err := f.run(f.opts(a, true, true), rw)
	if err != nil {
		t.Fatal(err)
	}
	if a.starts != 1 || rw.pushes != 2 || !out.Redone || out.Superseded != 1 || !out.Pushed {
		t.Fatalf("sessions %d, pushes %d, redone %v, superseded %d, pushed %v", a.starts, rw.pushes, out.Redone, out.Superseded, out.Pushed)
	}
	line := "- rule retry-on-review in delivery, from correction on " + short(retry)
	wantMessage(t, message(t, f.bare, Branch), "docs(map): Learn from 1 change\n\n"+line+
		"\n\nLearned-through: "+sign+"\nLearned: "+short(retry)+"\nCapability: learning")
	if got := subjects(t, f.bare, "main.."+Branch); len(got) != 2 || ref(f.bare, Branch+"^:other.txt") == "" {
		t.Errorf("branch = %q, want the other run's commit under this one", got)
	}
	if ref(f.dir, "refs/heads/"+Branch) != ref(f.bare, Branch) {
		t.Error("the local branch is not what was pushed")
	}
	if len(out.Report.Kept) != 1 || out.Report.Kept[0].Slug != "retry-on-review" {
		t.Errorf("kept %+v", out.Report.Kept)
	}
	last := out.Report.Dropped[len(out.Report.Dropped)-1]
	if want := "another run on physa/learn learned from change `" + short(sign) + "` meanwhile"; last.Reason != want {
		t.Errorf("dropped %q, want %q", last.Reason, want)
	}
	if f.forge.body != prIntro+"\n\n"+otherLine+"\n"+line+"\n" {
		t.Errorf("PR body %q", f.forge.body)
	}
	if got := text(t, out); !strings.Contains(got, "redone once, without a new session, dropping 1 proposal learned meanwhile.\n") {
		t.Errorf("text = %q", got)
	}
}

func TestLeaseLostTwiceStops(t *testing.T) {
	t.Parallel()
	f, _, sign, a := twoCorrections(t)
	var theirs string
	rw := &racingWriter{Writer: f.writer(), races: 2, race: func(n int) {
		theirs = f.otherLearns(fmt.Sprintf("other%d.txt", n), "docs(map): Learn from 1 change\n\n- rule x in delivery, from correction on "+
			short(sign)+"\n\nLearned-through: "+f.first+"\nLearned: "+short(sign)+"\nCapability: learning")
	}}
	out, err := f.run(f.opts(a, true, true), rw)
	if err == nil || err.Error() != "physa/learn moved on the remote twice while this run wrote it; run physa map learn again" {
		t.Fatalf("err = %v", err)
	}
	t.Logf("second loss: %v", err)
	if a.starts != 1 || rw.pushes != 2 || out.Pushed {
		t.Errorf("sessions %d, pushes %d, pushed %v", a.starts, rw.pushes, out.Pushed)
	}
	if got := ref(f.bare, Branch); got != theirs {
		t.Errorf("remote physa/learn = %s, want only the other run's %s", got, theirs)
	}
	t.Logf("remote physa/learn holds only the other run's %s", short(theirs))
	if ref(f.dir, "refs/heads/"+Branch) != "" {
		t.Error("the local branch records what the remote refused")
	}
	if len(f.forge.calls) != 0 {
		t.Errorf("forge calls %q", f.forge.calls)
	}
}

func TestCheckedOutIsRefused(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := f.change("feat: Retry sends")
	f.corrected(c)
	gitIn(t, f.dir, "checkout", "-q", "-b", Branch)
	a := agentFor(rule("retry-on-review", c))
	_, err := f.run(f.opts(a, true, true), nil)
	want := "physa/learn is checked out in a working copy; switch that copy to another branch first, as Physa never writes a checked-out branch"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	t.Logf("refused: %v", err)
	if a.starts != 0 || ref(f.bare, Branch) != "" || ref(f.dir, "refs/heads/"+Branch) != c || len(f.forge.calls) != 0 {
		t.Errorf("sessions %d, remote %q, local %q, forge %q", a.starts, ref(f.bare, Branch), ref(f.dir, "refs/heads/"+Branch), f.forge.calls)
	}
}

func TestBreakerStopsAndWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	c := f.change("feat: Retry sends")
	f.corrected(c)
	a := agentFor(rule("retry-on-review", c))
	a.reports = []adapter.Tokens{{Input: 50000}}
	out, err := f.run(f.opts(a, true, true), nil)
	if err != nil || out.Report.Session != propose.SessionStopped || out.Commit != nil {
		t.Fatalf("%v, session %s, commit %+v", err, out.Report.Session, out.Commit)
	}
	if ref(f.bare, Branch) != "" || ref(f.dir, "refs/heads/"+Branch) != "" || len(f.forge.calls) != 0 {
		t.Error("a stopped session wrote something")
	}
	if got := text(t, out); got != "Nothing written to physa/learn: the session stopped at the appetite.\n" {
		t.Errorf("text = %q", got)
	}
}

func TestDroppedRecordKeepsItsLearnedIDs(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	held := f.change("chore: Held back")
	f.forge.errFor[held] = fmt.Errorf("the forge stumbled")
	retry := f.change("feat: Retry sends")
	f.corrected(retry)
	tidy := f.change("chore: Tidy")
	tree := gitIn(t, f.dir, "rev-parse", "HEAD^{tree}")
	rec := gitIn(t, f.dir, "commit-tree", tree, "-p", "HEAD", "-m", "docs(map): Learn from 1 change\n\n"+noProposals+
		"\n\nLearned-through: "+f.first+"\nLearned: "+short(tidy)+"\nCapability: learning")
	gitIn(t, f.dir, "update-ref", "refs/heads/"+Branch, rec)

	a := agentFor(rule("retry-on-review", retry))
	o := f.opts(a, false, false)
	out, err := f.run(o, nil)
	if err != nil || out.Commit == nil || out.Pushed {
		t.Fatalf("%v, commit %+v, pushed %v", err, out.Commit, out.Pushed)
	}
	line := "- rule retry-on-review in delivery, from correction on " + short(retry)
	wantMessage(t, message(t, f.dir, Branch), "docs(map): Learn from 2 changes\n\n"+line+
		"\n\nLearned-through: "+f.first+"\nLearned: "+short(retry)+", "+short(tidy)+"\nCapability: learning")
	if parent := ref(f.dir, Branch+"^"); parent != tidy {
		t.Errorf("parent %s, want %s (the record that added nothing is not replayed)", parent, tidy)
	}
	if ref(f.bare, Branch) != "" {
		t.Error("a run without push pushed")
	}
}

func TestFirstRunWithoutForgeStillRecordsLearnedThrough(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	writeFile(t, f.dir, "send.txt", "send\n")
	gitIn(t, f.dir, "add", "send.txt")
	gitIn(t, f.dir, "commit", "-q", "-m", "feat: Send twice")
	gitIn(t, f.dir, "revert", "--no-edit", "HEAD")
	gitIn(t, f.dir, "push", "-q", "origin", "main")
	rev := gitIn(t, f.dir, "rev-parse", "HEAD")
	s := short(rev)
	p := fmt.Sprintf(`{"kind":"rule","capability":"delivery","section":"Rules","signal":{"kind":"revert","changes":[%q]},"changes":[%q],"entry":"### Send once\n`+"`send-once`"+` · %s\nA send goes out once.\nBecause: sending twice was reverted."}`, s, s, s)
	o := f.opts(agentFor(p), true, true)
	o.Forge, o.PRs, o.ForgeReason = nil, nil, "no forge"
	out, err := f.run(o, nil)
	if err != nil || out.Commit == nil {
		t.Fatalf("run: %v, commit %+v", err, out.Commit)
	}
	wantMessage(t, message(t, f.bare, Branch), "docs(map): Learn from 3 changes\n\n- rule send-once in delivery, from revert on "+s+
		"\n\nLearned-through: none\nLearned: "+s+"\nCapability: learning")
	if out.PR.State != PRReady {
		t.Errorf("PR %+v, want ready for review", out.PR)
	}

	tip := ref(f.bare, Branch)
	again := agentFor()
	o.Session.Agent = func() (adapter.Adapter, error) { return again, nil }
	out, err = f.run(o, nil)
	if err != nil || out.Commit != nil || again.starts != 0 || ref(f.bare, Branch) != tip {
		t.Fatalf("second run: %v, commit %+v, sessions %d", err, out.Commit, again.starts)
	}
}

// TestIDWithASlashNamesOnlyItself shows a Learned id that names no commit,
// as older learning records hold, matches only itself, never a change.
func TestIDWithASlashNamesOnlyItself(t *testing.T) {
	t.Parallel()
	if sameChange("old/quiet-mode", "old/quiet-mode-2") || !sameChange("old/quiet-mode", " Old/Quiet-Mode ") {
		t.Error("an id with a slash does not name exactly itself")
	}
	if sameChange("abc1234", "abc1234/x") || !sameChange("abc1234", "abc1234def") {
		t.Error("a change id matches an id with a slash, or no longer its own prefix")
	}
}
