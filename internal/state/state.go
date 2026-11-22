// Package state is the engine's one writer for a repo's store: every change to ideas, pitches, bets, plans, intents and timers is a command applied one at a time in one transaction that appends its event. Reads come from the state tables; events are never replayed to answer one.
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dphaener/physa/internal/store"
)

var (
	// ErrClosed is returned by Submit once the State is closed.
	ErrClosed = errors.New("the repo's state is closed")
	// ErrNoEvent refuses a command that changed state rows and appended no event.
	ErrNoEvent = errors.New("the command changed state and appended no event; nothing was written")
	errTxDone  = errors.New("the command has returned; its Tx is no longer usable")
)

// Options configures a State. Now defaults to time.Now. Timers and Intents
// hold extra kinds; the retention timer and log.append are always present.
type Options struct {
	StoreDir string
	Now      func() time.Time
	Timers   []TimerKind
	Intents  []IntentKind
}

// Command is one state change, run on the loop inside one write transaction.
// It uses only its Tx: calling Submit, Events or Close would wait on itself.
type Command func(tx *Tx) error

// State is the one writer for a store's engine tables: a loop that applies
// commands and fires due timers one at a time, and a worker for intents.
type State struct {
	db       *sql.DB
	storeDir string
	now      func() time.Time
	kinds    map[string]TimerKind
	intents  map[string]IntentKind
	cmds     chan func()     // unbuffered: a command sent is one the loop has taken
	pokes    chan struct{}   // wake the loop to look at its timers
	wake     chan struct{}   // wake the worker to look at its intents
	done     chan struct{}   // closed when the loop and the worker have stopped
	ctx      context.Context // ended by Close: no more commands, and a Do is cut short
	cancel   context.CancelFunc
	pending  atomic.Int64 // pending intents of the registered kinds
	names    []any        // the registered intent kinds, as SQL arguments
	mine     string       // "(?, ?)": one mark per name
	boot     int64        // worker only: see dueIntent

	mu      sync.Mutex // guards next and hasNext
	next    time.Time
	hasNext bool

	// Used by the loop only (and by Open before the loop starts).
	fails  map[[2]string]int   // consecutive failed fires per (kind, key)
	held   map[int64]time.Time // backoffs that could not be written to the row
	logged map[string]bool     // kinds already logged as unknown
}

