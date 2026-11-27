package gather

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/coverage"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather/merged"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

var base = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

type testRepo struct {
	t   *testing.T
	dir string
	n   int
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	r := &testRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	at := base.Add(time.Duration(r.n) * 24 * time.Hour).Format(time.RFC3339)
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_AUTHOR_DATE="+at,
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_COMMITTER_DATE="+at)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit makes the next commit, a day after the last, touching files.
func (r *testRepo) commit(message string, files ...string) string {
	r.t.Helper()
	r.n++
	for _, f := range files {
		write(r.t, r.dir, f, fmt.Sprintf("%s %d", f, r.n))
	}
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", message)
	return r.git("rev-parse", "HEAD")
}

func (r *testRepo) open() vcs.Repo {
	r.t.Helper()
	v, err := git.Open(context.Background(), r.dir, os.Environ())
	if err != nil {
		r.t.Fatal(err)
	}
	return v
}

func (r *testRepo) at(days int) time.Time { return base.Add(time.Duration(days) * 24 * time.Hour) }

// plainForge implements forge.Forge only.
type plainForge struct{}

func (plainForge) Name() string                                { return "fake" }
func (plainForge) PRExists(context.Context, int) (bool, error) { return false, nil }
func (plainForge) ChangePR(context.Context, string) (forge.PR, bool, error) {
	return forge.PR{}, false, nil
}

// mergedForge also implements forge.Merged.
type mergedForge struct {
	plainForge
	prs  []forge.PR
	more bool
	err  error
}

func (m mergedForge) MergedPRs(context.Context, time.Time, int) ([]forge.PR, bool, error) {
	return m.prs, m.more, m.err
}

func gatherNow(t *testing.T) time.Time { return base.Add(60 * 24 * time.Hour) }

func run(t *testing.T, r *testRepo, f forge.Forge, o Bounds) (Summary, int) {
	t.Helper()
	g, s, err := Run(t.Context(), r.dir, r.open(), f, o, Discover{}, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	return s, len(g.Changes)
}

// bounds is the bound of a default gather at gatherNow: twelve months back and
// no PR limit, changed by with.
func bounds(t *testing.T, with ...func(*Bounds)) Bounds {
	t.Helper()
	b := Bounds{Since: gatherNow(t).AddDate(-1, 0, 0)}
	for _, w := range with {
		w(&b)
	}
	return b
}

func TestRunHistory(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: first\n\nSession: s1\nCapability: init", "a.go", "b.go")
	bad := r.commit("feat: second\n\nbody", "b.go")
	r.git("revert", "--no-edit", bad)
	r.n++
	write(t, r.dir, "AGENTS.md", "agents")
	g, s, err := Run(t.Context(), r.dir, r.open(), nil, bounds(t), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	h := s.History
	if h.Changes != 3 || h.WithTrailers != 1 || h.Reverts != 1 || h.WithAltered != 3 || h.DistinctFiles != 2 {
		t.Errorf("history %+v", h)
	}
	var first, revert bool
	for _, c := range g.Changes {
		if c.Subject == "feat: first" {
			first = len(c.Trailers) == 2 && c.Trailers[0].Key == "Session" && c.Trailers[1].Value == "init" && len(c.Files) == 2
		}
		if strings.HasPrefix(c.Subject, "Revert") {
			revert = c.Reverts != "" && strings.HasPrefix(bad, c.Reverts)
		}
	}
	if !first || !revert {
		t.Errorf("first %v revert %v: %+v", first, revert, g.Changes)
	}
	if s.Docs.Count != 1 || s.Docs.Files[0].Path != "AGENTS.md" {
		t.Errorf("docs %+v", s.Docs)
	}
}

func TestRunSinceCutsOlderChanges(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: old", "a")
	r.commit("feat: mid", "a")
	r.commit("feat: new", "a")
	// Changes are on days 1, 2 and 3 at 10:00; the date cuts day 1.
	o := bounds(t, func(b *Bounds) {
		b.Since, b.SinceSet = time.Date(r.at(2).Year(), r.at(2).Month(), r.at(2).Day(), 0, 0, 0, 0, time.UTC), true
	})
	s, n := run(t, r, nil, o)
	if n != 2 || s.History.Changes != 2 {
		t.Errorf("changes %d", n)
	}
	if !s.Bound.FromFlag || s.Bound.Since != r.at(2).Format("2006-01-02") {
		t.Errorf("bound %+v", s.Bound)
	}
}

func TestRunUnavailableNoForge(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	s, _ := run(t, r, nil, bounds(t))
	if s.PRs.Available || s.PRs.Reason != "no forge" {
		t.Errorf("%+v", s.PRs)
	}
}

func TestRunUnavailableWithoutMerged(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	s, _ := run(t, r, plainForge{}, bounds(t))
	if s.PRs.Available || s.PRs.Reason == "" {
		t.Errorf("%+v", s.PRs)
	}
}

func TestRunUnavailableForgeError(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	f := mergedForge{err: &forge.Error{Class: forge.ErrUnavailable, Reason: "gh is not installed"}}
	g, s, err := Run(t.Context(), r.dir, r.open(), f, bounds(t), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatalf("an unavailable forge failed the run: %v", err)
	}
	if s.PRs.Available || s.PRs.Reason != "gh is not installed" || s.PRs.Count != 0 || len(g.PRs) != 0 {
		t.Errorf("%+v", s.PRs)
	}
	if g.Run.ForgeReason != "gh is not installed" || g.Run.Forge != "fake" {
		t.Errorf("%+v", g.Run)
	}
	var buf bytes.Buffer
	if err := WriteText(&buf, s); err != nil || !strings.Contains(buf.String(), "PRs: unavailable (gh is not installed)") {
		t.Errorf("%v\n%s", err, buf.String())
	}
	if s.History.Changes != 1 {
		t.Errorf("history still gathered: %+v", s.History)
	}
}

// pagesForge also implements forge.MergedPages: it replies with its answers
// in order and records the cursors asked.
type pagesForge struct {
	plainForge
	answers []pageAnswer
	cursors []string
}

type pageAnswer struct {
	page forge.Page
	err  error
}

func (p *pagesForge) MergedPage(_ context.Context, _ time.Time, cursor string) (forge.Page, error) {
	p.cursors = append(p.cursors, cursor)
	if len(p.answers) == 0 {
		return forge.Page{}, errors.New("fake forge: no answer left")
	}
	a := p.answers[0]
	p.answers = p.answers[1:]
	return a.page, a.err
}

func onePage(next string, prs ...forge.PR) pageAnswer {
	return pageAnswer{page: forge.Page{PRs: prs, Next: next}}
}

// fakeWait is a Wait whose sleeps are recorded and move a fake clock on.
func fakeWait(limit time.Duration, slept *[]time.Duration) merged.Wait {
	now := gatherNow(nil)
	return merged.Wait{
		Limit: limit,
		Sleep: func(d time.Duration) { *slept = append(*slept, d); now = now.Add(d) },
		Now:   func() time.Time { return now },
	}
}

func TestRunWaitsOutRateLimitAndKeepsEveryPage(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	secondary := &forge.RateLimit{Secondary: true, RetryAfter: time.Minute, Detail: "You have exceeded a secondary rate limit"}
	f := &pagesForge{answers: []pageAnswer{
		onePage("c1", forge.PR{Number: 5, MergedAt: r.at(5)}, forge.PR{Number: 4, MergedAt: r.at(4)}),
		onePage("c2", forge.PR{Number: 3, MergedAt: r.at(3)}),
		{err: secondary},
		onePage("", forge.PR{Number: 2, MergedAt: r.at(2)}, forge.PR{Number: 1, MergedAt: r.at(1)}),
	}}
	var slept []time.Duration
	g, s, err := Run(t.Context(), r.dir, r.open(), f, bounds(t, func(b *Bounds) { b.Wait = fakeWait(0, &slept) }), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatalf("a rate limit waited out failed the run: %v", err)
	}
	if len(g.PRs) != 5 || s.PRs.Count != 5 || !s.PRs.Available || g.Run.ForgeReason != "" {
		t.Errorf("%d PRs, summary %+v", len(g.PRs), s.PRs)
	}
	if want := []string{"", "c1", "c2", "c2"}; !reflect.DeepEqual(f.cursors, want) {
		t.Errorf("cursors %q, want %q", f.cursors, want)
	}
	if want := []time.Duration{time.Minute}; !reflect.DeepEqual(slept, want) {
		t.Errorf("slept %v, want %v", slept, want)
	}
}

func TestRunStoppedPastRateLimitKeepsThePRs(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	r.commit("feat: y", "b")
	nohint := pageAnswer{err: &forge.RateLimit{Secondary: true, Detail: "API rate limit exceeded"}}
	f := &pagesForge{answers: []pageAnswer{
		onePage("c1", forge.PR{Number: 5, MergedAt: r.at(5)}, forge.PR{Number: 4, MergedAt: r.at(4)}),
		onePage("c2", forge.PR{Number: 3, MergedAt: r.at(3)}),
		nohint, nohint, nohint, nohint,
	}}
	var slept []time.Duration
	limit := 3 * time.Minute // 60s and 120s are slept; 240s more is refused
	g, s, err := Run(t.Context(), r.dir, r.open(), f, bounds(t, func(b *Bounds) { b.Wait = fakeWait(limit, &slept) }), Discover{}, gatherNow(t))
	var stop *Stopped
	if !errors.As(err, &stop) {
		t.Fatalf("err = %v, want a *Stopped", err)
	}
	var rl *forge.RateLimit
	if !errors.As(err, &rl) || !rl.Secondary {
		t.Errorf("the stop does not wrap the rate limit: %v", err)
	}
	t.Logf("stopped: %v", err)
	msg := err.Error()
	reached := r.at(3).Format(time.DateOnly)
	asked := gatherNow(t).AddDate(-1, 0, 0).Format(time.DateOnly)
	for _, want := range []string{"rate limit", "the 3 PRs kept reach back to " + reached, "of the asked " + asked} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if stop.PRs != 3 || !stop.Reached.Equal(r.at(3)) {
		t.Errorf("stop %+v", stop)
	}
	if !g.Run.Stopped || !g.Run.Reached.Equal(r.at(3)) || g.Run.StopReason != stop.Err.Error() || !strings.Contains(g.Run.StopReason, "rate limit") {
		t.Errorf("the gather is not marked stopped: %+v", g.Run)
	}
	if len(g.PRs) != 3 || g.PRs[0].Number != 5 || s.PRs.Count != 3 || !s.PRs.Available {
		t.Errorf("%d PRs kept, summary %+v", len(g.PRs), s.PRs)
	}
	if g.Run.PRsLimited || s.Bound.PRLimitReached || s.Quiet != nil {
		t.Errorf("a stop marked the PR limit reached: %+v %+v", g.Run, s.Bound)
	}
	if len(g.Changes) != 2 || s.History.Changes != 2 {
		t.Errorf("history %d changes, want the whole bound's 2", len(g.Changes))
	}
	var total time.Duration
	for _, d := range slept {
		total += d
	}
	if total > limit {
		t.Errorf("slept %v in all, past the limit %v", total, limit)
	}
}

func TestRunStoppedOnPlainError(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	for name, f := range map[string]forge.Forge{
		"pages":     &pagesForge{answers: []pageAnswer{{err: errors.New("boom")}}},
		"merged":    mergedForge{err: errors.New("boom")},
		"not found": &pagesForge{answers: []pageAnswer{{err: &forge.Error{Class: forge.ErrNotFound, Reason: "the repo is not on GitHub"}}}},
	} {
		g, s, err := Run(t.Context(), r.dir, r.open(), f, bounds(t), Discover{}, gatherNow(t))
		var stop *Stopped
		if !errors.As(err, &stop) {
			t.Fatalf("%s: err = %v, want a *Stopped", name, err)
		}
		t.Logf("%s: stopped: %v", name, err)
		if stop.PRs != 0 || len(g.PRs) != 0 || !strings.Contains(err.Error(), "no PR was listed") {
			t.Errorf("%s: %v, %d PRs", name, err, len(g.PRs))
		}
		if s.History.Changes != 1 {
			t.Errorf("%s: history not gathered: %+v", name, s.History)
		}
		if !g.Run.Stopped || !g.Run.Reached.IsZero() || g.Run.StopReason != stop.Err.Error() {
			t.Errorf("%s: mark %+v, want stopped with no date reached", name, g.Run)
		}
	}
}

func TestRunStoppedWhenUnavailableAfterPRs(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	unauthenticated := pageAnswer{err: &forge.Error{Class: forge.ErrUnauthenticated, Reason: "gh is not logged in"}}
	f := &pagesForge{answers: []pageAnswer{onePage("c1", forge.PR{Number: 2, MergedAt: r.at(1)}), unauthenticated}}
	g, _, err := Run(t.Context(), r.dir, r.open(), f, bounds(t), Discover{}, gatherNow(t))
	var stop *Stopped
	if !errors.As(err, &stop) || len(g.PRs) != 1 {
		t.Fatalf("err = %v, %d PRs", err, len(g.PRs))
	}
	g, s, err := Run(t.Context(), r.dir, r.open(), &pagesForge{answers: []pageAnswer{unauthenticated}}, bounds(t), Discover{}, gatherNow(t))
	if err != nil || s.PRs.Available || s.PRs.Reason != "gh is not logged in" || len(g.PRs) != 0 {
		t.Errorf("unauthenticated before any PR: %v %+v", err, s.PRs)
	}
}

func TestRunNoRemoteBeforeAnyPRIsUnavailable(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	noRemote := pageAnswer{err: &forge.Error{Class: forge.ErrUnavailable, Reason: "gh finds no GitHub remote", Detail: "no git remotes found"}}
	g, s, err := Run(t.Context(), r.dir, r.open(), &pagesForge{answers: []pageAnswer{noRemote}}, bounds(t), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatalf("a repo with no remote stopped the gather: %v", err)
	}
	if s.PRs.Available || s.PRs.Reason != "gh finds no GitHub remote" || len(g.PRs) != 0 || g.Run.ForgeReason != "gh finds no GitHub remote" {
		t.Errorf("%+v %+v", s.PRs, g.Run)
	}
}

// cappedForge is mergedForge that records the max it was asked for.
type cappedForge struct {
	mergedForge
	max *int
}

func (c cappedForge) MergedPRs(_ context.Context, since time.Time, max int) ([]forge.PR, bool, error) {
	*c.max = max
	return c.mergedForge.MergedPRs(context.Background(), since, max)
}

func TestRunNoLimitListsEveryPRSince(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	pages := func() *pagesForge {
		return &pagesForge{answers: []pageAnswer{
			onePage("c1", forge.PR{Number: 4, MergedAt: r.at(4)}, forge.PR{Number: 3, MergedAt: r.at(3)}),
			onePage("c2", forge.PR{Number: 2, MergedAt: r.at(2)}),
			onePage("", forge.PR{Number: 1, MergedAt: r.at(1)}, forge.PR{Number: 0, MergedAt: r.at(-400)}),
		}}
	}
	g, s, err := Run(t.Context(), r.dir, r.open(), pages(), bounds(t), Discover{}, gatherNow(t))
	if err != nil || len(g.PRs) != 4 || s.Bound.MaxPRs != 0 || s.Bound.PRLimitReached {
		t.Errorf("no limit: %v, %d PRs, bound %+v", err, len(g.PRs), s.Bound)
	}
	var buf bytes.Buffer
	if err := WriteText(&buf, s); err != nil || !strings.Contains(buf.String(), "(the last 12 months), no PR limit.\n") {
		t.Errorf("%v\n%s", err, buf.String())
	}
	g, s, err = Run(t.Context(), r.dir, r.open(), pages(), bounds(t, func(b *Bounds) { b.MaxPRs = 2 }), Discover{}, gatherNow(t))
	if err != nil || len(g.PRs) != 2 || !s.Bound.PRLimitReached || s.Bound.MaxPRs != 2 {
		t.Errorf("max 2: %v, %d PRs, bound %+v", err, len(g.PRs), s.Bound)
	}
	var asked int
	if _, _, err := Run(t.Context(), r.dir, r.open(), cappedForge{max: &asked}, bounds(t), Discover{}, gatherNow(t)); err != nil || asked < 1<<30 {
		t.Errorf("a forge offering only Merged was asked for at most %d PRs (%v)", asked, err)
	}
}

func TestRunMoreStartsHistoryAtOldestPR(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	for i := 0; i < 5; i++ {
		r.commit(fmt.Sprintf("feat: c%d", i), "a")
	}
	// Changes on days 1..5. PRs merged on days 4 and 3 (newest first).
	f := mergedForge{more: true, prs: []forge.PR{
		{Number: 9, MergedAt: r.at(4)}, {Number: 8, MergedAt: r.at(3)},
	}}
	g, s, err := Run(t.Context(), r.dir, r.open(), f, bounds(t, func(b *Bounds) { b.MaxPRs = 2 }), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Changes) != 3 || !s.Bound.PRLimitReached || !g.Run.PRsLimited {
		t.Errorf("changes %d bound %+v", len(g.Changes), s.Bound)
	}
	if s.Bound.HistorySince != r.at(3).Format("2006-01-02") {
		t.Errorf("history since %s", s.Bound.HistorySince)
	}
	var buf bytes.Buffer
	if err := WriteText(&buf, s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "The PR limit was reached first; history starts at "+s.Bound.HistorySince+".") {
		t.Errorf("%s", buf.String())
	}
}

func TestRunPRCounts(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	f := mergedForge{prs: []forge.PR{
		{Number: 2, Title: "two", Body: "bb", MergedAt: r.at(1), Issues: []forge.Issue{{Number: 1, Title: "it", Body: "ib"}, {Number: 3, Title: "jt"}},
			Comments: []forge.Comment{
				{Author: "a", Kind: forge.KindReview, Body: "no", RequestsChanges: true},
				{Author: "b", Kind: forge.KindDiscussion, Body: "ok"},
			}},
		{Number: 1, Title: "one", MergedAt: r.at(0)},
	}}
	g, s, err := Run(t.Context(), r.dir, r.open(), f, bounds(t), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	p := s.PRs
	if !p.Available || p.Count != 2 || p.ReviewComments != 2 || p.RequestingChanges != 1 || p.LinkedIssues != 2 {
		t.Errorf("%+v", p)
	}
	if s.Bound.PRLimitReached || len(g.PRs) != 2 || !g.PRs[0].Comments[0].RequestsChanges {
		t.Errorf("%+v", g.PRs)
	}
	if g.PRs[0].Issues[0] != "#1 it\n\nib" || g.PRs[0].Issues[1] != "#3 jt" {
		t.Errorf("issues %q", g.PRs[0].Issues)
	}
	wantBytes := len("two") + len("bb") + len("no") + len("ok") + len("#1 it\n\nib") + len("#3 jt") + len("one")
	if p.Bytes != wantBytes {
		t.Errorf("bytes %d, want %d", p.Bytes, wantBytes)
	}
}

func TestRunTokenEstimate(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x\n\nCapability: a", "f.go")
	write(t, r.dir, "README.md", strings.Repeat("r", 100))
	f := mergedForge{prs: []forge.PR{{Number: 1, Title: "t", Body: strings.Repeat("b", 50), MergedAt: r.at(1)}}}
	g, s, err := Run(t.Context(), r.dir, r.open(), f, bounds(t), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	total := s.Docs.Bytes + s.History.Bytes + s.PRs.Bytes
	hist := len(g.Changes[0].Message) + len("f.go")
	if s.Docs.Bytes != 100 || s.History.Bytes != hist || s.PRs.Bytes != 51 {
		t.Errorf("bytes %+v", s)
	}
	if s.EstimatedTokens != reading.BytesTokens(total) || s.EstimatedTokens == 0 {
		t.Errorf("tokens %d, bytes %d", s.EstimatedTokens, total)
	}
}

func TestRunDocsSkippedAndHistoryFallback(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	write(t, r.dir, "README.md", strings.Repeat("x", MaxDocBytes+1))
	s, _ := run(t, r, nil, bounds(t))
	if s.Docs.Count != 0 || len(s.Docs.Skipped) != 1 {
		t.Errorf("%+v", s.Docs)
	}
}

func fullSummary() Summary {
	return Summary{
		Version: 2, Store: "/s/physa.db",
		Bound: BoundSummary{Since: "2025-11-21"},
		Docs: DocsSummary{Count: 4, Bytes: 40, Skipped: []Skip{{"BIG.md", "over 2 MB (2097153 bytes)"}}, Files: []DocFile{
			{"AGENTS.md", KindAgent, "an agent instructions file"}, {"docs/records/0001-a.md", KindDecisions, "a decision-record folder"}, {"docs/records/0002-b.md", KindDecisions, "a decision-record folder"}, {"decisions.md", KindDecisions, "a decisions file"},
		}},
		History:         HistorySummary{Changes: 10, WithTrailers: 4, Reverts: 1, WithAltered: 9, DistinctFiles: 30, Bytes: 80},
		PRs:             PRsSummary{Available: true, Count: 3, ReviewComments: 5, RequestingChanges: 2, LinkedIssues: 1, Bytes: 20},
		Discovery:       Discovery{Started: true, Named: 12, Added: 9, Model: "m", Tokens: adapter.Tokens{Input: 30000, Output: 1204}},
		EstimatedTokens: 35,
	}
}

func TestWriteText(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteText(&buf, fullSummary()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	t.Log("\n" + out)
	for _, want := range []string{
		"Gathered into /s/physa.db, replacing the previous gather.\n",
		"Bound: since 2025-11-21 (the last 12 months), no PR limit.\n",
		"Docs: 4 files\n",
		"  AGENTS.md (agent): an agent instructions file\n",
		"  decisions.md (decisions): a decisions file\n",
		"  docs/records/ (2 files)\n",
		"  skipped BIG.md: over 2 MB (2097153 bytes)\n",
		"Discovery: 12 named, 9 added, 31204 tokens (m)\n",
		"History: 10 changes, 4 with trailers, 1 revert, 9 with altered files, 30 distinct files.\n",
		"PRs: 3, 5 review comments (2 requesting changes), 1 linked issue.\n",
		"About 35 tokens to read it all (an estimate: bytes / 2.3).\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "0001-a.md") || strings.Contains(out, "limit was reached") {
		t.Errorf("unexpected: %s", out)
	}
	s := fullSummary()
	s.PRs = PRsSummary{Reason: "no forge"}
	s.Bound.FromFlag, s.Bound.MaxPRs = true, 1000
	buf.Reset()
	if err := WriteText(&buf, s); err != nil || !strings.Contains(buf.String(), "PRs: unavailable (no forge)\n") || !strings.Contains(buf.String(), "(from --since), at most 1000 PRs.\n") {
		t.Errorf("%v\n%s", err, buf.String())
	}
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := WriteJSON(&buf, fullSummary()); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "store", "bound", "docs", "history", "prs", "estimated_tokens", "discovery"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if m["version"].(float64) != 2 || m["estimated_tokens"].(float64) != 35 {
		t.Errorf("%v", m)
	}
	if b := m["bound"].(map[string]any); b["max_prs"].(float64) != 0 || b["since"] != "2025-11-21" {
		t.Errorf("%v", b)
	}
	if d := m["docs"].(map[string]any); d["skipped"].([]any)[0].(map[string]any)["path"] != "BIG.md" || d["skipped"].([]any)[0].(map[string]any)["why"] == "" || d["files"].([]any)[0].(map[string]any)["reason"] != "an agent instructions file" || d["files"].([]any)[0].(map[string]any)["kind"] != "agent" {
		t.Errorf("%v", d)
	}
}

func TestWriteTextSingular(t *testing.T) {
	t.Parallel()
	one := Summary{
		Store: "/s/physa.db", Bound: BoundSummary{Since: "2025-11-21", MaxPRs: 1000},
		Docs:            DocsSummary{Count: 1, Files: []DocFile{{"AGENTS.md", KindAgent, "x"}}},
		History:         HistorySummary{Changes: 1, WithTrailers: 1, Reverts: 1, WithAltered: 1, DistinctFiles: 1},
		PRs:             PRsSummary{Available: true, Count: 1, ReviewComments: 1, RequestingChanges: 1, LinkedIssues: 1},
		EstimatedTokens: 1,
	}
	var buf bytes.Buffer
	if err := WriteText(&buf, one); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Docs: 1 file\n",
		"History: 1 change, 1 with trailers, 1 revert, 1 with altered files, 1 distinct file.\n",
		"PRs: 1, 1 review comment (1 requesting changes), 1 linked issue.\n",
		"About 1 token to read it all",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in %s", want, buf.String())
		}
	}
	buf.Reset()
	two := fullSummary()
	two.Docs.Files = append(two.Docs.Files[:1:1], DocFile{"docs/records/0001-a.md", KindDecisions, "r"}, DocFile{"docs/records/0002-b.md", KindDecisions, "r"})
	two.Docs.Count = 3
	if err := WriteText(&buf, two); err != nil || !strings.Contains(buf.String(), "Docs: 3 files\n") || !strings.Contains(buf.String(), "(2 files)") {
		t.Errorf("%v %s", err, buf.String())
	}
}

func TestWriteTextDiscoveryNotStarted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		d    Discovery
		want string
	}{
		{Discovery{Why: "discovery not started: the repo has a map"}, "Discovery: not started: the repo has a map\n"},
		{Discovery{}, "Discovery: not asked\n"},
		{Discovery{Started: true, Why: "the reply had no sources"}, "0 named, 0 added, 0 tokens; the reply had no sources\n"},
	} {
		var buf bytes.Buffer
		s := fullSummary()
		s.Discovery = tc.d
		if err := WriteText(&buf, s); err != nil || !strings.Contains(buf.String(), tc.want) {
			t.Errorf("%v\n%s", err, buf.String())
		}
	}
}

func TestRunSummaryVersionAndEmptyDiscovery(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	s, _ := run(t, r, nil, bounds(t))
	var buf bytes.Buffer
	if err := WriteJSON(&buf, s); err != nil {
		t.Fatal(err)
	}
	if s.Version != 3 || !strings.Contains(buf.String(), `"skipped": []`) || !strings.Contains(buf.String(), `"discovery"`) {
		t.Errorf("%d %s", s.Version, buf.String())
	}
}

type loginForge struct {
	plainForge
	logins map[string]string
	err    error
}

func (f loginForge) ChangeLogins(_ context.Context, ids []string) (map[string]string, error) {
	return f.logins, f.err
}

func TestUseLoginsNamesChangesByLogin(t *testing.T) {
	t.Parallel()
	changes := []store.GatheredChange{{ID: "aaa", Author: "Dana Lee"}, {ID: "bbb", Author: "Sam"}}
	useLogins(t.Context(), loginForge{logins: map[string]string{"aaa": "dana"}}, changes)
	if changes[0].Author != "dana" || changes[1].Author != "Sam" {
		t.Errorf("authors = %q, %q", changes[0].Author, changes[1].Author)
	}
	useLogins(t.Context(), loginForge{err: errors.New("down")}, changes)
	useLogins(t.Context(), plainForge{}, changes)
	if changes[0].Author != "dana" {
		t.Errorf("a failing or plain forge changed the authors")
	}
}

func TestRunSaysWhenTheCloneIsShallow(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: first", "a.go")
	r.commit("feat: second", "b.go")
	r.commit("feat: third", "a.go")
	if s, _ := run(t, r, nil, bounds(t)); s.History.Shallow {
		t.Fatal("a full clone reads as shallow")
	}
	shallow := &testRepo{t: t, dir: t.TempDir(), n: r.n}
	shallow.git("clone", "-q", "--depth", "1", "file://"+r.dir, ".")
	s, changes := run(t, shallow, nil, bounds(t))
	if !s.History.Shallow || changes != 1 {
		t.Fatalf("shallow %v, %d changes", s.History.Shallow, changes)
	}
	var b strings.Builder
	if err := WriteText(&b, s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "The clone is shallow") || !strings.Contains(b.String(), "git fetch --unshallow") {
		t.Fatalf("text does not say the clone is shallow:\n%s", b.String())
	}
}

// changeForge is mergedForge that knows which PR each change came from.
type changeForge struct {
	mergedForge
	byID map[string]int
}

func (c changeForge) ChangePR(_ context.Context, id string) (forge.PR, bool, error) {
	n, ok := c.byID[id]
	return forge.PR{Number: n}, ok, nil
}

func TestRunGathersPRsForAreasThePRsMiss(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	byID := map[string]int{}
	for i := 1; i <= 3; i++ {
		byID[r.commit(fmt.Sprintf("Keep the ledger %d (#%d)", i, i), "ledger/entry.rb", "ledger/book.rb")] = i
	}
	r.commit("Add notes (#4)", "notes/model.rb")
	r.commit("Edit notes (#5)", "notes/model.rb")
	f := changeForge{mergedForge{more: true, prs: []forge.PR{{Number: 5, MergedAt: r.at(5)}, {Number: 4, MergedAt: r.at(4)}}}, byID}
	g, s, err := Run(t.Context(), r.dir, r.open(), f, bounds(t, func(b *Bounds) { b.MaxPRs = 2 }), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.Quiet == nil || s.Quiet.Clusters != 1 || s.Quiet.PRs != 3 || len(g.PRs) != 5 || len(g.Changes) != 5 || s.PRs.Count != 5 {
		t.Fatalf("quiet %+v, %d PRs, %d changes, summary %+v", s.Quiet, len(g.PRs), len(g.Changes), s.PRs)
	}
	if g.PRs[2].Title != "Keep the ledger 3" {
		t.Errorf("PR %+v", g.PRs[2])
	}
	var buf bytes.Buffer
	if err := WriteText(&buf, s); err != nil || !strings.Contains(buf.String(), "Quiet areas: 1 cluster of files changed before then and not since, with 3 PRs of their newest changes gathered too.") {
		t.Errorf("%v\n%s", err, buf.String())
	}
}

// TestRunTiesPRsLandedAsMergesToTheirFiles checks that in a repo that lands
// PRs as merge commits, whose own changes do not name them, each PR is tied
// to its files by its merge.
func TestRunTiesPRsLandedAsMergesToTheirFiles(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("Start", "README.md")
	for n, area := range map[int]string{7: "ledger", 8: "notes"} {
		r.git("checkout", "-q", "-b", area)
		r.commit("Add the model", area+"/model.rb")
		r.commit("Add the view", area+"/view.rb")
		r.git("checkout", "-q", "main")
		r.git("merge", "-q", "--no-ff", "-m", fmt.Sprintf("Merge pull request #%d from dana/%s", n, area), area)
	}
	f := mergedForge{prs: []forge.PR{{Number: 8}, {Number: 7}}}
	g, _, err := Run(t.Context(), r.dir, r.open(), f, bounds(t), Discover{}, gatherNow(t))
	if err != nil {
		t.Fatal(err)
	}
	files := map[int][]string{}
	for _, c := range g.Changes {
		if n := coverage.PROf(c); n > 0 {
			files[n] = append(files[n], c.Files...)
		}
	}
	if want := map[int][]string{7: {"ledger/model.rb", "ledger/view.rb"}, 8: {"notes/model.rb", "notes/view.rb"}}; !reflect.DeepEqual(files, want) {
		t.Errorf("PRs' files %v, want %v", files, want)
	}
}

func TestRunCompleteGatherIsNotMarkedStopped(t *testing.T) {
	t.Parallel()
	r := newTestRepo(t)
	r.commit("feat: x", "a")
	f := &pagesForge{answers: []pageAnswer{onePage("", forge.PR{Number: 2, MergedAt: r.at(1)})}}
	g, _, err := Run(t.Context(), r.dir, r.open(), f, bounds(t), Discover{}, gatherNow(t))
	if err != nil || len(g.PRs) != 1 {
		t.Fatalf("err = %v, %d PRs", err, len(g.PRs))
	}
	if g.Run.Stopped || !g.Run.Reached.IsZero() || g.Run.StopReason != "" {
		t.Errorf("a complete gather is marked stopped: %+v", g.Run)
	}
}
