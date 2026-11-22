package state

import (
	"encoding/json"
	"fmt"
	"time"
)

// The retention timer: once a day the engine deletes events older than 90 days.
const (
	retentionKind  = "events.expire"
	retentionKey   = "retention"
	retentionEvery = 24 * time.Hour
	retentionAge   = 90 * 24 * time.Hour
)

// stamp writes a due time as RFC 3339 UTC with fixed milliseconds, so due
// times sort as text and one due within a second is not rounded into the past.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// Timer is one row of the timers table, as its kind's Fire sees it.
type Timer struct {
	ID        int64
	Kind, Key string
	Due       time.Time
	Payload   json.RawMessage
}

// TimerKind names a kind of timer and what the loop does when one is due.
// Fire runs as a command: it may set the same (kind, key) again with
// tx.SetTimer; if it does not, the row is deleted in the same transaction.
type TimerKind struct {
	Name string
	Fire func(tx *Tx, t Timer) error
}

// SetTimer sets the timer (kind, key) to fire at due, replacing its due time
// and payload if it exists. The kind must be registered, or nothing would
// ever fire it. A nil payload is {}.
func (tx *Tx) SetTimer(kind, key string, due time.Time, payload any) error {
	if tx.done.Load() {
		return errTxDone
	}
	body, err := marshal(payload)
	if _, ok := tx.s.kinds[kind]; !ok || err != nil {
		return fmt.Errorf("timer %q needs a registered kind and a payload that is JSON (%v)", kind, err)
	}
	_, err = tx.tx.Exec(`INSERT INTO timers (kind, key, due_at, payload) VALUES (?, ?, ?, ?)
		ON CONFLICT (kind, key) DO UPDATE SET due_at = excluded.due_at, payload = excluded.payload`,
		kind, key, stamp(due), body)
	tx.timers, tx.set[[2]string{kind, key}] = true, true
	return err
}

// DeleteTimer removes the timer (kind, key), if it exists.
func (tx *Tx) DeleteTimer(kind, key string) error {
	if tx.done.Load() {
		return errTxDone
	}
	_, err := tx.tx.Exec(`DELETE FROM timers WHERE kind = ? AND key = ?`, kind, key)
	tx.timers = true
	return err
}

// NextTimer returns the earliest due time of the timers this engine can
// fire, from memory. Timers of a kind it does not register are left out.
func (s *State) NextTimer() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next, s.hasNext
}

func (s *State) setNext(t time.Time, ok bool) {
	s.mu.Lock()
	s.next, s.hasNext = t, ok
	s.mu.Unlock()
}

// load reads the timers, stores the earliest due time of the registered
// kinds and returns the ones due now. A timer of a kind this binary does not
// register (set by a newer engine) is left in place and out of both, so it
// never spins the loop or keeps the engine up. No transaction is open here.
func (s *State) load() ([]Timer, error) {
	rows, err := s.db.Query(`SELECT id, kind, key, due_at, payload FROM timers ORDER BY due_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var due []Timer
	var next time.Time
	found, now := false, s.now()
	for rows.Next() {
		var t Timer
		var at, payload string
		if err := rows.Scan(&t.ID, &t.Kind, &t.Key, &at, &payload); err != nil {
			return nil, err
		}
		if _, ok := s.kinds[t.Kind]; !ok {
			if !s.logged[t.Kind] {
				s.logged[t.Kind] = true
				s.logf("leaving timers of kind %s alone: this engine does not know the kind", t.Kind)
			}
			continue
		}
		if t.Due, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		t.Payload = json.RawMessage(payload)
		when := t.Due
		if h, ok := s.held[t.ID]; ok && h.After(when) {
			when = h
		}
		if !found || when.Before(next) {
			next, found = when, true
		}
		if !when.After(now) {
			due = append(due, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.setNext(next, found)
	return due, nil
}

// refresh is load for the running loop: when the timers cannot be read it
// says so and looks again in five seconds, so the loop never spins on it.
func (s *State) refresh() []Timer {
	due, err := s.load()
	if err != nil {
		s.logf("reading timers: %v", err)
		s.setNext(s.now().Add(5*time.Second), true)
	}
	return due
}

// fireDue fires the timers due now, one command each, reading them again
// after every fire (a fire may set or delete another timer). It fires at most
// as many as were due at its start, so no kind can hold the loop.
func (s *State) fireDue() {
	if next, ok := s.NextTimer(); !ok || next.After(s.now()) {
		return
	}
	due := s.refresh()
	for n := len(due); n > 0 && len(due) > 0; n-- {
		s.fire(due[0])
		due = s.refresh()
	}
}

// fire applies one due timer as a command. A Fire that fails or panics rolls
// back; the timer is then pushed out, in its own transaction, by a backoff
// of one minute doubling per consecutive failure up to an hour.
func (s *State) fire(t Timer) {
	key := [2]string{t.Kind, t.Key}
	err := s.apply(func(tx *Tx) error {
		if err := s.kinds[t.Kind].Fire(tx, t); err != nil || tx.set[key] {
			return err
		}
		return tx.DeleteTimer(t.Kind, t.Key)
	})
	if err == nil {
		delete(s.fails, key)
		delete(s.held, t.ID)
		return
	}
	backoff := min(time.Minute<<min(s.fails[key], 6), time.Hour)
	s.fails[key]++
	until := s.now().Add(backoff)
	s.logf("timer %s %s failed, trying again in %s: %v", t.Kind, t.Key, backoff, err)
	if _, err := s.db.Exec(`UPDATE timers SET due_at = ? WHERE id = ?`, stamp(until), t.ID); err != nil {
		s.held[t.ID] = until // the row could not be written; hold it in memory
		s.logf("timer %s %s: could not move its due time: %v", t.Kind, t.Key, err)
	}
}

// expireEvents is the retention kind's Fire: it deletes events older than 90
// days and sets itself for a day later. With nothing to delete no row
// changed, so no event is appended.
func expireEvents(tx *Tx, t Timer) error {
	res, err := tx.Exec(`DELETE FROM events WHERE at < ?`, tx.Now().Add(-retentionAge).UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if err := tx.Event("events.expired", "events", 0, "", map[string]int64{"count": n}); err != nil {
			return err
		}
	}
	return tx.SetTimer(t.Kind, t.Key, tx.Now().Add(retentionEvery), nil)
}
