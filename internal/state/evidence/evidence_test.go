package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	observe "github.com/dphaener/physa/internal/evidence"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/signal"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
	"github.com/dphaener/physa/internal/store"
)

var bg = context.Background()

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeForge lists canned merged PRs, or fails with err, and counts its calls.
type fakeForge struct {
	mu    sync.Mutex
	prs   []forge.PR
	err   error
	calls int
}

func (f *fakeForge) Name() string                                { return "fake" }
func (f *fakeForge) PRExists(context.Context, int) (bool, error) { return false, nil }
func (f *fakeForge) ChangePR(context.Context, string) (forge.PR, bool, error) {
	return forge.PR{}, false, nil
}
func (f *fakeForge) set(prs []forge.PR, err error) {
	f.mu.Lock()
	f.prs, f.err = prs, err
	f.mu.Unlock()
}
func (f *fakeForge) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func (f *fakeForge) MergedPRs(_ context.Context, since time.Time, max int) ([]forge.PR, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.prs, false, f.err
}

type env struct {
	t    *testing.T
	dir  string // the repo root
	c    *clock
	ff   *fakeForge
	st   *store.Store
	s    *state.State
	file int
}

// newEnv makes a repo with its store at .git/physa and opens the engine's
// state on it with the evidence kinds; ff nil is a repo with no forge.
func newEnv(t *testing.T, ff *fakeForge) *env {
	t.Helper()
	e := &env{t: t, dir: t.TempDir(), ff: ff, c: &clock{t: time.Now().UTC().Truncate(time.Second).Add(time.Minute)}}
	e.git("init", "-q", "-b", "main")
	e.open()
	t.Cleanup(e.close)
	return e
}

func (e *env) open() {
	e.t.Helper()
	dir := filepath.Join(e.dir, ".git", "physa")
	st, err := store.Open(context.Background(), dir)
	if err != nil {
		e.t.Fatal(err)
	}
	newForge := func(string) forge.Forge { return nil }
	if e.ff != nil {
		newForge = func(string) forge.Forge { return e.ff }
	}
	timers, intents := Options(newForge)
	s, err := state.Open(st, state.Options{StoreDir: dir, Now: e.c.Now, Timers: timers, Intents: intents})
	if err != nil {
		e.t.Fatal(err)
	}
	e.st, e.s = st, s
	e.settle() // the poll is due at once on open
}

func (e *env) close() {
	if e.s != nil {
		e.s.Close(bg)
		e.st.Close()
		e.s, e.st = nil, nil
	}
}

// settle waits for the loop to fire what is due, then for the polls it
// wrote to be recorded. The loop fires due timers before it takes the next
// command, so the second command is taken only after the fires.
func (e *env) settle() {
	e.t.Helper()
	for range 2 {
		if err := e.s.Submit(bg, func(*state.Tx) error { return nil }); err != nil {
			e.t.Fatal(err)
		}
	}
	for deadline := time.Now().Add(10 * time.Second); e.s.PendingIntents() != 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			e.t.Fatal("timed out waiting for the poll")
		}
	}
}

// poll makes the poll due now and waits for it.
func (e *env) poll() {
	e.t.Helper()
	if err := Wake(bg, e.s); err != nil {
		e.t.Fatal(err)
	}
	e.settle()
}

