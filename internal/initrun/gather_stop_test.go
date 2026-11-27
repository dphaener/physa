package initrun

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather/merged"
	"github.com/dphaener/physa/internal/initrecord"
)

// pagesFake is a forge that lists merged PRs a page at a time: it replies
// with its answers in order and records the since and cursor of each ask.
type pagesFake struct {
	mergedFake
	answers []pageAnswer
	since   []time.Time
	cursors []string
}

type pageAnswer struct {
	page forge.Page
	err  error
}

func (p *pagesFake) MergedPage(since time.Time, cursor string) (forge.Page, error) {
	p.since, p.cursors = append(p.since, since), append(p.cursors, cursor)
	if len(p.answers) == 0 {
		return forge.Page{}, errors.New("fake forge: no answer left")
	}
	a := p.answers[0]
	p.answers = p.answers[1:]
	return a.page, a.err
}

// recordingFake is a forge that lists merged PRs in one call and records
// what it was asked for.
type recordingFake struct {
	mergedFake
	since time.Time
	max   int
}

func (r *recordingFake) MergedPRs(since time.Time, max int) ([]forge.PR, bool, error) {
	r.since, r.max = since, max
	return []forge.PR{}, false, nil
}

// prsPage is a page of n PRs numbered down from first, each merged a minute
// before the one above it, the newest at newest.
func prsPage(next string, first, n int, newest time.Time) pageAnswer {
	var prs []forge.PR
	for i := range n {
		prs = append(prs, forge.PR{Number: first - i, Title: "A change", MergedAt: newest.Add(-time.Duration(i) * time.Minute)})
	}
	return pageAnswer{page: forge.Page{PRs: prs, Next: next}}
}

// shortWait makes every wait for the forge's rate limit a recorded one that
// moves a fake clock on, never a real sleep, with limit as the most waited in
// total, until the test ends.
func shortWait(t *testing.T, limit time.Duration) *[]time.Duration {
	t.Helper()
	slept, now := new([]time.Duration), time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	merged.Defaults = merged.Wait{
		Limit: limit,
		Sleep: func(d time.Duration) { *slept = append(*slept, d); now = now.Add(d) },
		Now:   func() time.Time { return now },
	}
	t.Cleanup(func() { merged.Defaults = merged.Wait{} })
	return slept
}

// fixedNow gives env a clock that stands still, and returns its time.
func fixedNow(env *Env) time.Time {
	now := time.Now().UTC().Truncate(time.Second)
	env.Now = func() time.Time { return now }
	return now
}

// TestBareGatherAsksTwelveMonthsAndNoPRLimit checks that a bare --gather asks
// the forge for the twelve months before now with no PR limit, and keeps
// every PR of every page, more than the 1,000 it once stopped at.
func TestBareGatherAsksTwelveMonthsAndNoPRLimit(t *testing.T) {
	newRepo(t)
	shortWait(t, time.Minute)
	env := testEnv(t, nil, nil)
	now := fixedNow(&env)
	newest := now.Add(-time.Hour)
	f := &pagesFake{answers: []pageAnswer{
		prsPage("c1", 1500, 500, newest),
		prsPage("c2", 1000, 500, newest.Add(-500*time.Minute)),
		prsPage("", 500, 500, newest.Add(-1000*time.Minute)),
	}}
	env.Forge = func(string) forge.Forge { return f }
	code, _, errOut := runInit(env, "--gather")
	if code != 0 {
		t.Fatalf("code %d, stderr %s", code, errOut)
	}
	if want := []string{"", "c1", "c2"}; !reflect.DeepEqual(f.cursors, want) {
		t.Errorf("cursors %q, want %q", f.cursors, want)
	}
	for _, since := range f.since {
		if want := now.AddDate(-1, 0, 0); !since.Equal(want) {
			t.Errorf("asked since %v, want twelve months before now, %v", since, want)
		}
	}
	if g := storedGather(t, env); len(g.PRs) != 1500 || g.Run.PRsLimited {
		t.Errorf("stored %d PRs (limited: %v), want all 1500", len(g.PRs), g.Run.PRsLimited)
	}

	// A forge that lists in one call is asked with no limit either.
	rec := &recordingFake{}
	env.Forge = func(string) forge.Forge { return rec }
	if code, _, errOut := runInit(env, "--gather"); code != 0 {
		t.Fatalf("code %d, stderr %s", code, errOut)
	}
	if want := now.AddDate(-1, 0, 0); !rec.since.Equal(want) || rec.max != math.MaxInt {
		t.Errorf("asked since %v at most %d, want %v and no limit", rec.since, rec.max, want)
	}
}

