package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/plan"
	"github.com/dphaener/physa/internal/progress"
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

func (c *clock) tick() {
	c.mu.Lock()
	c.t = c.t.Add(time.Minute)
	c.mu.Unlock()
}

// counting is a Submitter that counts the transactions a call used.
type counting struct {
	s *state.State
	n int
}

func (c *counting) Submit(ctx context.Context, cmd state.Command) error {
	c.n++
	return c.s.Submit(ctx, cmd)
}

type env struct {
	t   *testing.T
	s   *state.State
	st  *store.Store
	c   *clock
	dir string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := &clock{t: time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)}
	s, err := state.Open(st, state.Options{Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(bg) })
	return &env{t, s, st, c, dir}
}

// bet adds an idea and bets on it with the appetite.
func (e *env) bet(slug string, appetite int64) {
	e.t.Helper()
	e.c.tick()
	if _, err := work.AddIdea(bg, e.s, work.IdeaIn{Text: slug, Slug: slug, Person: "ana"}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := work.StartBet(bg, e.s, work.BetIn{Slug: slug, Appetite: appetite, Person: "ana"}); err != nil {
		e.t.Fatal(err)
	}
}

func change(name string, after ...string) plan.Change {
	return plan.Change{Name: name, Capability: "ideas", What: "Do " + name + ".",
		Acceptance: []string{"it works", "failure case: it says why"}, After: after}
}

func plainPlan(names ...string) plan.Plan {
	var p plan.Plan
	for _, n := range names {
		p.Changes = append(p.Changes, change(n))
	}
	return p
}

func (e *env) set(bet, reason string, names ...string) Detail {
	e.t.Helper()
	var d Detail
	e.events("plan.revised", func() (err error) {
		d, err = Set(bg, e.s, SetIn{Bet: bet, Reason: reason, Person: "ana", Plan: plainPlan(names...)})
		return err
	})
	return d
}

// kinds returns the kinds of every event, oldest first.
func (e *env) kinds() []string {
	e.t.Helper()
	evs, err := e.s.Events(bg, 500)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for i := len(evs) - 1; i >= 0; i-- {
		out = append(out, evs[i].Kind)
	}
	return out
}

func (e *env) last() state.Event {
	e.t.Helper()
	evs, err := e.s.Events(bg, 1)
	if err != nil || len(evs) == 0 {
		e.t.Fatal(err)
	}
	return evs[0]
}

// events moves the clock, runs f and checks it appended exactly the kinds.
func (e *env) events(want string, f func() error) {
	e.t.Helper()
	e.c.tick()
	before := len(e.kinds())
	if err := f(); err != nil {
		e.t.Fatalf("%s: %v", want, err)
	}
	got := strings.Join(e.kinds()[before:], " ")
	if got != want {
		e.t.Fatalf("events = %q, want %q", got, want)
	}
}

// counts is the rows in every table these commands write, and the events.
func (e *env) counts() string {
	e.t.Helper()
	var out []string
	for _, table := range []string{"bets", "plans", "plan_revisions", "changes", "session_changes", "spends", "needs_you", "events"} {
		var n int
		if err := e.st.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, table+"="+strconv.Itoa(n))
	}
	var spent int64
	if err := e.st.DB().QueryRow(`SELECT coalesce(sum(spent_tokens), 0) FROM bets`).Scan(&spent); err != nil {
		e.t.Fatal(err)
	}
	return strings.Join(out, " ") + " spent=" + strconv.FormatInt(spent, 10)
}

// refused checks f fails with kind, as a work.Refusal, and writes nothing.
func (e *env) refused(kind error, f func() error) error {
	e.t.Helper()
	before := e.counts()
	err := f()
	if !errors.Is(err, kind) {
		e.t.Fatalf("err = %v, want %v", err, kind)
	}
	if !errors.As(err, new(*work.Refusal)) {
		e.t.Fatalf("err %v is not a Refusal", err)
	}
	if after := e.counts(); after != before {
		e.t.Fatalf("a refused call wrote: before %s, after %s", before, after)
	}
	return err
}

