package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxAttempts = 10 // tries before an intent is failed

// IntentKind names a kind of intent. Repeat says, in one sentence, why
// carrying it out twice is safe. Check, if set, refuses a bad payload when
// the intent is written. Do runs off the loop, under a ctx that Close ends.
// Record, if set, stores what a successful Do found, in the transaction that
// marks the intent done; if it fails, nothing is kept and Do counts as failed.
type IntentKind struct {
	Name   string
	Repeat string // required
	Check  func(payload json.RawMessage) error
	Do     func(ctx context.Context, in Intent) (result string, err error)
	Record func(tx *Tx, in Intent, result string) error
}

// recordError is a Record's failure: carry records it as the Do's.
type recordError struct{ error }

// Intent is one pending intent, as its kind's Do sees it.
type Intent struct {
	ID       int64
	Kind     string
	Payload  json.RawMessage
	Attempts int
}

// register takes log.append and the extra kinds; each needs a Repeat text.
func (s *State) register(extra []IntentKind) error {
	s.intents = map[string]IntentKind{}
	for _, k := range append([]IntentKind{logAppend(s.storeDir)}, extra...) {
		if _, dup := s.intents[k.Name]; dup || k.Name == "" || k.Do == nil || strings.TrimSpace(k.Repeat) == "" {
			return fmt.Errorf("intent kind %q needs a name of its own, a Do function and a Repeat text saying why carrying it out twice is safe", k.Name)
		}
		s.intents[k.Name], s.names = k, append(s.names, k.Name)
	}
	s.mine = "(?" + strings.Repeat(", ?", len(s.names)-1) + ")"
	return nil
}

// Intent writes a pending intent, due now, in the command's transaction. The
// kind must be registered and its Check must accept the payload; nil is {}.
func (tx *Tx) Intent(kind string, payload any) (id int64, err error) {
	if tx.done.Load() {
		return 0, errTxDone
	}
	k, ok := tx.s.intents[kind]
	body, err := marshal(payload)
	if ok && err == nil && k.Check != nil {
		err = k.Check(json.RawMessage(body))
	}
	if !ok || err != nil {
		return 0, fmt.Errorf("intent %q needs a registered kind and a payload the kind accepts (%v)", kind, err)
	}
	tx.intents = true
	err = tx.tx.QueryRow(`INSERT INTO intents (kind, payload, next_at, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
		kind, body, stamp(tx.now), stamp(tx.now)).Scan(&id)
	return id, err
}

// PendingIntents counts, from memory, the pending intents of registered kinds.
func (s *State) PendingIntents() int { return int(s.pending.Load()) }

// countIntents refreshes that count, in Open and on the loop after a commit.
// Intents of a kind this binary does not register are named once and left.
func (s *State) countIntents() error {
	var n int64
	var others sql.NullString
	err := s.db.QueryRow(`SELECT count(*) FILTER (WHERE kind IN `+s.mine+`), group_concat(DISTINCT kind) FILTER (WHERE kind NOT IN `+s.mine+`)
		FROM intents WHERE state = 'pending'`, append(append([]any{}, s.names...), s.names...)...).Scan(&n, &others)
	if err != nil {
		return err
	}
	if s.pending.Store(n); others.Valid && !s.logged["intents "+others.String] {
		s.logged["intents "+others.String] = true
		s.logf("leaving intents of kind %s pending: this engine does not know the kind", others.String)
	}
	return nil
}

// work is the worker: it carries out due intents in id order, one at a time.
func (s *State) work() {
	for s.ctx.Err() == nil {
		in, pause, err := s.dueIntent()
		if in != nil {
			s.carry(*in)
			continue
		}
		if err != nil && s.ctx.Err() == nil {
			s.logf("reading intents: %v", err)
			pause = 5 * time.Second
		}
		select {
		case <-s.ctx.Done():
		case <-s.wake:
		case <-time.After(pause):
		}
	}
}

// dueIntent returns the first due pending intent, or how long until one is
// due (an hour with none: a nudge comes first). One this State has not tried,
// its id above boot, is due whatever its next_at: the retry at Open.
func (s *State) dueIntent() (*Intent, time.Duration, error) {
	var in Intent
	var next sql.NullString
	err := s.db.QueryRowContext(s.ctx, `SELECT id, kind, payload, attempts FROM intents WHERE state = 'pending' AND kind IN `+s.mine+` AND (next_at <= ? OR id > ?) ORDER BY id LIMIT 1`,
		append(s.names[:len(s.names):len(s.names)], stamp(s.now()), s.boot)...).Scan(&in.ID, &in.Kind, (*[]byte)(&in.Payload), &in.Attempts)
	if err == nil {
		s.boot = max(s.boot, in.ID)
		return &in, 0, nil
	}
	if err == sql.ErrNoRows {
		err = s.db.QueryRowContext(s.ctx, `SELECT min(next_at) FROM intents WHERE state = 'pending' AND kind IN `+s.mine, s.names...).Scan(&next)
	}
	if err != nil || !next.Valid {
		return nil, time.Hour, err
	}
	at, err := time.Parse(time.RFC3339Nano, next.String)
	return nil, at.Sub(s.now()), err
}

// carry calls Do with no transaction open and records the outcome as a
// command; if that fails, only the recording is tried again. Close ends both.
func (s *State) carry(in Intent) {
	result, doErr := safely("intent", func() (string, error) { return s.intents[in.Kind].Do(s.ctx, in) })
	for pause := 100 * time.Millisecond; s.ctx.Err() == nil; pause = min(2*pause, 5*time.Second) {
		err := s.Submit(s.ctx, s.record(in, result, doErr))
		if re := (recordError{}); errors.As(err, &re) {
			doErr = re.error
			continue
		} else if err == nil || s.ctx.Err() != nil {
			return
		}
		s.logf("intent %d (%s): could not record its outcome, trying again in %s: %v", in.ID, in.Kind, pause, err)
		select {
		case <-s.ctx.Done():
		case <-time.After(pause):
		}
	}
}

// record is the command for one outcome: done (with the kind's Record), pending
// again after 1 s doubling to a minute, or failed at the tenth attempt.
func (s *State) record(in Intent, result string, doErr error) Command {
	return func(tx *Tx) error {
		n, ok, k := in.Attempts+1, doErr == nil, s.intents[in.Kind]
		if ok && k.Record != nil {
			if _, err := safely("record", func() (string, error) { return "", k.Record(tx, in, result) }); err != nil {
				return recordError{fmt.Errorf("recording what it found: %w", err)}
			}
		}
		state, event, next, data := "done", "intent.done", tx.now, map[string]any{"kind": in.Kind, "attempts": n}
		var fail sql.NullString
		if !ok {
			state, event, fail = "failed", "intent.failed", sql.NullString{String: doErr.Error(), Valid: true}
			data["error"] = fail.String
		}
		if !ok && n < maxAttempts {
			state, event, next = "pending", "intent.retry", tx.now.Add(min(time.Second<<(n-1), time.Minute))
		}
		tx.intents = true
		if _, err := tx.tx.Exec(`UPDATE intents SET state = ?, attempts = ?, next_at = ?, last_error = ?, result = ?, done_at = ? WHERE id = ?`,
			state, n, stamp(next), fail, sql.NullString{String: result, Valid: ok}, sql.NullString{String: stamp(tx.now), Valid: ok}, in.ID); err != nil {
			return err
		}
		return tx.Event(event, "intent", in.ID, "", data)
	}
}