func (e *env) git(args ...string) string {
	e.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", e.dir,
		"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (e *env) commit(message string) string {
	e.t.Helper()
	e.file++
	if err := os.WriteFile(filepath.Join(e.dir, fmt.Sprintf("f%d", e.file)), []byte("x"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.git("add", "-A")
	e.git("commit", "-q", "-m", message)
	return e.git("rev-parse", "HEAD")
}

func (e *env) pursuit(slug string) {
	e.t.Helper()
	if _, err := work.AddIdea(bg, e.s, work.IdeaIn{Text: slug, Slug: slug, Person: "ana"}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := work.StartPursuit(bg, e.s, work.PursueIn{Slug: slug, Appetite: 1000, Person: "ana", Signal: signal.Signal{Claim: "c", For: "f", Against: "a", Window: "2099-01-01", Resolver: "ana"}}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.st.DB().QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) list(f Filter) []observe.Fact {
	e.t.Helper()
	facts, err := List(bg, e.s, f)
	if err != nil {
		e.t.Fatal(err)
	}
	return facts
}

// polls returns the watermark row's forge failures and next try.
func (e *env) polls() (int, string) {
	e.t.Helper()
	var n int
	var next string
	if err := e.st.DB().QueryRow(`SELECT forge_failures, forge_next_at FROM evidence_polls WHERE id = 1`).Scan(&n, &next); err != nil {
		e.t.Fatal(err)
	}
	return n, next
}

func (e *env) due(kind string) time.Time {
	e.t.Helper()
	var at string
	if err := e.st.DB().QueryRow(`SELECT due_at FROM timers WHERE kind = ?`, kind).Scan(&at); err != nil {
		e.t.Fatal(err)
	}
	d, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func kinds(facts []observe.Fact) map[string]int {
	out := map[string]int{}
	for _, f := range facts {
		out[f.Kind]++
	}
	return out
}

const facts = `SELECT count(*) FROM evidence`

// (a) A poll records the landed changes, a revert and the forge's facts,
// each with its pursuit's idea.
func TestPollRecordsFacts(t *testing.T) {
	ff := &fakeForge{}
	e := newEnv(t, ff)
	e.pursuit("my-pursuit")
	one := e.commit("feat: One\n\nCapability: ideas\nChange: my-pursuit/one\n")
	two := e.commit("feat: Two\n\nCapability: map\nChange: my-pursuit/two\n")
	e.git("revert", "--no-edit", two)
	now := e.c.Now()
	ff.set([]forge.PR{{Number: 7, CreatedAt: now.Add(-2 * time.Hour), MergedAt: now.Add(-time.Hour), MergeCommit: one,
		Commits: []forge.Commit{{ID: one, Checks: "FAILURE"}, {ID: one, Checks: "SUCCESS"}}}}, nil)
	e.poll()

	all := e.list(Filter{})
	k := kinds(all)
	if k[observe.KindLanded] != 3 || k[observe.KindReverted] != 1 || k[observe.KindLeadTime] != 1 || k[observe.KindCIPassedAfterFails] != 1 || len(all) != 6 {
		t.Fatalf("kinds = %v", k)
	}
	for _, f := range all {
		if f.Pursuit == "my-pursuit" && f.Idea != "my-pursuit" {
			t.Fatalf("a fact of the pursuit has no idea: %+v", f)
		}
		if f.Kind == observe.KindLeadTime && (f.Seconds == nil || *f.Seconds != 3600 || f.PR != 7 || f.Source != observe.SourceForge) {
			t.Fatalf("lead time = %+v", f)
		}
		if f.Kind == observe.KindReverted && (f.Commit != two || f.Capability != "map") {
			t.Fatalf("reverted = %+v", f)
		}
	}
	if n := e.count(`SELECT count(*) FROM events WHERE kind = 'evidence.recorded' AND data ->> 'count' = 6`); n != 1 {
		t.Fatalf("%d evidence.recorded events with count 6, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM evidence_polls WHERE vcs_commit = ?`, e.git("rev-parse", "HEAD")); n != 1 {
		t.Fatal("the watermark is not at the head")
	}
}

// (b) A poll carried out again, as after a kill before its done, records
// nothing twice and moves nothing twice.
func TestRepeatedPollRecordsNothingTwice(t *testing.T) {
	ff := &fakeForge{}
	e := newEnv(t, ff)
	e.commit("feat: One\n\nCapability: ideas\n")
	e.commit("feat: Two\n")
	now := e.c.Now()
	ff.set([]forge.PR{{Number: 1, CreatedAt: now.Add(-time.Hour), MergedAt: now}}, nil)
	e.poll()
	before, events := e.count(facts), e.count(`SELECT count(*) FROM events WHERE kind = 'evidence.recorded'`)
	if before != 3 {
		t.Fatalf("%d facts, want 3", before)
	}
	var id int64
	if err := e.st.DB().QueryRow(`SELECT max(id) FROM intents WHERE kind = ? AND state = 'done'`, PollKind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	e.close()
	// The kill: the poll's facts and watermark are stored, but it is pending.
	st, err := store.Open(context.Background(), filepath.Join(e.dir, ".git", "physa"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.DB().Exec(`UPDATE intents SET state = 'pending', done_at = NULL, result = NULL WHERE id = ?`, id)
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	e.open()
	var state string
	var attempts int
	if err := e.st.DB().QueryRow(`SELECT state, attempts FROM intents WHERE id = ?`, id).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "done" || attempts != 2 {
		t.Fatalf("the repeated poll is %s after %d attempts, want done after 2", state, attempts)
	}
	if n := e.count(facts); n != before {
		t.Fatalf("%d facts after the repeat, want %d", n, before)
	}
	if n := e.count(`SELECT count(*) FROM events WHERE kind = 'evidence.recorded'`); n != events {
		t.Fatalf("%d evidence.recorded events after the repeat, want %d", n, events)
	}
	if n, next := e.polls(); n != 0 || next != "" {
		t.Fatalf("forge failures %d next %q", n, next)
	}
}

// (c) Two polls in a row record only the commits that are new.
func TestPollsRecordNewCommitsOnly(t *testing.T) {
	e := newEnv(t, nil)
	e.commit("feat: One\n")
	e.poll()
	three := e.commit("feat: Three\n\nCapability: ideas\n")
	e.poll()
	all := e.list(Filter{})
	if len(all) != 2 || all[0].Commit != three && all[1].Commit != three {
		t.Fatalf("facts = %+v", all)
	}
	var data string
	if err := e.st.DB().QueryRow(`SELECT data FROM events WHERE kind = 'evidence.recorded' ORDER BY id DESC LIMIT 1`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if data != `{"count":1,"kinds":{"landed":1}}` {
		t.Fatalf("event data = %s", data)
	}
	e.poll()
	if n := e.count(`SELECT count(*) FROM events WHERE kind = 'evidence.recorded'`); n != 2 {
		t.Fatalf("%d evidence.recorded events, want 2: a poll with nothing new appends none", n)
	}
}

// (d) Opening the store again polls at once.
func TestReopenPollsAtOnce(t *testing.T) {
	e := newEnv(t, nil)
	e.commit("feat: One\n")
	e.poll()
	e.close()
	e.commit("feat: Two\n")
	e.open() // waits for what open made due, and nothing else
	if n := e.count(facts); n != 2 {
		t.Fatalf("%d facts after the reopen, want 2", n)
	}
	if n := e.count(`SELECT count(*) FROM timers WHERE kind IN (?, ?)`, PollKind, ExpireKind); n != 2 {
		t.Fatalf("%d evidence timers, want one poll and one retention", n)
	}
}

// (e) The poll comes every 5 minutes while a pursuit is active, daily otherwise.
func TestCadence(t *testing.T) {
	e := newEnv(t, nil)
	if got, want := e.due(PollKind), e.c.Now().Add(24*time.Hour); !got.Equal(want) {
		t.Fatalf("with no pursuit the poll is due %v, want %v", got, want)
	}
	e.pursuit("my-pursuit")
	e.poll()
	if got, want := e.due(PollKind), e.c.Now().Add(5*time.Minute); !got.Equal(want) {
		t.Fatalf("with an active pursuit the poll is due %v, want %v", got, want)
	}
	if _, err := work.StopPursuit(bg, e.s, work.StopIn{Slug: "my-pursuit", Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	e.poll()
	if got, want := e.due(PollKind), e.c.Now().Add(24*time.Hour); !got.Equal(want) {
		t.Fatalf("after the pursuit stopped the poll is due %v, want %v", got, want)
	}
}

// (f) A failing forge backs off, doubling, while version control's facts
// are still recorded; its success resets the backoff.
func TestForgeBacksOff(t *testing.T) {
	ff := &fakeForge{err: forge.ErrUnavailable}
	e := newEnv(t, ff) // the open's poll fails on the forge
	e.commit("feat: One\n")
	start, calls := e.c.Now(), ff.count()
	if n, next := e.polls(); n != 1 || next != ts(start.Add(5*time.Minute)) {
		t.Fatalf("after a failure: failures %d next %q", n, next)
	}
	e.poll() // within the backoff: the forge is not asked
	if ff.count() != calls || e.count(facts) != 1 {
		t.Fatalf("forge calls %d (want %d), facts %d (want 1)", ff.count(), calls, e.count(facts))
	}
	e.c.Add(6 * time.Minute)
	e.poll()
	if n, next := e.polls(); ff.count() != calls+1 || n != 2 || next != ts(e.c.Now().Add(10*time.Minute)) {
		t.Fatalf("after a second failure: calls %d failures %d next %q", ff.count(), n, next)
	}
	ff.set([]forge.PR{{Number: 3, CreatedAt: e.c.Now().Add(-time.Hour), MergedAt: e.c.Now()}}, nil)
	e.c.Add(11 * time.Minute)
	e.poll()
	if n, next := e.polls(); n != 0 || next != "" || kinds(e.list(Filter{}))[observe.KindLeadTime] != 1 {
		t.Fatalf("after a success: failures %d next %q", n, next)
	}
}

// (g) Facts observed more than a year ago, and done polls older than a week,
// expire on the retention timer.
func TestFactsExpire(t *testing.T) {
	e := newEnv(t, nil)
	e.commit("feat: One\n")
	e.poll()
	if e.count(facts) != 1 || e.count(`SELECT count(*) FROM intents WHERE kind = ? AND state = 'done'`, PollKind) != 2 {
		t.Fatal("want one fact and two done polls")
	}
	if got, want := e.due(ExpireKind), e.c.Now().Add(24*time.Hour); got.After(want) {
		t.Fatalf("retention due %v, want by %v", got, want)
	}
	e.c.Add(366 * 24 * time.Hour)
	e.settle()
	if n := e.count(facts); n != 0 {
		t.Fatalf("%d facts left, want 0", n)
	}
	if n := e.count(`SELECT count(*) FROM intents WHERE kind = ? AND state = 'done' AND done_at < ?`, PollKind,
		e.c.Now().Add(-keepPolls).UTC().Format(intentStamp)); n != 0 {
		t.Fatalf("%d old done polls left", n)
	}
	var data string
	if err := e.st.DB().QueryRow(`SELECT data FROM events WHERE kind = 'evidence.expired'`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal([]byte(data), &got); err != nil || got["facts"] != 1 || got["polls"] < 2 {
		t.Fatalf("evidence.expired = %s", data)
	}
	if got, want := e.due(ExpireKind), e.c.Now().Add(24*time.Hour); !got.Equal(want) {
		t.Fatalf("retention due %v after firing, want %v", got, want)
	}
}

// (h) List filters by capability and by pursuit, newest first.
func TestListFilters(t *testing.T) {
	e := newEnv(t, nil)
	e.pursuit("alpha")
	e.commit("feat: A\n\nCapability: ideas\nChange: alpha/a\n")
	e.commit("feat: B\n\nCapability: map\nChange: alpha/b\n")
	e.commit("feat: C\n\nCapability: ideas\n")
	e.poll()
	if got := e.list(Filter{Capability: "ideas"}); len(got) != 2 || got[0].Capability != "ideas" || got[1].Capability != "ideas" {
		t.Fatalf("ideas = %+v", got)
	}
	alpha := e.list(Filter{Pursuit: "alpha"})
	if len(alpha) != 2 || alpha[0].Pursuit != "alpha" || alpha[0].Idea != "alpha" {
		t.Fatalf("alpha = %+v", alpha)
	}
	if got := e.list(Filter{Pursuit: "alpha", Capability: "map"}); len(got) != 1 || got[0].Change != "b" {
		t.Fatalf("alpha and map = %+v", got)
	}
	if all := e.list(Filter{Limit: 2}); len(all) != 2 || all[0].At.Before(all[1].At) {
		t.Fatalf("limit 2 = %+v", all)
	}
}

// Wake does nothing on a state that does not register the poll.
func TestWakeWithoutPoll(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, err := state.Open(st, state.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	if err := Wake(bg, s); err != nil {
		t.Fatalf("Wake = %v, want nil", err)
	}
}
