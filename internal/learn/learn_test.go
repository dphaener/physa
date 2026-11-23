package learn

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

var base = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// testRepo is a temporary git repo whose commits are a minute apart.
type testRepo struct {
	t     *testing.T
	dir   string
	n     int
	times map[string]time.Time
}

func newTestRepo(t *testing.T, branch string) *testRepo {
	t.Helper()
	r := &testRepo{t: t, dir: t.TempDir(), times: map[string]time.Time{}}
	r.git("init", "-q", "-b", branch)
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	at := base.Add(time.Duration(r.n) * time.Minute).Format(time.RFC3339)
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

// commit makes the next commit, a minute after the last, and returns its id.
func (r *testRepo) commit(message string) string {
	r.t.Helper()
	r.n++
	r.git("commit", "-q", "--allow-empty", "-m", message)
	id := r.git("rev-parse", "HEAD")
	r.times[id] = base.Add(time.Duration(r.n) * time.Minute)
	return id
}

// revert reverts HEAD as git revert does, and returns the revert's id.
func (r *testRepo) revert() string {
	r.t.Helper()
	r.n++
	r.git("revert", "--no-edit", "HEAD")
	id := r.git("rev-parse", "HEAD")
	r.times[id] = base.Add(time.Duration(r.n) * time.Minute)
	return id
}

func (r *testRepo) open() vcs.Repo {
	r.t.Helper()
	repo, err := git.Open(r.dir)
	if err != nil {
		r.t.Fatal(err)
	}
	return repo
}

// fakeForge answers from fixed PRs and errors and counts its calls.
type fakeForge struct {
	prs    map[string]forge.PR
	err    error            // every call fails with it
	errFor map[string]error // the calls for one change fail with it
	calls  int
}

func (f *fakeForge) Name() string               { return "fake" }
func (f *fakeForge) PRExists(int) (bool, error) { return false, nil }
func (f *fakeForge) ChangePR(id string) (forge.PR, bool, error) {
	f.calls++
	if f.err != nil {
		return forge.PR{}, false, f.err
	}
	if err := f.errFor[id]; err != nil {
		return forge.PR{}, false, err
	}
	pr, ok := f.prs[id]
	return pr, ok, nil
}

func run(t *testing.T, repo vcs.Repo, o Options) Result {
	t.Helper()
	res, err := Run(repo, o)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func ids(refs []ChangeRef) []string {
	out := []string{}
	for _, r := range refs {
		out = append(out, r.Change)
	}
	return out
}

func kinds(us []Unavailable) []string {
	out := []string{}
	for _, u := range us {
		out = append(out, u.Kind)
	}
	return out
}

func TestDefaultRangeTakesTheNewest(t *testing.T) {
	r := newTestRepo(t, "main")
	var all []string
	for i := 0; i < DefaultChanges+10; i++ {
		all = append(all, r.commit(fmt.Sprintf("c%d", i)))
	}
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if res.Changes != DefaultChanges || len(res.Nothing) != DefaultChanges || res.AlreadyLearned != 0 {
		t.Fatalf("changes %d, nothing %d, learned %d", res.Changes, len(res.Nothing), res.AlreadyLearned)
	}
	if res.Nothing[0].Change != all[len(all)-1] || res.Nothing[DefaultChanges-1].Change != all[10] {
		t.Errorf("not the newest %d, newest first", DefaultChanges)
	}
	if want := all[9][:7] + "..main"; !strings.HasPrefix(res.Range, all[9][:7]) || !strings.HasSuffix(res.Range, "..main") {
		t.Errorf("range = %q, want %q", res.Range, want)
	}
	if res.Forge != "fake" {
		t.Errorf("forge = %q", res.Forge)
	}
}

func TestFewerThanTheDefault(t *testing.T) {
	r := newTestRepo(t, "main")
	r.commit("one")
	r.commit("two")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if res.Changes != 2 || res.Range != "main" {
		t.Errorf("changes %d, range %q", res.Changes, res.Range)
	}
}

func TestSince(t *testing.T) {
	r := newTestRepo(t, "main")
	var all []string
	for i := 0; i < 5; i++ {
		all = append(all, r.commit(fmt.Sprintf("c%d", i)))
	}
	res := run(t, r.open(), Options{Since: all[1], Forge: &fakeForge{}})
	if got, want := ids(res.Nothing), []string{all[4], all[3], all[2]}; !reflect.DeepEqual(got, want) {
		t.Errorf("changes = %v, want %v", got, want)
	}
	if res.Range != all[1]+"..main" {
		t.Errorf("range = %q", res.Range)
	}
}

func TestBadSinceIsAnError(t *testing.T) {
	r := newTestRepo(t, "main")
	r.commit("one")
	if _, err := Run(r.open(), Options{Since: "nosuchref"}); err == nil {
		t.Error("want an error for a change that does not exist")
	}
}

func TestNoDefaultBranchIsAnError(t *testing.T) {
	r := newTestRepo(t, "main")
	r.git("checkout", "-q", "-b", "trunk")
	r.commit("one")
	if _, err := Run(r.open(), Options{}); err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Errorf("err = %v, want the default branch cannot be found", err)
	}
}

func TestLearnedBranchDropsLearnedChanges(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	b := r.commit("b")
	c := r.commit("c")
	d := r.commit("d")
	r.git("checkout", "-q", "-b", "physa/learn")
	r.commit(fmt.Sprintf("docs(map): Learn\n\nLearned: %s, %s\nLearned: %s", b[:7], c, d[:6]))
	r.git("checkout", "-q", "main")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if got, want := ids(res.Nothing), []string{d, a}; !reflect.DeepEqual(got, want) {
		t.Errorf("learned from %v, want %v (a 6-character id names nothing)", got, want)
	}
	if res.Changes != 2 || res.AlreadyLearned != 2 {
		t.Errorf("changes %d, already learned %d", res.Changes, res.AlreadyLearned)
	}
}

func TestCorrection(t *testing.T) {
	r := newTestRepo(t, "main")
	fixed := r.commit("fixed after review")
	early := r.commit("committed before the review")
	at := base.Add(time.Hour)
	review := forge.Comment{Author: "owner", At: at, Kind: forge.KindReview,
		Body: "Say change,\nnot ticket. " + strings.Repeat("x", 100)}
	f := &fakeForge{prs: map[string]forge.PR{
		fixed: {Number: 9, Comments: []forge.Comment{review},
			Commits: []forge.Commit{{ID: "aaaaaaa111", At: at.Add(-time.Minute)}, {ID: "bbbbbbb222", At: at.Add(time.Minute)}}},
		early: {Number: 10, Comments: []forge.Comment{review},
			Commits: []forge.Commit{{ID: "ccccccc333", At: at.Add(-time.Minute)}}},
	}}
	res := run(t, r.open(), Options{Forge: f})
	if len(res.Learn) != 1 || res.Learn[0].Change != fixed {
		t.Fatalf("learn = %+v", res.Learn)
	}
	s := res.Learn[0].Signals[0]
	body := []rune("Say change, not ticket. " + strings.Repeat("x", 100))
	want := Signal{Kind: KindCorrection, Rule: RuleReviewThenCommit, Changes: []string{res.Learn[0].Short}, Evidence: []string{
		"review by owner at 2026-11-01T13:00:00Z: " + string(body[:80]),
		"then commit bbbbbbb at 2026-11-01T13:01:00Z",
	}}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("signal = %+v\nwant     %+v", s, want)
	}
	if got := ids(res.Nothing); !reflect.DeepEqual(got, []string{early}) {
		t.Errorf("nothing = %v, want the change committed before its review", got)
	}
}

