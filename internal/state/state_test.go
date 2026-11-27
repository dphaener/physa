package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/store"
)

// clock is the tests' fake clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)} }

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

var bg = context.Background()

func openStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func openState(t *testing.T, st *store.Store, c *clock, kinds ...TimerKind) *State {
	t.Helper()
	s, err := Open(st, Options{Now: c.Now, Timers: kinds})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(bg) })
	return s
}

func newState(t *testing.T, kinds ...TimerKind) (*State, *store.Store, *clock) {
	t.Helper()
	c := newClock()
	st := openStore(t, t.TempDir())
	return openState(t, st, c, kinds...), st, c
}

// settle returns once the loop has fired every timer that was due when it
// was called: the loop checks timers before it takes each command.
func settle(t *testing.T, s *State) {
	t.Helper()
	s.poke()
	if err := s.Submit(bg, func(*Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func addIdea(slug, text string) Command {
	return func(tx *Tx) error {
		at := tx.Now().UTC().Format(time.RFC3339)
		res, err := tx.Exec(`INSERT INTO ideas (slug, text, person, created_at, updated_at) VALUES (?, ?, 'test', ?, ?)`, slug, text, at, at)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		return tx.Event("idea.added", "idea", id, "test", map[string]string{"slug": slug})
	}
}

func note(kind string) Command {
	return func(tx *Tx) error { return tx.Event(kind, "test", 0, "", nil) }
}

func count(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dueOf(t *testing.T, st *store.Store, kind, key string) time.Time {
	t.Helper()
	var at string
	if err := st.DB().QueryRow(`SELECT due_at FROM timers WHERE kind = ? AND key = ?`, kind, key).Scan(&at); err != nil {
		t.Fatalf("timer %s %s: %v", kind, key, err)
	}
	due, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		t.Fatal(err)
	}
	return due
}

func kinds(t *testing.T, s *State) []string {
	t.Helper()
	evs, err := s.Events(bg, 500)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

func TestCommandAppendsEvent(t *testing.T) {
	s, st, c := newState(t)
	if err := s.Submit(bg, addIdea("one", "first")); err != nil {
		t.Fatal(err)
	}
	if n := count(t, st, `SELECT count(*) FROM ideas WHERE slug = 'one'`); n != 1 {
		t.Fatalf("ideas = %d, want 1", n)
	}
	evs, err := s.Events(bg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	e := evs[0]
	if e.Kind != "idea.added" || e.Subject != "idea" || e.SubjectID != 1 || e.Person != "test" ||
		string(e.Data) != `{"slug":"one"}` || !e.At.Equal(c.Now()) {
		t.Fatalf("event = %+v", e)
	}
}

func TestCommandErrorRollsBack(t *testing.T) {
	s, st, _ := newState(t)
	boom := errors.New("boom")
	err := s.Submit(bg, func(tx *Tx) error {
		if err := addIdea("one", "first")(tx); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if n := count(t, st, `SELECT count(*) FROM ideas`) + count(t, st, `SELECT count(*) FROM events`); n != 0 {
		t.Fatalf("%d rows were left by a failed command", n)
	}
	if err := s.Submit(bg, addIdea("two", "second")); err != nil {
		t.Fatalf("the loop stopped after an error: %v", err)
	}
}

func TestCommandPanicRollsBack(t *testing.T) {
	s, st, _ := newState(t)
	err := s.Submit(bg, func(tx *Tx) error {
		if err := addIdea("one", "first")(tx); err != nil {
			return err
		}
		panic("kaboom")
	})
	if err == nil || err.Error() != "the command panicked: kaboom" {
		t.Fatalf("err = %v", err)
	}
	if n := count(t, st, `SELECT count(*) FROM ideas`) + count(t, st, `SELECT count(*) FROM events`); n != 0 {
		t.Fatalf("%d rows were left by a command that panicked", n)
	}
	if err := s.Submit(bg, addIdea("two", "second")); err != nil {
		t.Fatalf("the loop stopped after a panic: %v", err)
	}
}

func TestCommandsOneAtATime(t *testing.T) {
	s, st, _ := newState(t)
	if err := s.Submit(bg, addIdea("counter", "0")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Submit(bg, func(tx *Tx) error {
				var text string
				if err := tx.QueryRow(`SELECT text FROM ideas WHERE slug = 'counter'`).Scan(&text); err != nil {
					return err
				}
				n, err := strconv.Atoi(text)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE ideas SET text = ? WHERE slug = 'counter'`, strconv.Itoa(n+1)); err != nil {
					return err
				}
				return tx.Event("idea.counted", "idea", 1, "test", map[string]int{"n": n + 1})
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var text string
	if err := st.DB().QueryRow(`SELECT text FROM ideas WHERE slug = 'counter'`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "100" {
		t.Fatalf("counter = %s, want 100", text)
	}
	if n := count(t, st, `SELECT count(*) FROM events WHERE kind = 'idea.counted'`); n != 100 {
		t.Fatalf("events = %d, want 100", n)
	}
}

func TestEventsNewestFirst(t *testing.T) {
	s, _, _ := newState(t)
	err := s.Submit(bg, func(tx *Tx) error {
		for i := range 510 {
			if err := tx.Event(fmt.Sprintf("n.%03d", i), "test", 0, "", nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ limit, want int }{{0, 50}, {2, 2}, {-5, 1}, {500, 500}, {10000, 500}} {
		evs, err := s.Events(bg, c.limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) != c.want {
			t.Fatalf("limit %d gave %d events, want %d", c.limit, len(evs), c.want)
		}
		for i, e := range evs {
			if want := fmt.Sprintf("n.%03d", 509-i); e.Kind != want {
				t.Fatalf("limit %d: event %d is %s, want %s", c.limit, i, e.Kind, want)
			}
		}
	}
}

func TestEventsEmptyIsNotNil(t *testing.T) {
	s, _, _ := newState(t)
	evs, err := s.Events(bg, 0)
	if err != nil || evs == nil || len(evs) != 0 {
		t.Fatalf("events = %#v, %v; want an empty list", evs, err)
	}
}

func TestEventsDuringSubmits(t *testing.T) {
	s, st, _ := newState(t)
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	stop := make(chan struct{})
	var readers, writers sync.WaitGroup
	errs := make(chan error, 300)
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := s.Events(ctx, 20); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	for w := range 8 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := range 25 {
				if err := s.Submit(ctx, addIdea(fmt.Sprintf("w%d-%d", w, i), "x")); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("reads and commands did not finish inside 10s: %v", err)
	}
	if n := count(t, st, `SELECT count(*) FROM ideas`); n != 200 {
		t.Fatalf("ideas = %d, want 200", n)
	}
}

func TestSubmitAfterClose(t *testing.T) {
	s, st, _ := newState(t)
	done, cancel := context.WithCancel(bg)
	cancel()
	if err := s.Submit(done, addIdea("late", "x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("a done ctx gave %v, want context.Canceled", err)
	}
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	if err := s.Submit(bg, addIdea("one", "x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if err := s.Close(bg); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
	if n := count(t, st, `SELECT count(*) FROM ideas`); n != 0 {
		t.Fatalf("%d ideas were applied by refused commands", n)
	}
}

func TestWriteWithoutEventRefused(t *testing.T) {
	s, st, _ := newState(t)
	insert := `INSERT INTO ideas (slug, text, person, created_at, updated_at) VALUES ('s', 't', 'p', 'a', 'b')`
	for _, q := range []string{
		insert,
		"  -- a note\n /* and\n another */ " + insert,
		`WITH x AS (SELECT 1) ` + insert,
	} {
		err := s.Submit(bg, func(tx *Tx) error {
			_, err := tx.Exec(q)
			return err
		})
		if !errors.Is(err, ErrNoEvent) {
			t.Fatalf("%q: err = %v, want ErrNoEvent", q, err)
		}
	}
	err := s.Submit(bg, func(tx *Tx) error {
		var id int64
		return tx.QueryRow(insert + ` RETURNING id`).Scan(&id)
	})
	if !errors.Is(err, ErrNoEvent) {
		t.Fatalf("a write through QueryRow: err = %v, want ErrNoEvent", err)
	}
	if n := count(t, st, `SELECT count(*) FROM ideas`); n != 0 {
		t.Fatalf("%d ideas were written with no event", n)
	}
	// A write that changes no rows, and a read, are not state changes.
	err = s.Submit(bg, func(tx *Tx) error {
		if _, err := tx.Exec(`UPDATE ideas SET text = 'x' WHERE slug = 'nobody'`); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT 'delete' FROM ideas`)
		if err != nil {
			return err
		}
		return rows.Close()
	})
	if err != nil {
		t.Fatalf("a command that changed nothing was refused: %v", err)
	}
	for q, want := range map[string]bool{
		"select * from ideas": false, "  update ideas set x = 1": true, "Delete from ideas": true,
		"replace into ideas values (1)": true, "WITH a AS (SELECT 1) SELECT * FROM a": false,
		"with a as (select 1) delete from ideas": true, "/* insert */ SELECT 1": false, "PRAGMA user_version": false,
	} {
		if got := isWrite(q); got != want {
			t.Errorf("isWrite(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestEventDataMustBeObject(t *testing.T) {
	s, st, _ := newState(t)
	for _, bad := range []any{[]int{1}, "text", 5, map[string]int(nil), func() {}} {
		err := s.Submit(bg, func(tx *Tx) error { return tx.Event("bad", "test", 0, "", bad) })
		if err == nil {
			t.Fatalf("data %#v was accepted", bad)
		}
	}
	if err := s.Submit(bg, func(tx *Tx) error { return tx.Event("", "test", 0, "", nil) }); err == nil {
		t.Fatal("an event with no kind was accepted")
	}
	if n := count(t, st, `SELECT count(*) FROM events`); n != 0 {
		t.Fatalf("%d events were written by refused commands", n)
	}
	if err := s.Submit(bg, note("plain")); err != nil {
		t.Fatal(err)
	}
	if n := count(t, st, `SELECT count(*) FROM events WHERE subject_id IS NULL AND person IS NULL AND data = '{}'`); n != 1 {
		t.Fatal("a subject id of 0 and an empty person were not stored as NULL, or nil data not as {}")
	}
	evs, err := s.Events(bg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if e := evs[0]; e.SubjectID != 0 || e.Person != "" || string(e.Data) != "{}" {
		t.Fatalf("event = %+v", e)
	}
}

func TestTxRefusedAfterCommand(t *testing.T) {
	s, _, _ := newState(t)
	var kept *Tx
	if err := s.Submit(bg, func(tx *Tx) error { kept = tx; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := kept.Exec(`DELETE FROM ideas`); !errors.Is(err, errTxDone) {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := kept.Query(`SELECT 1`); !errors.Is(err, errTxDone) {
		t.Fatalf("Query: %v", err)
	}
	var n int
	if err := kept.QueryRow(`SELECT 1`).Scan(&n); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("QueryRow: %v", err)
	}
	if err := kept.Event("late", "test", 0, "", nil); !errors.Is(err, errTxDone) {
		t.Fatalf("Event: %v", err)
	}
	if err := kept.SetTimer(retentionKind, "x", time.Now(), nil); !errors.Is(err, errTxDone) {
		t.Fatalf("SetTimer: %v", err)
	}
	if err := kept.DeleteTimer(retentionKind, retentionKey); !errors.Is(err, errTxDone) {
		t.Fatalf("DeleteTimer: %v", err)
	}
}

func TestRetentionDeletesOld(t *testing.T) {
	s, st, c := newState(t)
	day := 24 * time.Hour
	if err := s.Submit(bg, note("old")); err != nil { // will be 91 days old
		t.Fatal(err)
	}
	c.Add(2 * day)
	if err := s.Submit(bg, note("new")); err != nil { // will be 89 days old
		t.Fatal(err)
	}
	settle(t, s) // the first daily fire: nothing is old yet
	c.Add(89 * day)
	settle(t, s)
	got := kinds(t, s)
	if len(got) != 2 || got[0] != "events.expired" || got[1] != "new" {
		t.Fatalf("events = %v, want [events.expired new]", got)
	}
	evs, _ := s.Events(bg, 1)
	if string(evs[0].Data) != `{"count":1}` || evs[0].Subject != "events" {
		t.Fatalf("events.expired = %+v", evs[0])
	}
	want := c.Now().Add(day)
	if due := dueOf(t, st, retentionKind, retentionKey); !due.Equal(want) {
		t.Fatalf("retention is due %s, want %s", due, want)
	}
	if next, ok := s.NextTimer(); !ok || !next.Equal(want) {
		t.Fatalf("NextTimer = %s, %v; want %s", next, ok, want)
	}
}

func TestRetentionNothingToDelete(t *testing.T) {
	s, st, c := newState(t)
	c.Add(25 * time.Hour)
	settle(t, s)
	if n := count(t, st, `SELECT count(*) FROM events`); n != 0 {
		t.Fatalf("%d events were appended by a fire that deleted nothing", n)
	}
	want := c.Now().Add(24 * time.Hour)
	if due := dueOf(t, st, retentionKind, retentionKey); !due.Equal(want) {
		t.Fatalf("retention is due %s, want %s", due, want)
	}
	if n := count(t, st, `SELECT count(*) FROM timers`); n != 1 {
		t.Fatalf("timers = %d, want 1", n)
	}
}

func TestTimerSurvivesReopen(t *testing.T) {
	c := newClock()
	dir := t.TempDir()
	st := openStore(t, dir)
	s := openState(t, st, c)
	want := c.Now().Add(24 * time.Hour)
	if due := dueOf(t, st, retentionKind, retentionKey); !due.Equal(want) {
		t.Fatalf("retention is due %s, want %s", due, want)
	}
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	c.Add(time.Hour)
	st = openStore(t, dir)
	s = openState(t, st, c)
	settle(t, s)
	if due := dueOf(t, st, retentionKind, retentionKey); !due.Equal(want) {
		t.Fatalf("after reopening, retention is due %s, want %s", due, want)
	}
	if next, ok := s.NextTimer(); !ok || !next.Equal(want) {
		t.Fatalf("NextTimer = %s, %v; want %s", next, ok, want)
	}
	if n := count(t, st, `SELECT count(*) FROM timers`); n != 1 {
		t.Fatalf("timers = %d, want 1", n)
	}
}

func TestOverdueTimerFiresOnOpen(t *testing.T) {
	ping := TimerKind{Name: "test.ping", Fire: func(tx *Tx, tm Timer) error {
		return tx.Event("test.fired", "timer", tm.ID, "", map[string]any{"key": tm.Key, "payload": tm.Payload})
	}}
	c := newClock()
	dir := t.TempDir()
	st := openStore(t, dir)
	s := openState(t, st, c, ping)
	err := s.Submit(bg, func(tx *Tx) error {
		return tx.SetTimer("test.ping", "a", tx.Now().Add(time.Hour), map[string]int{"n": 7})
	})
	if err != nil {
		t.Fatal(err)
	}
	if next, ok := s.NextTimer(); !ok || !next.Equal(c.Now().Add(time.Hour)) {
		t.Fatalf("NextTimer = %s, %v; want the new timer's due time", next, ok)
	}
	settle(t, s)
	if got := kinds(t, s); len(got) != 0 {
		t.Fatalf("the timer fired early: %v", got)
	}
	s.Close(bg)
	st.Close()

	c.Add(2 * time.Hour)
	st = openStore(t, dir)
	s = openState(t, st, c, ping)
	if err := s.Submit(bg, func(*Tx) error { return nil }); err != nil { // no poke: Open alone fires it
		t.Fatal(err)
	}
	evs, err := s.Events(bg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != "test.fired" || string(evs[0].Data) != `{"key":"a","payload":{"n":7}}` {
		t.Fatalf("events = %+v, want one test.fired", evs)
	}
	// Fire did not set it again, so the row went in the same transaction.
	if n := count(t, st, `SELECT count(*) FROM timers WHERE kind = 'test.ping'`); n != 0 {
		t.Fatal("a fired timer that was not set again is still there")
	}
}

func TestUnknownTimerKindIgnored(t *testing.T) {
	c := newClock()
	st := openStore(t, t.TempDir())
	overdue := stamp(c.Now().Add(-time.Hour))
	if _, err := st.DB().Exec(`INSERT INTO timers (kind, key, due_at, payload) VALUES ('later.kind', 'k', ?, '{"a":1}')`, overdue); err != nil {
		t.Fatal(err)
	}
	s := openState(t, st, c)
	// The retention timer is always there, so "none" is: nothing but it.
	if next, ok := s.NextTimer(); !ok || !next.Equal(c.Now().Add(24*time.Hour)) {
		t.Fatalf("NextTimer = %s, %v; want only the retention timer's time", next, ok)
	}
	settle(t, s)
	settle(t, s)
	var due, payload string
	if err := st.DB().QueryRow(`SELECT due_at, payload FROM timers WHERE kind = 'later.kind' AND key = 'k'`).Scan(&due, &payload); err != nil {
		t.Fatal(err)
	}
	if due != overdue || payload != `{"a":1}` {
		t.Fatalf("the row changed: due %s payload %s", due, payload)
	}
	if n := count(t, st, `SELECT count(*) FROM events`); n != 0 {
		t.Fatalf("%d events were written", n)
	}
	if err := s.Submit(bg, func(tx *Tx) error { return tx.SetTimer("later.kind", "k2", tx.Now(), nil) }); err == nil {
		t.Fatal("a timer of an unregistered kind was set")
	}
}

func TestTimerFireErrorBacksOff(t *testing.T) {
	fires := 0
	failing := TimerKind{Name: "test.fail", Fire: func(tx *Tx, tm Timer) error {
		fires++
		if err := addIdea(fmt.Sprintf("f%d", fires), "x")(tx); err != nil {
			return err
		}
		if fires == 2 {
			panic("second fire")
		}
		return errors.New("no")
	}}
	s, st, c := newState(t, failing)
	start := c.Now()
	if err := s.Submit(bg, func(tx *Tx) error { return tx.SetTimer("test.fail", "k", tx.Now(), nil) }); err != nil {
		t.Fatal(err)
	}
	settle(t, s)
	settle(t, s) // still one fire: the timer is no longer due
	for i, want := range []time.Duration{time.Minute, 3 * time.Minute, 7 * time.Minute} {
		if fires != i+1 {
			t.Fatalf("fires = %d, want %d", fires, i+1)
		}
		if due := dueOf(t, st, "test.fail", "k"); !due.Equal(start.Add(want)) {
			t.Fatalf("after failure %d the timer is due %s after the start, want %s", i+1, due.Sub(start), want)
		}
		if next, ok := s.NextTimer(); !ok || !next.Equal(start.Add(want)) {
			t.Fatalf("NextTimer = %s, %v", next, ok)
		}
		c.Add(want - c.Now().Sub(start))
		settle(t, s)
	}
	if n := count(t, st, `SELECT count(*) FROM ideas`) + count(t, st, `SELECT count(*) FROM events`); n != 0 {
		t.Fatalf("%d rows were left by failed fires", n)
	}
}

func TestBadTimerKindRefused(t *testing.T) {
	st := openStore(t, t.TempDir())
	ok := func(*Tx, Timer) error { return nil }
	for _, k := range [][]TimerKind{{{Name: "", Fire: ok}}, {{Name: "a"}}, {{Name: "a", Fire: ok}, {Name: "a", Fire: ok}}, {{Name: retentionKind, Fire: ok}}} {
		if s, err := Open(st, Options{Timers: k}); err == nil {
			s.Close(bg)
			t.Fatalf("kinds %+v were accepted", k)
		}
	}
}

func TestEnsuredTimerCatchesUpOnOpen(t *testing.T) {
	fired := TimerKind{Name: "test.catchup", Key: "poll", Fire: func(tx *Tx, tm Timer) error {
		return tx.Event("test.fired", "timer", tm.ID, "", map[string]string{"key": tm.Key})
	}}
	plain := TimerKind{Name: "test.plain", Fire: func(*Tx, Timer) error { return nil }}
	c := newClock()
	dir := t.TempDir()
	for open := 1; open <= 2; open++ { // each open, a restart, ensures it due at once
		st := openStore(t, dir)
		s := openState(t, st, c, fired, plain)
		if err := s.Submit(bg, func(*Tx) error { return nil }); err != nil { // no poke: Open alone makes it due
			t.Fatal(err)
		}
		if n := count(t, st, `SELECT count(*) FROM events WHERE kind = 'test.fired'`); n != open {
			t.Fatalf("open %d: %d fires, want %d", open, n, open)
		}
		if n := count(t, st, `SELECT count(*) FROM timers WHERE kind IN ('test.catchup', 'test.plain')`); n != 0 {
			t.Fatalf("open %d: %d rows left: the fired one is deleted and a kind without a Key gets none", open, n)
		}
		s.Close(bg)
		st.Close()
	}
}

func TestEnsuredTimerKeepsEarlierDue(t *testing.T) {
	hourly := TimerKind{Name: "test.hourly", Key: "k", Delay: time.Hour, Fire: func(*Tx, Timer) error { return nil }}
	c := newClock()
	dir := t.TempDir()
	st := openStore(t, dir)
	s := openState(t, st, c, hourly)
	want := c.Now().Add(time.Hour)
	if due := dueOf(t, st, "test.hourly", "k"); !due.Equal(want) {
		t.Fatalf("ensured due %s, want %s", due, want)
	}
	s.Close(bg)
	st.Close()

	c.Add(30 * time.Minute) // a reopen would make it due at +1h30; the earlier +1h stays
	st = openStore(t, dir)
	s = openState(t, st, c, hourly)
	if due := dueOf(t, st, "test.hourly", "k"); !due.Equal(want) {
		t.Fatalf("after a reopen, due %s, want the earlier %s", due, want)
	}
	if next, ok := s.NextTimer(); !ok || !next.Equal(want) {
		t.Fatalf("NextTimer = %s, %v; want %s", next, ok, want)
	}
	if err := s.Submit(bg, func(tx *Tx) error { return tx.SetTimer("test.hourly", "k", tx.Now().Add(5*time.Hour), nil) }); err != nil {
		t.Fatal(err)
	}
	s.Close(bg)
	st.Close()

	st = openStore(t, dir) // a due time later than open plus the delay is pulled in
	openState(t, st, c, hourly)
	if due, want := dueOf(t, st, "test.hourly", "k"), c.Now().Add(time.Hour); !due.Equal(want) {
		t.Fatalf("after a reopen, due %s, want %s", due, want)
	}
	if n := count(t, st, `SELECT count(*) FROM timers WHERE kind = 'test.hourly'`); n != 1 {
		t.Fatalf("%d rows of the ensured kind, want 1", n)
	}
}

func TestRetentionEnsuredOnce(t *testing.T) {
	c := newClock()
	dir := t.TempDir()
	for open := 1; open <= 2; open++ {
		st := openStore(t, dir)
		s := openState(t, st, c)
		if n := count(t, st, `SELECT count(*) FROM timers WHERE kind = 'events.expire' AND key = 'retention'`); n != 1 {
			t.Fatalf("open %d: %d retention rows, want 1", open, n)
		}
		if n := count(t, st, `SELECT count(*) FROM timers`); n != 1 {
			t.Fatalf("open %d: %d timers, want 1", open, n)
		}
		if due, want := dueOf(t, st, retentionKind, retentionKey), newClock().Now().Add(24*time.Hour); !due.Equal(want) {
			t.Fatalf("open %d: retention due %s, want %s", open, due, want)
		}
		s.Close(bg)
		st.Close()
		c.Add(time.Hour)
	}
}
