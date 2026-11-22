package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/store"
)

// openIntents opens a State whose store directory is dir, so log.append has
// somewhere to write, with extra intent kinds.
func openIntents(t *testing.T, st *store.Store, dir string, c *clock, kinds ...IntentKind) *State {
	t.Helper()
	s, err := Open(st, Options{StoreDir: dir, Now: c.Now, Intents: kinds})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(bg) })
	return s
}

func newIntents(t *testing.T, kinds ...IntentKind) (*State, *store.Store, *clock, string) {
	t.Helper()
	dir, c := t.TempDir(), newClock()
	st := openStore(t, dir)
	return openIntents(t, st, dir, c, kinds...), st, c, dir
}

// eventually waits for the worker, which runs on its own goroutine: it polls
// ok with a deadline. Due times are never waited for; tests move the clock.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

type intentRow struct {
	state           string
	attempts        int
	result, lastErr sql.NullString
	next            time.Time
	done            sql.NullString
}

func readIntent(t *testing.T, st *store.Store, id int64) intentRow {
	t.Helper()
	var r intentRow
	var next string
	err := st.DB().QueryRow(`SELECT state, attempts, result, last_error, next_at, done_at FROM intents WHERE id = ?`, id).
		Scan(&r.state, &r.attempts, &r.result, &r.lastErr, &next, &r.done)
	if err != nil {
		t.Fatalf("intent %d: %v", id, err)
	}
	if r.next, err = time.Parse(time.RFC3339Nano, next); err != nil {
		t.Fatal(err)
	}
	return r
}