func TestArguedOptions(t *testing.T) {
	r := newTestRepo(t, "main")
	threeTwo := r.commit("three comments, two people")
	threeOne := r.commit("three comments, one person")
	twoTwo := r.commit("two comments, two people")
	say := func(people ...string) []forge.Comment {
		var cs []forge.Comment
		for i, p := range people {
			cs = append(cs, forge.Comment{Author: p, At: base.Add(time.Duration(i) * time.Hour), Kind: forge.KindDiscussion, Body: "option"})
		}
		return cs
	}
	f := &fakeForge{prs: map[string]forge.PR{
		threeTwo: {Comments: say("zed", "amy", "zed")},
		threeOne: {Comments: say("amy", "amy", "amy")},
		twoTwo:   {Comments: say("amy", "zed")},
	}}
	res := run(t, r.open(), Options{Forge: f})
	if len(res.Learn) != 1 || res.Learn[0].Change != threeTwo {
		t.Fatalf("learn = %+v", res.Learn)
	}
	s := res.Learn[0].Signals[0]
	if s.Kind != KindArguedOptions || s.Rule != RuleLongDiscussion || !reflect.DeepEqual(s.Evidence, []string{"3 comments from amy, zed"}) {
		t.Errorf("signal = %+v", s)
	}
	if got := ids(res.Nothing); !reflect.DeepEqual(got, []string{twoTwo, threeOne}) {
		t.Errorf("nothing = %v", got)
	}
}