func (e *env) one(query string, args ...any) string {
	e.t.Helper()
	var v sql.NullString
	if err := e.st.DB().QueryRow(query, args...).Scan(&v); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return v.String
}

func (e *env) spend(in SpendIn) SpendOut {
	e.t.Helper()
	e.c.tick()
	out, err := Spend(bg, e.s, in)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func data(t *testing.T, ev state.Event) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSetFirstRevision(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	p := plan.Plan{Changes: []plan.Change{change("flag"), change("docs", "flag")}}
	var d Detail
	e.events("plan.revised", func() (err error) {
		d, err = Set(bg, e.s, SetIn{Bet: "quiet", Person: "ana", Plan: p})
		return err
	})
	if d.Bet != "quiet" || d.N != 1 || d.Replans != 0 || d.Reason != "" || d.Person != "ana" || !d.At.Equal(e.c.Now()) || d.History != nil {
		t.Errorf("detail = %+v", d)
	}
	if len(d.Changes) != 2 || d.Changes[1].Name != "docs" || !slices.Equal(d.Changes[1].After, []string{"flag"}) {
		t.Errorf("changes = %+v", d.Changes)
	}
	ev := e.last()
	if m := data(t, ev); ev.Subject != "plan" || ev.Person != "ana" || m["bet"] != "quiet" || m["n"] != 1.0 || len(m["changes"].([]any)) != 2 {
		t.Errorf("event = %+v %s", ev, ev.Data)
	}
	if got := e.one(`SELECT group_concat(name || ':' || capability || ':' || planned || ':' || state, ' ') FROM changes`); got != "flag:ideas:1:open docs:ideas:1:open" {
		t.Errorf("changes rows = %q", got)
	}
}

func TestSetReasonRequired(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.set("quiet", "", "flag")
	for _, reason := range []string{"", "   "} {
		err := e.refused(ErrNoReason, func() error {
			_, err := Set(bg, e.s, SetIn{Bet: "quiet", Reason: reason, Person: "ana", Plan: plainPlan("flag", "docs")})
			return err
		})
		t.Logf("refused: %v", err)
		if err.Error() != "a revision of the plan needs a reason: what shaping missed" {
			t.Errorf("message = %q", err)
		}
	}
}

func TestSetRevisionsAndReplans(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.set("quiet", "first cut", "flag", "docs")
	e.spend(SpendIn{Bet: "quiet", Change: "docs", Session: "s1", Tokens: 700})
	d := e.set("quiet", "docs belong to the flag", "flag", "help")
	if d.N != 2 || d.Replans != 1 || d.Reason != "docs belong to the flag" || !slices.Equal(plan.Names(plan.Plan{Changes: d.Changes}), []string{"flag", "help"}) {
		t.Errorf("detail = %+v", d)
	}
	if got := e.one(`SELECT planned || ':' || spent_tokens FROM changes WHERE name = 'docs'`); got != "0:700" {
		t.Errorf("dropped change = %q, want kept with planned 0 and its spend", got)
	}
	e.set("quiet", "the flag needed a default", "flag", "help", "docs")
	h, err := Show(bg, e.s, "quiet", true)
	if err != nil {
		t.Fatal(err)
	}
	if h.N != 3 || h.Replans != 2 || len(h.History) != 3 {
		t.Fatalf("history = %+v", h)
	}
	for i, want := range []string{"1:first cut:flag,docs", "2:docs belong to the flag:flag,help", "3:the flag needed a default:flag,help,docs"} {
		r := h.History[i]
		if got := strconv.Itoa(r.N) + ":" + r.Reason + ":" + strings.Join(r.Changes, ","); got != want || r.Person != "ana" {
			t.Errorf("revision %d = %q, want %q", i, got, want)
		}
	}
	if got := e.one(`SELECT planned FROM changes WHERE name = 'docs'`); got != "1" {
		t.Errorf("a change planned again: planned = %q", got)
	}
	f, err := Progress(bg, e.s, "quiet")
	if err != nil {
		t.Fatal(err)
	}
	if f.Replans != 2 || !slices.Equal(f.Planned, []string{"flag", "docs", "help"}) {
		t.Errorf("facts = %+v", f)
	}
}

func TestSetRefusals(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.bet("loud", 100_000)
	if _, err := work.StopBet(bg, e.s, work.StopIn{Slug: "loud", Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		kind error
		in   SetIn
	}{
		{work.ErrUnknownBet, SetIn{Bet: "nope", Person: "ana", Plan: plainPlan("a")}},
		{work.ErrNotActive, SetIn{Bet: "loud", Person: "ana", Plan: plainPlan("a")}},
		{work.ErrNoPerson, SetIn{Bet: "quiet", Person: " ", Plan: plainPlan("a")}},
		{ErrPlanInvalid, SetIn{Bet: "quiet", Person: "ana"}},
		{ErrPlanInvalid, SetIn{Bet: "quiet", Person: "ana", Plan: plainPlan("a", "a")}},
		{ErrPlanInvalid, SetIn{Bet: "quiet", Person: "ana", Plan: plainPlan("Bad Name")}},
		{ErrPlanInvalid, SetIn{Bet: "quiet", Person: "ana", Plan: plan.Plan{Changes: []plan.Change{{Name: "a"}}}}},
	} {
		e.refused(c.kind, func() error { _, err := Set(bg, e.s, c.in); return err })
	}
}

func TestShowNoPlan(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	err := e.refused(ErrNoPlan, func() error { _, err := Show(bg, e.s, "quiet", false); return err })
	if !strings.Contains(err.Error(), "physa bet plan set quiet") {
		t.Errorf("message = %q", err)
	}
	e.refused(work.ErrUnknownBet, func() error { _, err := Show(bg, e.s, "nope", false); return err })
}

func TestWorkRefusals(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.bet("loud", 100_000)
	e.set("quiet", "", "flag", "docs")
	e.set("loud", "", "flag")
	e.set("quiet", "docs went", "flag")
	if _, err := work.StopBet(bg, e.s, work.StopIn{Slug: "loud", Person: "ana"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		kind error
		in   WorkIn
	}{
		{ErrNoSession, WorkIn{Bet: "quiet", Change: "flag"}},
		{ErrNoSession, WorkIn{Session: "a b", Bet: "quiet", Change: "flag"}},
		{ErrNoSession, WorkIn{Session: "a/b", Bet: "quiet", Change: "flag"}},
		{work.ErrUnknownBet, WorkIn{Session: "s1", Bet: "nope", Change: "flag"}},
		{work.ErrNotActive, WorkIn{Session: "s1", Bet: "loud", Change: "flag"}},
		{ErrUnknownChange, WorkIn{Session: "s1", Bet: "quiet", Change: "nope"}},
		{ErrUnknownChange, WorkIn{Session: "s1", Bet: "quiet", Change: "docs"}},
	} {
		e.refused(c.kind, func() error { return Work(bg, e.s, c.in) })
	}
}

func TestWorkReplacesAndCurrent(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.set("quiet", "", "flag", "docs")
	e.events("change.in_work", func() error { return Work(bg, e.s, WorkIn{Session: "sess-1", Bet: "quiet", Change: "flag"}) })
	if m := data(t, e.last()); m["bet"] != "quiet" || m["change"] != "flag" || m["session"] != "sess-1" {
		t.Errorf("event = %v", m)
	}
	e.events("change.in_work", func() error { return Work(bg, e.s, WorkIn{Session: "sess-1", Bet: "quiet", Change: "docs"}) })
	if n := e.one(`SELECT count(*) FROM session_changes`); n != "1" {
		t.Errorf("session_changes = %s, want 1 (replaced)", n)
	}
	db, err := store.OpenReadOnly(filepath.Join(e.dir, "physa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for session, want := range map[string]string{"sess-1": "quiet/docs", "sess-2": "/", "": "/", "a b": "/"} {
		bet, change, err := Current(db, session)
		if err != nil || bet+"/"+change != want {
			t.Errorf("Current(%q) = %q, %q, %v; want %s", session, bet, change, err, want)
		}
	}
	e.set("quiet", "docs went", "flag")
	if bet, change, err := Current(db, "sess-1"); err != nil || bet != "" || change != "" {
		t.Errorf("Current on a dropped change = %q, %q, %v; want none", bet, change, err)
	}
}

func TestCurrentOlderStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "physa.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE bets (id INTEGER PRIMARY KEY, slug TEXT); PRAGMA user_version = 8`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db, err := store.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if bet, change, err := Current(db, "sess-1"); err != nil || bet != "" || change != "" {
		t.Errorf("Current = %q, %q, %v; want none and no error", bet, change, err)
	}
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != 8 {
		t.Errorf("user_version = %d, %v; want 8, untouched", v, err)
	}
}

func TestSpendAddsTheIncrease(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.set("quiet", "", "flag")
	var out SpendOut
	e.events("spend.reported", func() (err error) {
		out, err = Spend(bg, e.s, SpendIn{Bet: "quiet", Change: "flag", Session: "s1", Tokens: 1000})
		return err
	})
	e.events("spend.reported", func() (err error) {
		out, err = Spend(bg, e.s, SpendIn{Bet: "quiet", Change: "flag", Session: "s1", Tokens: 1500})
		return err
	})
	if m := data(t, e.last()); m["tokens"] != 500.0 || m["spent"] != 1500.0 || m["change"] != "flag" || m["session"] != "s1" {
		t.Errorf("event = %v", m)
	}
	e.spend(SpendIn{Bet: "quiet", Session: "s1", Tokens: 200})
	out = e.spend(SpendIn{Bet: "quiet", Change: "flag", Session: "s2", Tokens: 300})
	if out.Spent != 2000 || out.Appetite != 100_000 || out.State != "active" || out.Warned || out.Broke {
		t.Errorf("out = %+v", out)
	}
	if got := e.one(`SELECT spent_tokens FROM changes WHERE name = 'flag'`); got != "1800" {
		t.Errorf("change spent = %s, want 1800", got)
	}
	if got := e.one(`SELECT spent_tokens FROM bets WHERE slug = 'quiet'`); got != "2000" {
		t.Errorf("bet spent = %s, want 2000", got)
	}
}

func TestSpendRepeatChangesNothing(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.set("quiet", "", "flag")
	e.spend(SpendIn{Bet: "quiet", Change: "flag", Session: "s1", Tokens: 5000})
	before := e.counts()
	for _, tokens := range []int64{5000, 4000, 0} {
		out := e.spend(SpendIn{Bet: "quiet", Change: "flag", Session: "s1", Tokens: tokens})
		if out.Spent != 5000 || out.Warned || out.Broke {
			t.Errorf("report of %d: out = %+v", tokens, out)
		}
	}
	if after := e.counts(); after != before {
		t.Errorf("a repeat or lower total wrote: before %s, after %s", before, after)
	}
	t.Logf("after the same total twice and a lower one: %s", e.counts())
}

func TestSpendRefusals(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.set("quiet", "", "flag")
	for _, c := range []struct {
		kind error
		in   SpendIn
	}{
		{ErrBadTokens, SpendIn{Bet: "quiet", Session: "s1", Tokens: -1}},
		{ErrNoSession, SpendIn{Bet: "quiet", Tokens: 1}},
		{ErrNoSession, SpendIn{Bet: "quiet", Session: "s 1", Tokens: 1}},
		{work.ErrUnknownBet, SpendIn{Bet: "nope", Session: "s1", Tokens: 1}},
		{ErrUnknownChange, SpendIn{Bet: "quiet", Change: "nope", Session: "s1", Tokens: 1}},
	} {
		e.refused(c.kind, func() error { _, err := Spend(bg, e.s, c.in); return err })
	}
}

func TestSpendAppetiteNotice(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	if out := e.spend(SpendIn{Bet: "quiet", Session: "s1", Tokens: 79_999}); out.Warned {
		t.Errorf("warned below 80: %+v", out)
	}
	if n := e.one(`SELECT count(*) FROM needs_you`); n != "0" {
		t.Errorf("needs_you = %s below the notice", n)
	}
	c := &counting{s: e.s}
	e.events("spend.reported appetite.warned", func() error {
		out, err := Spend(bg, c, SpendIn{Bet: "quiet", Session: "s1", Tokens: 80_000})
		if err == nil && (!out.Warned || out.Broke || out.State != "active") {
			t.Errorf("out = %+v", out)
		}
		return err
	})
	if c.n != 1 {
		t.Errorf("spend and its check took %d transactions, want 1", c.n)
	}
	if got := e.one(`SELECT person || '|' || kind || '|' || text FROM needs_you`); got != `ana|appetite|the bet "quiet" has spent 80k of its 100k appetite` {
		t.Errorf("needs you = %q", got)
	}
	e.events("spend.reported", func() error {
		out, err := Spend(bg, e.s, SpendIn{Bet: "quiet", Session: "s1", Tokens: 90_000})
		if err == nil && out.Warned {
			t.Errorf("warned twice: %+v", out)
		}
		return err
	})
	if n := e.one(`SELECT count(*) FROM needs_you`); n != "1" {
		t.Errorf("needs_you = %s, want one notice", n)
	}
}

func TestSpendBreaker(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.set("quiet", "", "flag")
	e.spend(SpendIn{Bet: "quiet", Change: "flag", Session: "s1", Tokens: 85_000})
	c := &counting{s: e.s}
	var out SpendOut
	e.events("spend.reported bet.stopped", func() (err error) {
		out, err = Spend(bg, c, SpendIn{Bet: "quiet", Change: "flag", Session: "s1", Tokens: 100_000})
		return err
	})
	if c.n != 1 || !out.Broke || out.Warned || out.State != "stopped" || out.Spent != 100_000 || out.Appetite != 100_000 {
		t.Errorf("out = %+v in %d transactions", out, c.n)
	}
	if m := data(t, e.last()); m["by"] != "circuit breaker" || m["bet"] != "quiet" {
		t.Errorf("bet.stopped = %v", m)
	}
	b, err := work.Bet(bg, e.s, "quiet")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stopped: state %s, reason %q, appetite %d", b.State, b.StopReason, b.Appetite)
	if b.State != "stopped" || b.StopReason != "circuit breaker: spent 100k of 100k tokens" || b.Appetite != 100_000 || b.EndedAt.IsZero() {
		t.Errorf("bet = %+v", b.BetFacts)
	}
	if got := e.one(`SELECT text FROM needs_you WHERE kind = 'breaker'`); got != `the circuit breaker stopped the bet "quiet": spent 100k of 100k tokens; it goes back to shaping` {
		t.Errorf("breaker item = %q", got)
	}
	e.events("spend.reported", func() (err error) {
		out, err = Spend(bg, e.s, SpendIn{Bet: "quiet", Change: "flag", Session: "s2", Tokens: 4000})
		return err
	})
	if out.Spent != 104_000 || out.Warned || out.Broke || out.State != "stopped" {
		t.Errorf("spend on a stopped bet: %+v", out)
	}
	if n := e.one(`SELECT count(*) FROM needs_you`); n != "2" {
		t.Errorf("needs_you = %s, want the notice and the breaker only", n)
	}
	if a := e.one(`SELECT appetite_tokens FROM bets`); a != "100000" {
		t.Errorf("appetite = %s, raised", a)
	}
}

func TestSpendJumpPastAppetite(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 1000)
	var out SpendOut
	e.events("spend.reported appetite.warned bet.stopped", func() (err error) {
		out, err = Spend(bg, e.s, SpendIn{Bet: "quiet", Session: "s1", Tokens: 1500})
		return err
	})
	if !out.Warned || !out.Broke {
		t.Errorf("out = %+v", out)
	}
}

func TestLand(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	e.bet("bare", 100_000)
	if err := Land(bg, e.s, "bare", nil); err != nil {
		t.Fatal(err)
	}
	e.set("quiet", "", "flag", "docs")
	at := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	flag := map[string]progress.Landing{"flag": {Commit: "abc123", Short: "abc", At: at}, "other": {Commit: "fff"}}
	e.events("change.landed", func() error { return Land(bg, e.s, "quiet", flag) })
	if m := data(t, e.last()); m["change"] != "flag" || m["commit"] != "abc123" || m["bet"] != "quiet" {
		t.Errorf("event = %v", m)
	}
	before := e.counts()
	if err := Land(bg, e.s, "quiet", flag); err != nil {
		t.Fatal(err)
	}
	if after := e.counts(); after != before {
		t.Errorf("a repeat wrote: %s then %s", before, after)
	}
	both := map[string]progress.Landing{"flag": {Commit: "zzz", At: at.Add(time.Hour)}, "docs": {Commit: "def456", At: at.Add(time.Hour)}}
	e.events("change.landed bet.done", func() error { return Land(bg, e.s, "quiet", both) })
	b, err := work.Bet(bg, e.s, "quiet")
	if err != nil || b.State != "done" || b.EndedAt.IsZero() {
		t.Errorf("bet = %+v, %v", b.BetFacts, err)
	}
	if got := e.one(`SELECT landed_commit || ' ' || landed_at FROM changes WHERE name = 'flag'`); got != "abc123 2026-11-23T09:00:00Z" {
		t.Errorf("flag kept its first landing: %q", got)
	}
	before = e.counts()
	if err := Land(bg, e.s, "quiet", both); err != nil {
		t.Fatal(err)
	}
	if after := e.counts(); after != before {
		t.Errorf("a repeat after done wrote: %s then %s", before, after)
	}
	if b, _ := work.Bet(bg, e.s, "bare"); b.State != "active" {
		t.Errorf("a bet with no plan = %s, want active", b.State)
	}
	e.refused(work.ErrUnknownBet, func() error { return Land(bg, e.s, "nope", nil) })
}

func TestProgress(t *testing.T) {
	e := newEnv(t)
	e.bet("quiet", 100_000)
	f, err := Progress(bg, e.s, "quiet")
	if err != nil || len(f.Planned) != 0 || f.Replans != 0 || f.Appetite != 100_000 {
		t.Fatalf("no plan: %+v, %v", f, err)
	}
	e.set("quiet", "", "flag", "docs", "help")
	for _, w := range []WorkIn{{"s1", "quiet", "flag"}, {"s2", "quiet", "docs"}} {
		if err := Work(bg, e.s, w); err != nil {
			t.Fatal(err)
		}
	}
	e.spend(SpendIn{Bet: "quiet", Change: "flag", Session: "s1", Tokens: 3000})
	e.spend(SpendIn{Bet: "quiet", Session: "s1", Tokens: 5000})
	e.spend(SpendIn{Bet: "quiet", Change: "docs", Session: "s2", Tokens: 1000})
	e.spend(SpendIn{Bet: "quiet", Change: "flag", Session: "s3", Tokens: 1000})
	at := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	if err := Land(bg, e.s, "quiet", map[string]progress.Landing{"flag": {Commit: "abc", At: at}}); err != nil {
		t.Fatal(err)
	}
	e.spend(SpendIn{Bet: "quiet", Change: "help", Session: "s4", Tokens: 72_000})
	if f, err = Progress(bg, e.s, "quiet"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Planned, []string{"flag", "docs", "help"}) || !slices.Equal(f.InWork, []string{"docs"}) {
		t.Errorf("planned %v, in work %v", f.Planned, f.InWork)
	}
	if len(f.Landed) != 1 || f.Landed[0] != (LandedChange{"flag", "abc", at}) {
		t.Errorf("landed = %+v", f.Landed)
	}
	if f.Spent != 82_000 || f.AsksOpen != 0 || f.Replans != 0 {
		t.Errorf("facts = %+v", f)
	}
	want := []ChangeSpend{{"help", 72_000}, {"", 5000}, {"flag", 4000}, {"docs", 1000}}
	if !slices.Equal(f.PerChange, want) {
		t.Errorf("per change = %+v, want %+v", f.PerChange, want)
	}
	if len(f.Needs) != 1 || f.Needs[0].Kind != "appetite" || !strings.Contains(f.Needs[0].Text, "82k of its 100k") {
		t.Errorf("needs = %+v", f.Needs)
	}
}