// TestGatherMaxPRsStillCaps checks that --max-prs N asks for at most N and
// keeps exactly N.
func TestGatherMaxPRsStillCaps(t *testing.T) {
	newRepo(t)
	shortWait(t, time.Minute)
	env := testEnv(t, nil, nil)
	now := fixedNow(&env)
	rec := &recordingFake{}
	env.Forge = func(string) forge.Forge { return rec }
	if code, _, errOut := runInit(env, "--gather", "--max-prs", "5"); code != 0 {
		t.Fatalf("code %d, stderr %s", code, errOut)
	}
	if want := now.AddDate(-1, 0, 0); !rec.since.Equal(want) || rec.max != 5 {
		t.Errorf("asked since %v at most %d, want %v and 5", rec.since, rec.max, want)
	}

	f := &pagesFake{answers: []pageAnswer{prsPage("c1", 20, 10, now.Add(-time.Hour)), prsPage("", 10, 10, now.Add(-2*time.Hour))}}
	env.Forge = func(string) forge.Forge { return f }
	if code, _, errOut := runInit(env, "--gather", "--max-prs", "5"); code != 0 {
		t.Fatalf("code %d, stderr %s", code, errOut)
	}
	g := storedGather(t, env)
	listed := 0
	for _, pr := range g.PRs {
		if pr.Number > 10 { // the first page's; anything else the quiet top-up added
			listed++
		}
	}
	if listed != 5 || !g.Run.PRsLimited || len(f.cursors) != 1 {
		t.Errorf("kept %d of the newest PRs (limited: %v) over %d pages, want 5, limited, 1 page", listed, g.Run.PRsLimited, len(f.cursors))
	}
}

// stoppingForge lists two pages, three PRs, then keeps answering with a
// secondary rate limit that gives no hint. reached is the oldest PR's merge.
func stoppingForge(now time.Time) (f *pagesFake, reached time.Time) {
	at := func(days int) time.Time { return now.AddDate(0, 0, -days) }
	limit := pageAnswer{err: &forge.RateLimit{Secondary: true, Detail: "API rate limit exceeded"}}
	return &pagesFake{answers: []pageAnswer{
		{page: forge.Page{PRs: []forge.PR{{Number: 5, Title: "Five", MergedAt: at(10)}, {Number: 4, Title: "Four", MergedAt: at(20)}}, Next: "c1"}},
		{page: forge.Page{PRs: []forge.PR{{Number: 3, Title: "Three", MergedAt: at(30)}}, Next: "c2"}},
		limit, limit, limit, limit,
	}}, at(30)
}

// TestGatherStoppedByRateLimitIsStoredAndExitsNonZero checks that a forge
// which keeps refusing past the wait limit leaves the pages fetched in the
// store, says each wait and then the date reached and the rate limit on
// stderr, and makes --gather exit 2.
func TestGatherStoppedByRateLimitIsStoredAndExitsNonZero(t *testing.T) {
	newRepo(t)
	slept := shortWait(t, 3*time.Minute) // 60s and 120s are slept; 240s more is refused
	env := testEnv(t, nil, nil)
	now := fixedNow(&env)
	f, reached := stoppingForge(now)
	env.Forge = func(string) forge.Forge { return f }
	code, out, errOut := runInit(env, "--gather")
	t.Logf("code %d, stderr:\n%s", code, errOut)
	if code != 2 {
		t.Errorf("code %d, want 2", code)
	}
	if out != "" {
		t.Errorf("a stopped gather printed a summary: %q", out)
	}
	if want := []time.Duration{time.Minute, 2 * time.Minute}; !reflect.DeepEqual(*slept, want) {
		t.Errorf("slept %v, want %v", *slept, want)
	}
	if want := []string{"", "c1", "c2", "c2", "c2"}; !reflect.DeepEqual(f.cursors, want) {
		t.Errorf("cursors %q, want %q", f.cursors, want)
	}
	for _, want := range []string{
		"the forge's secondary rate limit: waiting 1m0s, resuming at ",
		"the forge's secondary rate limit: waiting 2m0s, resuming at ",
		"physa init: the forge stopped the gather: still limited after waiting 3m0s in total: secondary rate limit of the forge",
		"the 3 PRs kept reach back to " + reached.UTC().Format(time.DateOnly),
		"of the asked " + now.AddDate(-1, 0, 0).Format(time.DateOnly),
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q", want)
		}
	}
	g := storedGather(t, env)
	if len(g.PRs) != 3 || g.PRs[0].Number != 5 || g.PRs[2].Number != 3 || g.Run.PRsLimited {
		t.Errorf("stored %d PRs (limited: %v), want the 3 fetched", len(g.PRs), g.Run.PRsLimited)
	}
	if len(g.Changes) == 0 {
		t.Error("the stored gather has no history")
	}
}

// TestFullRunStoppedByRateLimitDoesNotPropose checks that the full run
// stores a stopped gather, exits 2 and starts no session to propose.
func TestFullRunStoppedByRateLimitDoesNotPropose(t *testing.T) {
	remoteRepo(t)
	shortWait(t, 3*time.Minute)
	a := newRoutedAgent()
	env := fullEnv(t, nil, a)
	f, _ := stoppingForge(fixedNow(&env))
	env.Forge = func(string) forge.Forge { return f }
	var stdout, stderr bytes.Buffer
	code := runInitTo(&stdout, &stderr, env)
	if code != 2 || !strings.Contains(stderr.String(), "physa init: the forge stopped the gather") || !strings.Contains(stderr.String(), "rate limit") {
		t.Errorf("code %d, stderr %s", code, stderr.String())
	}
	for _, s := range a.started {
		if s != "discover" {
			t.Errorf("a stopped gather went on to %s (sessions %v)", s, a.started)
		}
	}
	if g := storedGather(t, env); len(g.PRs) != 3 {
		t.Errorf("stored %d PRs, want the 3 fetched", len(g.PRs))
	}
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, ok, err := initrecord.Of(st).Proposed(); ok || err != nil {
		t.Errorf("a proposal was stored after a stopped gather: %v %v", ok, err)
	}
}