func TestRevert(t *testing.T) {
	r := newTestRepo(t, "main")
	if err := os.WriteFile(filepath.Join(r.dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git("add", "f.txt")
	added := r.commit("feat: Add f")
	reverted := r.revert()
	res := run(t, r.open(), Options{})
	if len(res.Learn) != 1 || res.Learn[0].Change != reverted {
		t.Fatalf("learn = %+v", res.Learn)
	}
	s := res.Learn[0].Signals[0]
	if s.Kind != KindRevert || s.Rule != RuleRevertMessage || !reflect.DeepEqual(s.Evidence, []string{"reverts " + added + ": feat: Add f"}) {
		t.Errorf("signal = %+v", s)
	}
	if got := ids(incompleteRefs(res.Incomplete)); !reflect.DeepEqual(got, []string{added}) {
		t.Errorf("incomplete = %v", got)
	}
}

func incompleteRefs(in []Incomplete) []ChangeRef {
	var out []ChangeRef
	for _, i := range in {
		out = append(out, i.ChangeRef)
	}
	return out
}

// served makes one commit per entry serving capability learning; an entry is
// the session and capabilities of a slice record in that commit's window, or
// nil for none. It returns the ids and the records.
func served(t *testing.T, r *testRepo, sessions ...[]string) ([]string, []store.SliceRecord) {
	t.Helper()
	var all []string
	var recs []store.SliceRecord
	for i, s := range sessions {
		id := r.commit(fmt.Sprintf("feat: Serve %d\n\nCapability: learning", i))
		all = append(all, id)
		if s != nil {
			recs = append(recs, store.SliceRecord{At: r.times[id].Add(-30 * time.Second), Session: s[0], Capabilities: s[1:]})
		}
	}
	return all, recs
}

func TestServedNotLoaded(t *testing.T) {
	r := newTestRepo(t, "main")
	all, recs := served(t, r, []string{"s1", "map"}, []string{"s2", "map"}, []string{"", "slices"})
	other := r.commit("chore: Unrelated")
	res := run(t, r.open(), Options{Forge: &fakeForge{}, Records: recs, Capabilities: []string{"map", "learning"}})
	if len(res.Learn) != 3 || res.RecordsMissing != 1 {
		t.Fatalf("learn %d, records missing %d", len(res.Learn), res.RecordsMissing)
	}
	s := res.Learn[0].Signals[0]
	if s.Kind != KindServedNotLoaded || s.Capability != "learning" || len(s.Changes) != 3 {
		t.Fatalf("signal = %+v", s)
	}
	if s.Evidence[0] != "served by 3 changes whose sessions never loaded it" || len(s.Evidence) != 4 ||
		s.Evidence[3] != all[0][:len(res.Learn[2].Short)]+" feat: Serve 0" {
		t.Errorf("evidence = %q", s.Evidence)
	}
	if got := ids(res.Nothing); !reflect.DeepEqual(got, []string{other}) {
		t.Errorf("nothing = %v", got)
	}
}

func TestServedNotLoadedNeedsEnoughChanges(t *testing.T) {
	r := newTestRepo(t, "main")
	_, recs := served(t, r, []string{"s1", "map"}, []string{"s2", "map"}, nil)
	res := run(t, r.open(), Options{Forge: &fakeForge{}, Records: recs, Capabilities: []string{"learning"}})
	if len(res.Learn) != 0 || res.RecordsMissing != 1 {
		t.Errorf("learn %+v, records missing %d: a change with no record must not count", res.Learn, res.RecordsMissing)
	}
}

func TestServedNotLoadedSessionLoadedEarlier(t *testing.T) {
	r := newTestRepo(t, "main")
	_, recs := served(t, r, []string{"s", "learning"}, []string{"s", "map"}, []string{"t", "map"}, []string{"u", "map"})
	res := run(t, r.open(), Options{Forge: &fakeForge{}, Records: recs, Capabilities: []string{"learning"}})
	if len(res.Learn) != 0 {
		t.Errorf("learn = %+v: session s loaded learning in an earlier window", res.Learn)
	}
}

func TestRecordsUnavailable(t *testing.T) {
	r := newTestRepo(t, "main")
	served(t, r, nil, nil, nil)
	res := run(t, r.open(), Options{Forge: &fakeForge{}, RecordsReason: "the store cannot be opened", Capabilities: []string{"learning"}})
	if got := kinds(res.Unavailable); !reflect.DeepEqual(got, []string{KindServedNotLoaded, KindRecurringFinding, KindAnsweredAsk, KindIntervention, KindFixedFinding}) {
		t.Errorf("unavailable = %v", got)
	}
	if res.RecordsMissing != 0 || len(res.Nothing) != 3 {
		t.Errorf("records missing %d, nothing %d", res.RecordsMissing, len(res.Nothing))
	}
}

func TestEngineKindsAlwaysUnavailable(t *testing.T) {
	r := newTestRepo(t, "main")
	r.commit("one")
	res := run(t, r.open(), Options{Forge: &fakeForge{}, Sessions: &fakeSessions{}})
	want := []Unavailable{{KindRecurringFinding, reasonRecurringFinding}}
	if !reflect.DeepEqual(res.Unavailable, want) {
		t.Errorf("unavailable = %+v", res.Unavailable)
	}
	if !strings.HasPrefix(res.Unavailable[0].Reason, "needs the engine: ") {
		t.Errorf("reason %q", res.Unavailable[0].Reason)
	}
}

func TestNoSessionReaderListsSessionKindsNotChecked(t *testing.T) {
	r := newTestRepo(t, "main")
	r.commit("one")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	want := []Unavailable{
		{KindRecurringFinding, reasonRecurringFinding},
		{KindAnsweredAsk, "no session reader"},
		{KindIntervention, "no session reader"},
		{KindFixedFinding, "no session reader"},
	}
	if !reflect.DeepEqual(res.Unavailable, want) {
		t.Errorf("unavailable = %+v", res.Unavailable)
	}
	if res.SessionsRead != 0 || len(res.SessionsUnavailable) != 0 || res.SessionsUnavailable == nil {
		t.Errorf("sessions read %d, unavailable %#v", res.SessionsRead, res.SessionsUnavailable)
	}
	if len(res.Nothing) != 1 {
		t.Errorf("nothing = %+v: no session reader leaves the change triaged", res.Nothing)
	}
}

// sessionCall is one call a fakeSessions received.
type sessionCall struct {
	change   string
	recorded []string
	after    time.Time
}

// fakeSessions answers from fixed signals, ids read and gaps per change id,
// and records its calls.
type fakeSessions struct {
	signals map[string][]Signal
	read    map[string][]string
	gaps    map[string][]SessionGap
	calls   []sessionCall
}

func (f *fakeSessions) Signals(c vcs.Change, recorded []string, after time.Time) ([]Signal, []string, []SessionGap) {
	f.calls = append(f.calls, sessionCall{c.ID, recorded, after})
	return f.signals[c.ID], f.read[c.ID], f.gaps[c.ID]
}

func TestSessionsReceiveRecordedSessionsAndPreviousTime(t *testing.T) {
	r := newTestRepo(t, "main")
	first := r.commit("first")
	a := r.commit("a")
	b := r.commit("b")
	c := r.commit("c")
	at := func(id string, d time.Duration) time.Time { return r.times[id].Add(d) }
	recs := []store.SliceRecord{
		{At: at(a, -30*time.Second), Session: "s2", Capabilities: []string{"map"}},
		{At: at(a, -20*time.Second), Session: "s1", Capabilities: []string{"map"}},
		{At: at(a, -10*time.Second), Session: "s2", Capabilities: []string{"map"}},
		{At: at(a, 0), Capabilities: []string{"map"}},
		{At: at(c, -10*time.Second), Session: "s3", Capabilities: []string{"map"}},
	}
	f := &fakeSessions{}
	run(t, r.open(), Options{Forge: &fakeForge{}, Since: first, Records: recs, Sessions: f})
	want := []sessionCall{
		{c, []string{"s3"}, r.times[b]},
		{b, nil, r.times[a]},
		{a, []string{"s1", "s2"}, r.times[first]},
	}
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %+v", f.calls)
	}
	for i, w := range want {
		got := f.calls[i]
		if got.change != w.change || !reflect.DeepEqual(got.recorded, w.recorded) || !got.after.Equal(w.after) {
			t.Errorf("call %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestSessionSignalsMakeTheChangeLearned(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	b := r.commit("b")
	sig := Signal{Kind: KindIntervention, Rule: RuleMidRun, Changes: []string{b[:7]},
		Evidence: []string{"1 message mid-run in session 0a1b2c3d"}, Session: "0a1b2c3d-0000-4000-8000-000000000000",
		MatchedBy: MatchedByTrailer, Quotes: []Quote{{Author: "message in the session", At: r.times[b], Text: "not like that"}}}
	f := &fakeSessions{
		signals: map[string][]Signal{b: {sig}},
		read:    map[string][]string{a: {"s-other", "0a1b2c3d-0000-4000-8000-000000000000"}, b: {"0a1b2c3d-0000-4000-8000-000000000000"}},
	}
	res := run(t, r.open(), Options{Forge: &fakeForge{}, Sessions: f})
	if len(res.Learn) != 1 || res.Learn[0].Change != b {
		t.Fatalf("learn = %+v", res.Learn)
	}
	got := res.Learn[0]
	if !reflect.DeepEqual(got.Signals, []Signal{sig}) {
		t.Errorf("signals %+v", got.Signals)
	}
	if !reflect.DeepEqual(res.LearnedNow, []string{got.Short}) || !reflect.DeepEqual(ids(res.Nothing), []string{a}) {
		t.Errorf("learned now %v, nothing %v", res.LearnedNow, ids(res.Nothing))
	}
	if res.SessionsRead != 2 {
		t.Errorf("sessions read = %d, want 2: a session behind two changes counts once", res.SessionsRead)
	}
	if res.Through != b {
		t.Errorf("through = %s, want %s", res.Through, b)
	}
}

func TestSessionGapDoesNotHoldTheWatermarkBack(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	b := r.commit("b")
	f := &fakeSessions{gaps: map[string][]SessionGap{
		b: {{Session: "0a1b2c3d-0000-4000-8000-000000000000", Reason: "no log on this machine"}},
	}}
	res := run(t, r.open(), Options{Forge: &fakeForge{}, Sessions: f})
	want := []SessionGap{{Change: b[:7], Session: "0a1b2c3d-0000-4000-8000-000000000000", Reason: "no log on this machine"}}
	if !reflect.DeepEqual(res.SessionsUnavailable, want) {
		t.Errorf("sessions unavailable = %+v", res.SessionsUnavailable)
	}
	if len(res.Incomplete) != 0 || !reflect.DeepEqual(ids(res.Nothing), []string{b, a}) {
		t.Errorf("incomplete %+v, nothing %v: a gap must not make a change incomplete", res.Incomplete, ids(res.Nothing))
	}
	if res.Through != b {
		t.Errorf("through = %s, want %s: the watermark advances past a change whose log is unavailable", res.Through, b)
	}
	if len(res.Unavailable) != 1 {
		t.Errorf("unavailable = %+v: a gap does not make a kind not checked", res.Unavailable)
	}
}

func TestResultVersionTwoWithoutQuotes(t *testing.T) {
	r := newTestRepo(t, "main")
	b := r.commit("b")
	f := &fakeSessions{signals: map[string][]Signal{b: {{Kind: KindAnsweredAsk, Rule: RulePersonAnswered,
		Changes: []string{b[:7]}, Evidence: []string{"1 answered ask(s) in session 0a1b2c3d"}, Session: "0a1b2c3d", MatchedBy: MatchedByTime,
		Quotes: []Quote{{Author: "person", Text: "which-store-QUOTE", Options: []string{"sqlite-QUOTE"}, Answer: "files-QUOTE"}}}}}}
	res := run(t, r.open(), Options{Forge: &fakeForge{}, Sessions: f})
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"version":2`, `"session":"0a1b2c3d"`, `"matched_by":"time"`, `"sessions_read":0`, `"sessions_unavailable":[]`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("json lacks %s: %s", want, out)
		}
	}
	for _, banned := range []string{"QUOTE", `"quotes"`, `"author"`} {
		if strings.Contains(string(out), banned) {
			t.Errorf("json carries %s: %s", banned, out)
		}
	}
}

func TestNoForgeIsNeverNothing(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	b := r.commit("b")
	res := run(t, r.open(), Options{ForgeReason: "gh is not installed"})
	if len(res.Nothing) != 0 || !reflect.DeepEqual(ids(incompleteRefs(res.Incomplete)), []string{b, a}) {
		t.Fatalf("nothing %v, incomplete %+v", res.Nothing, res.Incomplete)
	}
	if res.Incomplete[0].Reason != "correction and argued-options not checked: gh is not installed" {
		t.Errorf("reason = %q", res.Incomplete[0].Reason)
	}
	want := []Unavailable{{KindCorrection, "gh is not installed"}, {KindArguedOptions, "gh is not installed"}}
	if !reflect.DeepEqual(res.Unavailable[:2], want) || res.Forge != "" {
		t.Errorf("unavailable = %+v, forge %q", res.Unavailable, res.Forge)
	}
}

func TestUnauthenticatedForgeStopsBeingAsked(t *testing.T) {
	r := newTestRepo(t, "main")
	for i := 0; i < 3; i++ {
		r.commit(fmt.Sprintf("c%d", i))
	}
	f := &fakeForge{err: &forge.Error{Class: forge.ErrUnauthenticated, Reason: "gh is not authenticated", Detail: "HTTP 401"}}
	res := run(t, r.open(), Options{Forge: f})
	if f.calls != 1 {
		t.Errorf("forge asked %d times, want once", f.calls)
	}
	if len(res.Incomplete) != 3 || len(res.Nothing) != 0 || res.Forge != "" {
		t.Fatalf("incomplete %d, nothing %d, forge %q", len(res.Incomplete), len(res.Nothing), res.Forge)
	}
	if got := res.Unavailable[0]; got != (Unavailable{KindCorrection, "gh is not authenticated"}) || res.Unavailable[1].Kind != KindArguedOptions {
		t.Errorf("unavailable = %+v", res.Unavailable)
	}
}

func TestForgeErrorOnOneChange(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	b := r.commit("b")
	f := &fakeForge{errFor: map[string]error{b: errors.New("HTTP 502")}}
	res := run(t, r.open(), Options{Forge: f})
	if f.calls != 2 || res.Forge != "fake" {
		t.Errorf("calls %d, forge %q: one failing change must not turn the forge off", f.calls, res.Forge)
	}
	if !reflect.DeepEqual(ids(res.Nothing), []string{a}) || len(res.Incomplete) != 1 || !strings.Contains(res.Incomplete[0].Reason, "HTTP 502") {
		t.Errorf("nothing %v, incomplete %+v", res.Nothing, res.Incomplete)
	}
}

func TestCorrectionCarriesMessageAndCapability(t *testing.T) {
	r := newTestRepo(t, "main")
	fixed := r.commit("fixed after review\n\nCapability: learning")
	at := base.Add(time.Hour)
	review := forge.Comment{Author: "owner", At: at, Kind: forge.KindReview, Body: "Say change, not ticket."}
	f := &fakeForge{prs: map[string]forge.PR{
		fixed: {Number: 9, Comments: []forge.Comment{review},
			Commits: []forge.Commit{{ID: "bbbbbbb222", At: at.Add(time.Minute)}}},
	}}
	res := run(t, r.open(), Options{Forge: f})
	if len(res.Learn) != 1 {
		t.Fatalf("learn = %+v", res.Learn)
	}
	got := res.Learn[0]
	if msg := strings.TrimSpace(got.Message); msg != "fixed after review\n\nCapability: learning" {
		t.Errorf("message = %q", got.Message)
	}
	if !reflect.DeepEqual(got.Capabilities, []string{"learning"}) {
		t.Errorf("capabilities = %v, want [learning]", got.Capabilities)
	}
}

func TestDraftingTextRidesOnTheSignals(t *testing.T) {
	r := newTestRepo(t, "main")
	if err := os.WriteFile(filepath.Join(r.dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git("add", "f.txt")
	added := r.commit("feat: Add f\n\nWhy f exists.")
	reverted := r.revert()
	argued := r.commit("feat: Argue")
	cs := []forge.Comment{
		{Author: "zed", At: base.Add(2 * time.Hour), Kind: forge.KindDiscussion, Body: "later"},
		{Author: "amy", At: base.Add(time.Hour), Kind: forge.KindDiscussion, Body: "first"},
		{Author: "zed", At: base.Add(3 * time.Hour), Kind: forge.KindDiscussion, Body: "last"},
	}
	f := &fakeForge{prs: map[string]forge.PR{argued: {Comments: cs}}}
	// The range starts after the reverted change: it is outside it.
	res := run(t, r.open(), Options{Forge: f, Since: added})
	var gotArgued, gotRevert *ChangeSignals
	for i := range res.Learn {
		switch res.Learn[i].Change {
		case argued:
			gotArgued = &res.Learn[i]
		case reverted:
			gotRevert = &res.Learn[i]
		}
	}
	if gotArgued == nil || gotRevert == nil {
		t.Fatalf("learn = %+v", res.Learn)
	}
	if gotArgued.At.IsZero() || !gotArgued.At.Equal(r.times[argued]) {
		t.Errorf("at = %v, want %v", gotArgued.At, r.times[argued])
	}
	var authors []string
	for _, c := range gotArgued.Signals[0].Comments {
		authors = append(authors, c.Author+":"+c.Body)
	}
	if want := []string{"amy:first", "zed:later", "zed:last"}; !reflect.DeepEqual(authors, want) {
		t.Errorf("comments = %v, want oldest first %v", authors, want)
	}
	rv := gotRevert.Signals[0].Reverted
	if rv == nil || rv.Subject != "feat: Add f" || !strings.Contains(rv.Message, "Why f exists.") {
		t.Errorf("reverted = %+v", rv)
	}
}

// onLearn makes a commit on physa/learn, branching it from main's HEAD when
// it does not exist yet, and returns to main.
func (r *testRepo) onLearn(message string) string {
	r.t.Helper()
	if _, err := exec.Command("git", "-C", r.dir, "rev-parse", "--verify", "--quiet", "refs/heads/physa/learn").Output(); err != nil {
		r.git("checkout", "-q", "-b", "physa/learn")
	} else {
		r.git("checkout", "-q", "physa/learn")
	}
	id := r.commit(message)
	r.git("checkout", "-q", "main")
	return id
}

// record is a learning commit's message learning through id.
func record(id string, learned ...string) string {
	m := "docs(map): Learn from changes\n\n- a proposal\n\nLearned-through: " + id
	if len(learned) > 0 {
		m += "\nLearned: " + strings.Join(learned, ", ")
	}
	return m + "\nCapability: learning"
}

func TestWatermarkOnLearnBranchLimitsCollection(t *testing.T) {
	r := newTestRepo(t, "main")
	r.commit("a")
	b := r.commit("b")
	r.onLearn(record(b))
	c := r.commit("c")
	d := r.commit("d")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if got, want := ids(res.Nothing), []string{d, c}; !reflect.DeepEqual(got, want) {
		t.Errorf("collected %v, want %v (only the changes after the watermark)", got, want)
	}
	if res.Watermark != b || res.Through != d {
		t.Errorf("watermark %q through %q, want %q and %q", res.Watermark, res.Through, b, d)
	}
	if res.Range != b[:7]+"..main" {
		t.Errorf("range = %q", res.Range)
	}
	if res.AlreadyLearned != 0 || res.Changes != 2 {
		t.Errorf("changes %d, already learned %d", res.Changes, res.AlreadyLearned)
	}
}

func TestWatermarkFromSquashMerge(t *testing.T) {
	r := newTestRepo(t, "main")
	r.commit("a")
	b := r.commit("b")
	c := r.commit("c")
	sq := r.commit("docs(learning): Proposals learned from merged changes (#5)\n\n" + record(b, c[:7]))
	d := r.commit("d")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if res.Watermark != b {
		t.Fatalf("watermark %q, want %q from the squash merge", res.Watermark, b)
	}
	if got, want := ids(res.Nothing), []string{d}; !reflect.DeepEqual(got, want) {
		t.Errorf("nothing %v, want %v: the squash merge is a record, c is learned", got, want)
	}
	if res.AlreadyLearned != 1 || res.Changes != 1 {
		t.Errorf("changes %d, already learned %d, want 1 and 1", res.Changes, res.AlreadyLearned)
	}
	for _, n := range res.Nothing {
		if n.Change == sq {
			t.Error("the learning commit was collected")
		}
	}
	if res.Through != d {
		t.Errorf("through %q, want %q", res.Through, d)
	}
}

func TestWatermarkRecordOnlyStaysPut(t *testing.T) {
	r := newTestRepo(t, "main")
	b := r.commit("b")
	r.commit("docs(learning): Learn (#6)\n\n" + record(b))
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if res.Changes != 0 || res.AlreadyLearned != 0 || len(res.Nothing) != 0 {
		t.Errorf("a learning commit was counted: %+v", res)
	}
	if res.Through != b || res.Watermark != b {
		t.Errorf("watermark %q through %q, want both %q", res.Watermark, res.Through, b)
	}
}

func TestWatermarkLaterWins(t *testing.T) {
	t.Run("default branch later", func(t *testing.T) {
		r := newTestRepo(t, "main")
		a := r.commit("a")
		r.onLearn(record(a))
		c := r.commit("c")
		r.commit("docs(learning): Learn (#7)\n\n" + record(c))
		e := r.commit("e")
		res := run(t, r.open(), Options{Forge: &fakeForge{}})
		if res.Watermark != c || !reflect.DeepEqual(ids(res.Nothing), []string{e}) {
			t.Errorf("watermark %q, collected %v, want %q and [e]", res.Watermark, ids(res.Nothing), c)
		}
	})
	t.Run("learn branch later", func(t *testing.T) {
		r := newTestRepo(t, "main")
		a := r.commit("a")
		r.commit("docs(learning): Learn (#7)\n\n" + record(a))
		c := r.commit("c")
		r.onLearn(record(c))
		e := r.commit("e")
		res := run(t, r.open(), Options{Forge: &fakeForge{}})
		if res.Watermark != c || !reflect.DeepEqual(ids(res.Nothing), []string{e}) {
			t.Errorf("watermark %q, collected %v, want %q and [e]", res.Watermark, ids(res.Nothing), c)
		}
	})
}

func TestWatermarkUnreachableIsIgnored(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	r.git("checkout", "-q", "-b", "side")
	side := r.commit("side")
	r.git("checkout", "-q", "main")
	b := r.commit("b")
	r.onLearn(record(side))
	r.commit("docs(learning): Learn (#8)\n\n" + record("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"))
	res, err := Run(r.open(), Options{Forge: &fakeForge{}})
	if err != nil {
		t.Fatalf("an unreachable watermark is an error: %v", err)
	}
	if res.Watermark != "" || res.Range != "main" {
		t.Errorf("watermark %q, range %q, want none and the newest changes", res.Watermark, res.Range)
	}
	if got, want := ids(res.Nothing), []string{b, a}; !reflect.DeepEqual(got, want) {
		t.Errorf("collected %v, want %v", got, want)
	}
}

func TestWatermarkHoldsBackWithoutForge(t *testing.T) {
	t.Run("stops below the oldest not fully triaged", func(t *testing.T) {
		r := newTestRepo(t, "main")
		if err := os.WriteFile(filepath.Join(r.dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r.git("add", "f.txt")
		f := r.commit("feat: Add f")
		w := r.commit("w")
		r.onLearn(record(w))
		r.n++
		r.git("revert", "--no-edit", f)
		rev := r.git("rev-parse", "HEAD")
		x := r.commit("x")
		r.commit("y")
		res := run(t, r.open(), Options{ForgeReason: "gh is not installed"})
		if len(res.Learn) != 1 || res.Learn[0].Change != rev || len(res.Incomplete) != 2 {
			t.Fatalf("learn %v, incomplete %v", res.Learn, res.Incomplete)
		}
		if res.Through != rev {
			t.Errorf("through %q, want %q, below the not fully triaged %q", res.Through, rev, x)
		}
		if !reflect.DeepEqual(res.LearnedNow, []string{res.Learn[0].Short}) {
			t.Errorf("learned now %v", res.LearnedNow)
		}
	})
	t.Run("stays put when the oldest is not fully triaged", func(t *testing.T) {
		r := newTestRepo(t, "main")
		w := r.commit("w")
		r.onLearn(record(w))
		r.commit("x")
		r.commit("y")
		res := run(t, r.open(), Options{ForgeReason: "gh is not installed"})
		if res.Through != w || res.Watermark != w {
			t.Errorf("watermark %q through %q, want both %q", res.Watermark, res.Through, w)
		}
	})
	t.Run("the change the run started after when nothing is triaged", func(t *testing.T) {
		r := newTestRepo(t, "main")
		a := r.commit("a")
		r.commit("x")
		res := run(t, r.open(), Options{Since: a, ForgeReason: "gh is not installed"})
		if res.Through != a || res.Watermark != "" {
			t.Errorf("watermark %q through %q, want through the base %q", res.Watermark, res.Through, a)
		}
	})
	t.Run("a none record gives no watermark", func(t *testing.T) {
		r := newTestRepo(t, "main")
		r.commit("x")
		r.onLearn("docs(map): Learn from 1 change\n\n- a proposal\n\nLearned-through: none")
		res := run(t, r.open(), Options{ForgeReason: "gh is not installed"})
		if res.Watermark != "" || res.Through != "" {
			t.Errorf("watermark %q through %q, want none", res.Watermark, res.Through)
		}
	})
	t.Run("none when there is no watermark", func(t *testing.T) {
		r := newTestRepo(t, "main")
		r.commit("x")
		res := run(t, r.open(), Options{ForgeReason: "gh is not installed"})
		if res.Through != "" || res.Watermark != "" {
			t.Errorf("watermark %q through %q, want none", res.Watermark, res.Through)
		}
	})
}

func TestWatermarkNeverMovesBack(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	r.commit("b")
	c := r.commit("c")
	r.onLearn(record(c))
	f := &fakeForge{errFor: map[string]error{c: errors.New("boom")}}
	res := run(t, r.open(), Options{Since: a, Forge: f})
	if len(res.Incomplete) != 1 || res.Incomplete[0].Change != c {
		t.Fatalf("incomplete %v, want c", res.Incomplete)
	}
	if res.Through != c {
		t.Errorf("through %q, want the read watermark %q, not behind it", res.Through, c)
	}
}

func TestLearnedOnUnmergedLearnCommit(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	b := r.commit("b")
	r.onLearn(record(a))
	r.onLearn("docs(learning): Learn (no watermark)\n\nLearned: " + b[:8])
	c := r.commit("c")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if got, want := ids(res.Nothing), []string{c}; !reflect.DeepEqual(got, want) {
		t.Errorf("learned from %v, want %v", got, want)
	}
	if res.AlreadyLearned != 1 || res.Through != c {
		t.Errorf("already learned %d through %q", res.AlreadyLearned, res.Through)
	}
}

func TestWatermarkReadFromLearnBranch(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	r.onLearn(record(a))
	b := r.commit("b")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if res.Watermark != a || res.Through != b || len(res.LearnedNow) != 0 {
		t.Errorf("watermark %q through %q learned now %v", res.Watermark, res.Through, res.LearnedNow)
	}
}

func TestWatermarkOnRemoteLearnBranch(t *testing.T) {
	r := newTestRepo(t, "main")
	a := r.commit("a")
	l := r.onLearn(record(a))
	r.git("update-ref", "refs/remotes/origin/physa/learn", l)
	r.git("branch", "-q", "-D", "physa/learn")
	b := r.commit("b")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	if res.Watermark != a || !reflect.DeepEqual(ids(res.Nothing), []string{b}) {
		t.Errorf("watermark %q, collected %v, want %q and [b]", res.Watermark, ids(res.Nothing), a)
	}
}