// writeIntent submits one command that appends an event and writes an intent.
func writeIntent(t *testing.T, s *State, kind string, payload any) int64 {
	t.Helper()
	var id int64
	err := s.Submit(bg, func(tx *Tx) (err error) {
		if id, err = tx.Intent(kind, payload); err != nil {
			return err
		}
		return tx.Event("test.asked", "test", 0, "", nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func line(file, text string) map[string]string {
	return map[string]string{"file": file, "text": text}
}

func readLog(t *testing.T, dir, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "logs", file))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIntentInSameTransaction(t *testing.T) {
	s, st, _, dir := newIntents(t)
	err := s.Submit(bg, func(tx *Tx) error {
		if _, err := tx.Intent("log.append", line("run.log", "never")); err != nil {
			t.Errorf("writing the intent: %v", err)
		}
		if err := tx.Event("test.asked", "test", 0, "", nil); err != nil {
			t.Errorf("appending the event: %v", err)
		}
		return errors.New("the command fails after its intent")
	})
	if err == nil {
		t.Fatal("the failing command reported no error")
	}
	// An intent of a kind nobody registered fails its command too.
	err = s.Submit(bg, func(tx *Tx) error {
		if err := addIdea("with-intent", "0")(tx); err != nil {
			return err
		}
		_, err := tx.Intent("no.such.kind", nil)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "no.such.kind") {
		t.Fatalf("an unregistered kind: got %v, want an error naming it", err)
	}
	settle(t, s)
	if err := s.Close(bg); err != nil { // the worker has stopped: nothing can still be written
		t.Fatal(err)
	}
	for _, table := range []string{"intents", "events", "ideas"} {
		if n := count(t, st, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%d rows in %s after two failed commands, want 0", n, table)
		}
	}
	if n := s.PendingIntents(); n != 0 {
		t.Errorf("PendingIntents = %d, want 0", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs")); !os.IsNotExist(err) {
		t.Errorf("a log was written for an intent that never committed (stat: %v)", err)
	}
}

func TestIntentDone(t *testing.T) {
	s, st, _, dir := newIntents(t)
	id := writeIntent(t, s, "log.append", line("run.log", "hello"))
	eventually(t, "the intent to be done", func() bool { return readIntent(t, st, id).state == "done" })
	r := readIntent(t, st, id)
	if r.attempts != 1 || r.result.String != "written" || !r.done.Valid || r.lastErr.Valid {
		t.Errorf("the done intent is %+v, want 1 attempt, result written, a done_at and no error", r)
	}
	if got := readLog(t, dir, "run.log"); got != "1 hello\n" {
		t.Errorf("run.log = %q, want one line %q", got, "1 hello\n")
	}
	if n := count(t, st, `SELECT count(*) FROM events WHERE kind = 'intent.done' AND subject = 'intent' AND subject_id = ?`, id); n != 1 {
		t.Errorf("%d intent.done events for the intent, want 1", n)
	}
	settle(t, s) // the count is refreshed on the loop, just after the commit
	if n := s.PendingIntents(); n != 0 {
		t.Errorf("PendingIntents = %d after the intent was done, want 0", n)
	}
	// A second intent adds its own line and leaves the first alone.
	id2 := writeIntent(t, s, "log.append", line("run.log", "again"))
	eventually(t, "the second intent to be done", func() bool { return readIntent(t, st, id2).state == "done" })
	if got := readLog(t, dir, "run.log"); got != "1 hello\n2 again\n" {
		t.Errorf("run.log = %q after two intents", got)
	}
	if info, err := os.Stat(filepath.Join(dir, "logs", "run.log")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("run.log: %v, mode %v, want 0600", err, info.Mode().Perm())
	}
}

func TestIntentRetriedOnOpen(t *testing.T) {
	dir, c := t.TempDir(), newClock()
	st := openStore(t, dir)
	started := make(chan struct{})
	s1 := openIntents(t, st, dir, c, IntentKind{
		Name: "test.hold", Repeat: "it changes nothing",
		Do: func(ctx context.Context, in Intent) (string, error) {
			close(started)
			<-ctx.Done() // cut short by Close
			return "", ctx.Err()
		},
	})
	id := writeIntent(t, s1, "test.hold", nil)
	<-started
	if err := s1.Close(bg); err != nil {
		t.Fatal(err)
	}
	if r := readIntent(t, st, id); r.state != "pending" || r.attempts != 0 {
		t.Fatalf("after a Do cut short by Close the intent is %+v, want pending with no attempt recorded", r)
	}
	if n := count(t, st, `SELECT count(*) FROM events WHERE subject = 'intent'`); n != 0 {
		t.Fatalf("%d intent events after a Do cut short by Close, want 0", n)
	}
	// Whatever its next_at: a pending intent is due at once on Open.
	if _, err := st.DB().Exec(`UPDATE intents SET next_at = ? WHERE id = ?`, stamp(c.Now().Add(24*time.Hour)), id); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	release := make(chan struct{})
	s2 := openIntents(t, st, dir, c, IntentKind{
		Name: "test.hold", Repeat: "it changes nothing",
		Do: func(ctx context.Context, in Intent) (string, error) {
			calls.Add(1)
			<-release
			return "ok", nil
		},
	})
	if n := s2.PendingIntents(); n != 1 {
		t.Fatalf("PendingIntents straight after Open = %d, want 1: a restarted engine must count its unfinished intents", n)
	}
	close(release)
	eventually(t, "the retried intent to be done", func() bool { return readIntent(t, st, id).state == "done" })
	settle(t, s2)
	if err := s2.Close(bg); err != nil {
		t.Fatal(err)
	}
	r := readIntent(t, st, id)
	if calls.Load() != 1 || r.attempts != 1 || r.result.String != "ok" {
		t.Errorf("after the retry: %d calls, intent %+v; want it done exactly once", calls.Load(), r)
	}
	if n := count(t, st, `SELECT count(*) FROM events WHERE kind = 'intent.done' AND subject_id = ?`, id); n != 1 {
		t.Errorf("%d intent.done events, want 1", n)
	}
	if n := s2.PendingIntents(); n != 0 {
		t.Errorf("PendingIntents = %d after the retry, want 0", n)
	}
}

func TestIntentRepeatSafe(t *testing.T) {
	for _, tc := range []struct{ name, have, result, want string }{
		{"line already written", "1 hello\n", "already written", "1 hello\n"},
		{"another id with the same first digit", "11 other\n", "written", "11 other\n1 hello\n"},
		{"the id later in the file", "7 x\n1 hello\n8 y\n", "already written", "7 x\n1 hello\n8 y\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, c := t.TempDir(), newClock()
			st := openStore(t, dir)
			// The crash this stands for: the line was written, then the
			// engine was killed before the intent was recorded done.
			if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "logs", "run.log"), []byte(tc.have), 0o600); err != nil {
				t.Fatal(err)
			}
			s := openIntents(t, st, dir, c)
			id := writeIntent(t, s, "log.append", line("run.log", "hello"))
			if id != 1 {
				t.Fatalf("the first intent has id %d, want 1", id)
			}
			eventually(t, "the intent to be done", func() bool { return readIntent(t, st, id).state == "done" })
			if r := readIntent(t, st, id); r.result.String != tc.result {
				t.Errorf("result = %q, want %q", r.result.String, tc.result)
			}
			if got := readLog(t, dir, "run.log"); got != tc.want {
				t.Errorf("run.log = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPendingIntentsCount(t *testing.T) {
	release := make(chan struct{})
	s, st, _, _ := newIntents(t, IntentKind{
		Name: "test.wait", Repeat: "it changes nothing",
		Do: func(ctx context.Context, in Intent) (string, error) {
			select {
			case <-release:
				return "ok", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	})
	if n := s.PendingIntents(); n != 0 {
		t.Fatalf("PendingIntents of a fresh store = %d, want 0", n)
	}
	for want := 1; want <= 3; want++ {
		writeIntent(t, s, "test.wait", map[string]int{"n": want})
		if n := s.PendingIntents(); n != want {
			t.Fatalf("PendingIntents = %d once Submit returned, want %d", n, want)
		}
	}
	close(release)
	eventually(t, "the count to reach 0", func() bool { return s.PendingIntents() == 0 })
	if n := count(t, st, `SELECT count(*) FROM intents WHERE state = 'done' AND attempts = 1`); n != 3 {
		t.Errorf("%d intents done once, want 3", n)
	}
}

func TestIntentBackoffThenFail(t *testing.T) {
	var calls atomic.Int32
	s, st, c, _ := newIntents(t, IntentKind{
		Name: "test.fail", Repeat: "it changes nothing",
		Do: func(ctx context.Context, in Intent) (string, error) {
			if n := calls.Add(1); int(n) != in.Attempts+1 {
				t.Errorf("call %d saw %d earlier attempts", n, in.Attempts)
			}
			return "", errors.New("nope")
		},
	})
	id := writeIntent(t, s, "test.fail", nil)
	for attempt := 1; attempt < maxAttempts; attempt++ {
		eventually(t, "the attempt to be recorded", func() bool { return readIntent(t, st, id).attempts == attempt })
		backoff := min(time.Second<<(attempt-1), time.Minute)
		r := readIntent(t, st, id)
		if r.state != "pending" || r.lastErr.String != "nope" || !r.next.Equal(c.Now().Add(backoff)) {
			t.Fatalf("after attempt %d: %+v, want pending, the error, and next_at %s from now", attempt, r, backoff)
		}
		// Not due yet: a look now must not carry it out again.
		c.Add(backoff - time.Millisecond)
		s.poke()
		settle(t, s)
		c.Add(time.Millisecond)
		s.poke()
	}
	eventually(t, "the intent to fail", func() bool { return readIntent(t, st, id).state == "failed" })
	c.Add(time.Hour)
	settle(t, s)
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	if r := readIntent(t, st, id); r.attempts != maxAttempts || r.lastErr.String != "nope" || r.done.Valid {
		t.Errorf("the failed intent is %+v, want %d attempts and its last error", r, maxAttempts)
	}
	if n := calls.Load(); n != maxAttempts {
		t.Errorf("Do ran %d times, want %d", n, maxAttempts)
	}
	if n := count(t, st, `SELECT count(*) FROM events WHERE kind = 'intent.retry' AND subject_id = ?`, id); n != maxAttempts-1 {
		t.Errorf("%d intent.retry events, want %d", n, maxAttempts-1)
	}
	if n := count(t, st, `SELECT count(*) FROM events WHERE kind = 'intent.failed' AND subject_id = ?`, id); n != 1 {
		t.Errorf("%d intent.failed events, want 1", n)
	}
	if n := s.PendingIntents(); n != 0 {
		t.Errorf("PendingIntents = %d: a failed intent must not count as pending", n)
	}
}

func TestIntentDoPanicIsAnError(t *testing.T) {
	s, st, _, _ := newIntents(t, IntentKind{
		Name: "test.panic", Repeat: "it changes nothing",
		Do: func(context.Context, Intent) (string, error) { panic("kaboom") },
	})
	id := writeIntent(t, s, "test.panic", nil)
	eventually(t, "the attempt to be recorded", func() bool { return readIntent(t, st, id).attempts == 1 })
	if r := readIntent(t, st, id); r.state != "pending" || r.lastErr.String != "the intent panicked: kaboom" {
		t.Errorf("after a Do that panicked: %+v, want pending with the panic as its error", r)
	}
}

func TestUnknownIntentKind(t *testing.T) {
	dir, c := t.TempDir(), newClock()
	st := openStore(t, dir)
	// Written by a newer engine: overdue, of a kind this binary does not know.
	past := stamp(c.Now().Add(-time.Hour))
	if _, err := st.DB().Exec(`INSERT INTO intents (kind, payload, next_at, created_at) VALUES ('later.kind', '{}', ?, ?)`, past, past); err != nil {
		t.Fatal(err)
	}
	s := openIntents(t, st, dir, c)
	if n := s.PendingIntents(); n != 0 {
		t.Fatalf("PendingIntents = %d, want 0: an unknown kind must not keep the engine up", n)
	}
	// The worker gets past it to an intent it does know.
	id := writeIntent(t, s, "log.append", line("run.log", "known"))
	eventually(t, "the known intent to be done", func() bool { return readIntent(t, st, id).state == "done" })
	settle(t, s)
	if n := s.PendingIntents(); n != 0 {
		t.Errorf("PendingIntents = %d after the known intent, want 0", n)
	}
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	if r := readIntent(t, st, 1); r.state != "pending" || r.attempts != 0 || r.lastErr.Valid || !r.next.Equal(c.Now().Add(-time.Hour)) {
		t.Errorf("the unknown intent is %+v, want it pending and untouched", r)
	}
	if n := count(t, st, `SELECT count(*) FROM events WHERE subject = 'intent' AND subject_id = 1`); n != 0 {
		t.Errorf("%d events about the unknown intent, want 0", n)
	}
}

func TestBadLogAppendPayload(t *testing.T) {
	s, st, _, dir := newIntents(t)
	for name, payload := range map[string]any{
		"a path with a separator": line("a/b", "x"),
		"a path that climbs":      line("../x.log", "x"),
		"dot dot":                 line("..", "x"),
		"dot":                     line(".", "x"),
		"no file":                 line("", "x"),
		"a backslash":             line(`a\b`, "x"),
		"a name too long":         line(strings.Repeat("n", 101), "x"),
		"a newline in the text":   line("run.log", "two\nlines"),
		"not an object":           "run.log",
	} {
		err := s.Submit(bg, func(tx *Tx) error {
			if err := tx.Event("test.asked", "test", 0, "", nil); err != nil {
				return err
			}
			_, err := tx.Intent("log.append", payload)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "log.append") {
			t.Errorf("%s: got %v, want the command refused with an error naming the kind", name, err)
		}
	}
	if n := count(t, st, `SELECT count(*) FROM intents`) + count(t, st, `SELECT count(*) FROM events`); n != 0 {
		t.Errorf("%d rows left by refused commands, want 0", n)
	}
	// The longest name allowed is accepted.
	id := writeIntent(t, s, "log.append", line(strings.Repeat("n", 100), "x"))
	eventually(t, "the intent to be done", func() bool { return readIntent(t, st, id).state == "done" })

	// A bad row that reached the table some other way is refused by Do too:
	// nothing is written outside the logs folder.
	now := stamp(s.now())
	if _, err := st.DB().Exec(`INSERT INTO intents (kind, payload, next_at, created_at) VALUES ('log.append', '{"file":"../escaped.log","text":"x"}', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	s.poke()
	eventually(t, "the bad intent to be tried", func() bool { return readIntent(t, st, id+1).attempts == 1 })
	if r := readIntent(t, st, id+1); r.state != "pending" || !strings.Contains(r.lastErr.String, "plain file name") {
		t.Errorf("the bad intent is %+v, want a recorded refusal", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.log")); !os.IsNotExist(err) {
		t.Errorf("log.append wrote outside its logs folder (stat: %v)", err)
	}

	// With no store directory there is nowhere to keep logs.
	bare, _, _ := newState(t)
	err := bare.Submit(bg, func(tx *Tx) error {
		_, err := tx.Intent("log.append", line("run.log", "x"))
		return err
	})
	if err == nil {
		t.Error("log.append was accepted by a state opened with no store directory")
	}
}

func TestKindNeedsRepeat(t *testing.T) {
	do := func(context.Context, Intent) (string, error) { return "", nil }
	st := openStore(t, t.TempDir())
	for name, kinds := range map[string][]IntentKind{
		"no Repeat":           {{Name: "test.kind", Do: do}},
		"a blank Repeat":      {{Name: "test.kind", Repeat: "  ", Do: do}},
		"no Do":               {{Name: "test.kind", Repeat: "it changes nothing"}},
		"no name":             {{Repeat: "it changes nothing", Do: do}},
		"a name used twice":   {{Name: "test.kind", Repeat: "it changes nothing", Do: do}, {Name: "test.kind", Repeat: "it changes nothing", Do: do}},
		"log.append's name":   {{Name: "log.append", Repeat: "it changes nothing", Do: do}},
		"a good kind, then a": {{Name: "test.kind", Repeat: "it changes nothing", Do: do}, {Name: "test.other", Do: do}},
	} {
		s, err := Open(st, Options{Now: newClock().Now, Intents: kinds})
		if err == nil {
			s.Close(bg)
			t.Errorf("%s: Open accepted the kinds", name)
		}
	}
	s, err := Open(st, Options{Now: newClock().Now, Intents: []IntentKind{{Name: "test.kind", Repeat: "it changes nothing", Do: do}}})
	if err != nil {
		t.Fatalf("a kind with a Repeat text was refused: %v", err)
	}
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	if got := logAppend("").Repeat; got != "the line carries the intent's id, and a line already written is not written again" {
		t.Errorf("log.append's Repeat text is %q", got)
	}
}