// TestGatherForgeErrorExitsNonZero checks that a forge's plain error on the
// first page makes --gather exit 2 and say so, never 0 with zero PRs.
func TestGatherForgeErrorExitsNonZero(t *testing.T) {
	newRepo(t)
	shortWait(t, time.Minute)
	env := testEnv(t, &pagesFake{answers: []pageAnswer{{err: errors.New("boom")}}}, nil)
	now := fixedNow(&env)
	code, out, errOut := runInit(env, "--gather")
	t.Logf("code %d, stderr:\n%s", code, errOut)
	want := "physa init: the forge stopped the gather: boom; no PR was listed of the asked " + now.AddDate(-1, 0, 0).Format(time.DateOnly)
	if code != 2 || out != "" || !strings.Contains(errOut, want) {
		t.Errorf("code %d, stdout %q, stderr lacks %q", code, out, want)
	}
	if g := storedGather(t, env); len(g.PRs) != 0 || len(g.Changes) == 0 {
		t.Errorf("stored %d PRs and %d changes, want no PRs and the history", len(g.PRs), len(g.Changes))
	}
}

// TestStoppedGatherThenProposeWarnsAndStillProposes checks acceptance 5: a
// --gather the forge stopped past the wait limit is stored with the mark,
// the date it reached and the reason, and a later --propose and --recover on
// it say so on stderr, still run, and exit as on a complete gather.
func TestStoppedGatherThenProposeWarnsAndStillProposes(t *testing.T) {
	remoteRepo(t)
	shortWait(t, 3*time.Minute)
	a := newRoutedAgent()
	env := fullEnv(t, nil, a)
	now := fixedNow(&env)
	f, reached := stoppingForge(now)
	env.Forge = func(string) forge.Forge { return f }
	if code, _, errOut := runInit(env, "--gather"); code != 2 {
		t.Fatalf("--gather: code %d, want 2\n%s", code, errOut)
	}
	g := storedGather(t, env)
	if !g.Run.Stopped || !g.Run.Reached.Equal(reached) || !strings.Contains(g.Run.StopReason, "secondary rate limit of the forge") {
		t.Errorf("stored mark %+v, want stopped, reaching %v, for the rate limit", g.Run, reached)
	}
	at, asked := reached.UTC().Format(time.DateOnly), now.AddDate(-1, 0, 0).Format(time.DateOnly)
	wants := []string{
		"physa init: warning: the stored gather was stopped short by the forge (still limited after waiting 3m0s in total: secondary rate limit of the forge",
		"its PRs reach back to " + at + " of the asked " + asked,
		"miss the PRs merged before " + at, "physa init --gather again",
	}
	code, out, errOut := runInit(env, "--propose")
	t.Logf("--propose: code %d, stderr:\n%s", code, errOut)
	if code != 0 || out == "" {
		t.Errorf("--propose on a stopped gather: code %d, stdout %q", code, out)
	}
	for _, want := range wants {
		if !strings.Contains(errOut, want) {
			t.Errorf("--propose stderr lacks %q", want)
		}
	}
	st, err := env.Store(".")
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := initrecord.Of(st).Proposed()
	st.Close()
	if !ok || err != nil {
		t.Errorf("no proposal stored after --propose on a stopped gather: %v %v", ok, err)
	}
	code, _, errOut = runInit(env, "--recover")
	if code != 0 || !strings.Contains(errOut, wants[0]) {
		t.Errorf("--recover on a stopped gather: code %d, stderr:\n%s", code, errOut)
	}
	if g := storedGather(t, env); !g.Run.Stopped {
		t.Error("a later phase treated the stopped gather as complete")
	}
}

// TestCompleteGatherThenProposeHasNoStoppedWarning checks that --propose on
// a complete gather says nothing of a stopped one.
func TestCompleteGatherThenProposeHasNoStoppedWarning(t *testing.T) {
	remoteRepo(t)
	env := fullEnv(t, onePR(), newRoutedAgent())
	if code, _, errOut := runInit(env, "--gather"); code != 0 {
		t.Fatalf("--gather: code %d\n%s", code, errOut)
	}
	if g := storedGather(t, env); g.Run.Stopped || g.Run.StopReason != "" {
		t.Errorf("a complete gather is marked stopped: %+v", g.Run)
	}
	code, _, errOut := runInit(env, "--propose")
	if code != 0 || strings.Contains(errOut, "stopped short") {
		t.Errorf("--propose on a complete gather: code %d, stderr:\n%s", code, errOut)
	}
}
