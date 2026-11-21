package learn

import (
	"bytes"
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
	if got := kinds(res.Unavailable); !reflect.DeepEqual(got, []string{KindServedNotLoaded, KindRecurringFinding, KindAsk}) {
		t.Errorf("unavailable = %v", got)
	}
	if res.RecordsMissing != 0 || len(res.Nothing) != 3 {
		t.Errorf("records missing %d, nothing %d", res.RecordsMissing, len(res.Nothing))
	}
	var b bytes.Buffer
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Slice records: unavailable (the store cannot be opened).\n") {
		t.Errorf("text:\n%s", b.String())
	}
}

func TestEngineKindsAlwaysUnavailable(t *testing.T) {
	r := newTestRepo(t, "main")
	r.commit("one")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	want := []Unavailable{
		{KindRecurringFinding, reasonRecurringFinding},
		{KindAsk, reasonAsk},
	}
	if !reflect.DeepEqual(res.Unavailable, want) {
		t.Errorf("unavailable = %+v", res.Unavailable)
	}
	for _, u := range res.Unavailable {
		if !strings.HasPrefix(u.Reason, "needs the engine: ") {
			t.Errorf("reason %q", u.Reason)
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

func TestWriteText(t *testing.T) {
	r := newTestRepo(t, "main")
	if err := os.WriteFile(filepath.Join(r.dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git("add", "f.txt")
	r.commit("feat: Add f")
	r.revert()
	res := run(t, r.open(), Options{ForgeReason: "gh is not installed"})
	var b bytes.Buffer
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	text := b.String()
	for _, want := range []string{
		"Learning from 2 changes (main), 0 already learned.\n",
		"Forge: unavailable (gh is not installed): correction and argued-options signals not checked.\n",
		"Slice records: 2 of 2 changes had none and were not counted.\n",
		"Not checked: correction (gh is not installed).\n",
		"Not checked: ask (needs the engine: ",
		"\nSignals (1 changes):\n  " + res.Learn[0].Short + " Revert \"feat: Add f\"\n    revert [revert-message]\n      reverts ",
		"\nNothing to learn (0):\n",
		"\nNot fully triaged (1):\n  " + res.Incomplete[0].Short + " feat: Add f (correction and argued-options not checked: gh is not installed)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	res.Forge = "github"
	b.Reset()
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Forge: github\n") {
		t.Errorf("text:\n%s", b.String())
	}
}

func TestWriteJSON(t *testing.T) {
	r := newTestRepo(t, "main")
	r.commit("one")
	res := run(t, r.open(), Options{Forge: &fakeForge{}})
	var b bytes.Buffer
	if err := WriteJSON(&b, res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"version":1`, `"learn":[]`, `"incomplete":[]`, `"already_learned":0`, `"records_missing":1`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("json lacks %s: %s", want, b.String())
		}
	}
	var back Result
	if err := json.Unmarshal(b.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, res) {
		t.Errorf("round trip = %+v\nwant         %+v", back, res)
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

func TestJSONOmitsMessageAndCapabilities(t *testing.T) {
	r := newTestRepo(t, "main")
	fixed := r.commit("fixed after review\n\nCapability: learning")
	at := base.Add(time.Hour)
	review := forge.Comment{Author: "owner", At: at, Kind: forge.KindReview, Body: "Say change, not ticket."}
	f := &fakeForge{prs: map[string]forge.PR{
		fixed: {Number: 9, Comments: []forge.Comment{review},
			Commits: []forge.Commit{{ID: "bbbbbbb222", At: at.Add(time.Minute)}}},
	}}
	res := run(t, r.open(), Options{Forge: f})
	if len(res.Learn) != 1 || res.Learn[0].Message == "" {
		t.Fatalf("learn = %+v", res.Learn)
	}
	var withFields, without bytes.Buffer
	if err := WriteJSON(&withFields, res); err != nil {
		t.Fatal(err)
	}
	stripped := res
	stripped.Learn = []ChangeSignals{{ChangeRef: res.Learn[0].ChangeRef, Signals: res.Learn[0].Signals}}
	if err := WriteJSON(&without, stripped); err != nil {
		t.Fatal(err)
	}
	if withFields.String() != without.String() {
		t.Errorf("json differs with message and capabilities set:\n%s\nwant\n%s", withFields.String(), without.String())
	}
	for _, banned := range []string{`"message"`, `"capabilities"`, "Capability: learning"} {
		if strings.Contains(withFields.String(), banned) {
			t.Errorf("json carries %s: %s", banned, withFields.String())
		}
	}
}