// Open ensures the retention timer, loads the next due time and the pending
// count, and starts the loop and the worker. Close it before the store.
func Open(st *store.Store, o Options) (*State, error) {
	s := &State{
		db: st.DB(), storeDir: o.StoreDir, now: o.Now,
		kinds: map[string]TimerKind{retentionKind: {Name: retentionKind, Fire: expireEvents}},
		cmds:  make(chan func()), pokes: make(chan struct{}, 1),
		done: make(chan struct{}), wake: make(chan struct{}, 1),
		fails: map[[2]string]int{}, held: map[int64]time.Time{}, logged: map[string]bool{},
	}
	if s.now == nil {
		s.now = time.Now
	}
	for _, k := range o.Timers {
		if _, dup := s.kinds[k.Name]; dup || k.Name == "" || k.Fire == nil {
			return nil, fmt.Errorf("timer kind %q needs a name of its own and a Fire function", k.Name)
		}
		s.kinds[k.Name] = k
	}
	if err := s.register(o.Intents); err != nil {
		return nil, err
	}
	// A timer row only, so no event; an existing row keeps its due time.
	if _, err := s.db.Exec(`INSERT INTO timers (kind, key, due_at) VALUES (?, ?, ?) ON CONFLICT (kind, key) DO NOTHING`,
		retentionKind, retentionKey, stamp(s.now().Add(retentionEvery))); err != nil {
		return nil, fmt.Errorf("ensure the retention timer: %w", err)
	}
	if _, err := s.load(); err != nil {
		return nil, err
	}
	if err := s.countIntents(); err != nil {
		return nil, fmt.Errorf("count the pending intents: %w", err)
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(s.loop)
	wg.Go(s.work)
	go func() { wg.Wait(); close(s.done) }()
	return s, nil
}

// Submit hands c to the loop and waits for its commit. A command error or
// panic rolls back and is returned. After Close, or when ctx ends before the
// loop took c, nothing is applied; once taken, c is applied whatever ctx does.
func (s *State) Submit(ctx context.Context, c Command) error {
	if s.ctx.Err() != nil {
		return ErrClosed
	} else if err := ctx.Err(); err != nil {
		return err
	}
	res := make(chan error, 1)
	select {
	case s.cmds <- func() { res <- s.apply(c) }:
		return <-res
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return ErrClosed
	}
}

// Close stops taking commands and cuts short the intent in hand. It returns
// once the loop and the worker have stopped, or with ctx's error.
func (s *State) Close(ctx context.Context) error {
	s.cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// poke makes the loop look at its timers and the worker at its intents now.
// It is the clock seam: a test moves its fake clock and pokes.
func (s *State) poke() { nudge(s.pokes); nudge(s.wake) }

func nudge(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

func (s *State) loop() {
	wait := time.NewTimer(time.Hour)
	defer wait.Stop()
	for {
		s.fireDue()
		var wake <-chan time.Time
		if next, ok := s.NextTimer(); ok {
			wait.Reset(max(next.Sub(s.now()), 0))
			wake = wait.C
		}
		select {
		case <-s.ctx.Done():
			return
		case apply := <-s.cmds:
			apply()
		case <-s.pokes:
		case <-wake:
		}
	}
}

// apply runs c in one write transaction (the store's DSN takes the write
// lock at BEGIN). The store has one connection: s.db waits until the commit.
func (s *State) apply(c Command) error {
	sqlTx, err := s.db.Begin()
	if err != nil {
		return err
	}
	tx := &Tx{s: s, tx: sqlTx, now: s.now(), set: map[[2]string]bool{}}
	_, err = safely("command", func() (string, error) { return "", c(tx) })
	tx.done.Store(true)
	if err == nil && tx.changed && !tx.evented {
		err = ErrNoEvent
	}
	if err == nil {
		err = sqlTx.Commit()
	}
	if err != nil {
		sqlTx.Rollback()
		return err
	}
	if tx.timers {
		s.refresh()
	}
	if tx.intents {
		if err := s.countIntents(); err != nil {
			s.logf("counting pending intents: %v", err)
		}
		nudge(s.wake)
	}
	return nil
}

// safely runs f and turns a panic into an error.
func safely(what string, f func() (string, error)) (res string, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the %s panicked: %v", what, p)
		}
	}()
	return f()
}

// Tx is a command's view of its transaction: state rows, the event, intents,
// timers and the clock. It is refused once the command has returned.
type Tx struct {
	s    *State
	tx   *sql.Tx
	now  time.Time
	done atomic.Bool

	changed, evented, timers, intents bool // a state row, an event, a timer row, an intent row was written
	set                               map[[2]string]bool
}

// Now is the command's time: one instant for the whole transaction.
func (tx *Tx) Now() time.Time { return tx.now }

// Exec runs a statement on the state tables. A write that changed a row is a
// state change, and the command must then append an Event.
func (tx *Tx) Exec(query string, args ...any) (sql.Result, error) {
	if tx.done.Load() {
		return nil, errTxDone
	}
	res, err := tx.tx.Exec(query, args...)
	if err == nil && isWrite(query) {
		n, _ := res.RowsAffected()
		tx.changed = tx.changed || n > 0
	}
	return res, err
}

// Query reads rows. A write given to it (with RETURNING) counts as a state
// change whether or not it changed a row, since no count comes back.
func (tx *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	if tx.done.Load() {
		return nil, errTxDone
	}
	tx.changed = tx.changed || isWrite(query)
	return tx.tx.Query(query, args...)
}

// QueryRow reads one row, with Query's rule for a write.
func (tx *Tx) QueryRow(query string, args ...any) *sql.Row {
	if !tx.done.Load() {
		tx.changed = tx.changed || isWrite(query)
	}
	return tx.tx.QueryRow(query, args...)
}

// Event appends the command's event, at the command's time. A subjectID of 0
// and a person of "" are stored as NULL. data is a JSON object; nil is {}.
func (tx *Tx) Event(kind, subject string, subjectID int64, person string, data any) error {
	if tx.done.Load() {
		return errTxDone
	}
	body, err := marshal(data)
	if err != nil || kind == "" || subject == "" || !strings.HasPrefix(body, "{") {
		return fmt.Errorf("event %q needs a kind, a subject and data that is a JSON object (got %s, %v)", kind, body, err)
	}
	_, err = tx.tx.Exec(`INSERT INTO events (at, kind, subject, subject_id, person, data) VALUES (?, ?, ?, ?, ?, ?)`,
		tx.now.UTC().Format(time.RFC3339), kind, subject, sql.NullInt64{Int64: subjectID, Valid: subjectID != 0},
		sql.NullString{String: person, Valid: person != ""}, body)
	tx.evented = tx.evented || err == nil
	return err
}

func marshal(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

var (
	firstWord = regexp.MustCompile(`(?is)^(?:\s+|--[^\n]*|/\*.*?\*/)*(\w+)`)
	writeWord = regexp.MustCompile(`(?i)\b(?:insert|update|delete|replace)\b`)
)

// isWrite reports whether SQL, after leading space and comments, starts with
// INSERT, UPDATE, DELETE or REPLACE, or with WITH and holds one of them.
func isWrite(query string) bool {
	m := firstWord.FindStringSubmatch(query)
	return m != nil && (writeWord.MatchString(m[1]) || (strings.EqualFold(m[1], "with") && writeWord.MatchString(query)))
}

// Event is one row of the events table.
type Event struct {
	At            time.Time
	Kind, Subject string
	SubjectID     int64
	Person        string
	Data          json.RawMessage
}

// Events returns the newest events first (by id: at has one-second
// precision), from the table. limit is clamped to 1..500, and 0 means 50.
func (s *State) Events(ctx context.Context, limit int) ([]Event, error) {
	if limit == 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT at, kind, subject, coalesce(subject_id, 0), coalesce(person, ''), data FROM events ORDER BY id DESC LIMIT ?`,
		min(max(limit, 1), 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var at string
		if err := rows.Scan(&at, &e.Kind, &e.Subject, &e.SubjectID, &e.Person, (*[]byte)(&e.Data)); err != nil {
			return nil, err
		}
		if e.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *State) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "physa engine: "+format+"\n", args...)
}
