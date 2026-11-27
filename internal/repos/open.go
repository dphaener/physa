package repos

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/store"
)

// ErrClosed is returned by a Set that has been closed.
var ErrClosed = errors.New("the engine's repos are closed")

// NotServedError is Get's error for a path the engine cannot serve: not
// absolute, not a repo, or a repo of another shape. It names the path.
type NotServedError struct{ Err error }

func (e *NotServedError) Error() string { return e.Err.Error() }
func (e *NotServedError) Unwrap() error { return e.Err }

// Set holds the open state of each registered repo, keyed by store
// directory, so the paths of one repo (its subdirectories and its worktrees)
// share one store and one state.
type Set struct {
	dir     string
	o       state.Options
	add     sync.Mutex // serializes Add, so racing Gets write one line
	opening atomic.Int32

	mu     sync.Mutex // guards closed and open
	closed bool
	open   map[string]*entry
}

// entry is one store, opened once: done closes when the open has finished,
// and the fields below it are read only after that.
type entry struct {
	root  string // the repo root it was first registered by
	done  chan struct{}
	st    *store.Store
	state *state.State
	err   error
}

// NewSet returns a Set for the settings directory dir. It opens nothing;
// each state is opened with o and its own StoreDir.
func NewSet(dir string, o state.Options) *Set {
	return &Set{dir: dir, o: o, open: map[string]*entry{}}
}

// Opening reports whether OpenRegistered is running.
func (s *Set) Opening() bool { return s.opening.Load() > 0 }

// OpenRegistered opens every registered repo, one at a time, each bounded by
// ctx. An entry that is no longer a repo, or whose store fails to open, is
// skipped with an error naming it, and stays in the file.
func (s *Set) OpenRegistered(ctx context.Context) []error {
	s.opening.Add(1)
	defer s.opening.Add(-1)
	paths, err := List(s.dir)
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, p := range paths {
		if _, err := s.get(ctx, p, false); err != nil {
			errs = append(errs, err)
			if errors.Is(err, ErrClosed) || ctx.Err() != nil {
				break
			}
		}
	}
	return errs
}

// Get returns the state of the repo at the absolute path, registering the
// repo's root when it is new and opening its store and state when they are
// not open yet. Concurrent calls for one repo open it once.
func (s *Set) Get(ctx context.Context, path string) (*state.State, error) {
	if !filepath.IsAbs(path) {
		return nil, &NotServedError{fmt.Errorf("%s: the repo path must be absolute", path)}
	}
	return s.get(ctx, filepath.Clean(path), true)
}

// get is the one open path Get and OpenRegistered share.
func (s *Set) get(ctx context.Context, path string, register bool) (*state.State, error) {
	if s.isClosed() {
		return nil, ErrClosed
	}
	sd, err := StoreDir(path)
	if err != nil {
		return nil, &NotServedError{err}
	}
	// The repo's root, shared by its worktrees: <root>/.git/physa.
	gitDir := filepath.Dir(sd)
	if filepath.Base(sd) != "physa" || filepath.Base(gitDir) != ".git" {
		return nil, &NotServedError{fmt.Errorf("%s: the engine serves only repos with a .git directory for now", path)}
	}
	root := filepath.Dir(gitDir)
	if register {
		s.add.Lock()
		_, err := Add(s.dir, root)
		s.add.Unlock()
		if err != nil {
			return nil, fmt.Errorf("%s: register the repo: %w", path, err)
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	e, ok := s.open[sd]
	if !ok {
		e = &entry{root: root, done: make(chan struct{})}
		s.open[sd] = e
		go s.start(sd, e)
	}
	s.mu.Unlock()
	select {
	case <-e.done:
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", path, ctx.Err())
	}
	if e.err != nil {
		return nil, fmt.Errorf("%s: %w", path, e.err)
	}
	return e.state, nil
}

// start opens one store and its state, off any caller, so a caller whose ctx
// ends does not cut the open short for the others. An open that finishes
// after Close is closed again here, and one that fails is forgotten, so a
// later Get tries again.
func (s *Set) start(sd string, e *entry) {
	defer close(e.done)
	st, err := store.Open(context.TODO(), sd)
	if err != nil {
		e.err = fmt.Errorf("open its store: %w", err)
		s.forget(sd, e)
		return
	}
	o := s.o
	o.StoreDir = sd
	stt, err := state.Open(st, o)
	if err != nil {
		e.err = errors.Join(fmt.Errorf("open its state: %w", err), st.Close())
		s.forget(sd, e)
		return
	}
	if s.isClosed() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.err = ErrClosed
		if err := stt.Close(ctx); err != nil {
			e.err = fmt.Errorf("%w; its state did not close, so its store is left open: %v", ErrClosed, err)
			return
		}
		e.err = errors.Join(ErrClosed, st.Close())
		return
	}
	e.st, e.state = st, stt
}

func (s *Set) forget(sd string, e *entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open[sd] == e {
		delete(s.open, sd)
	}
}

func (s *Set) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// ready returns the entries whose state is open.
func (s *Set) ready() []*entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*entry
	for _, e := range s.open {
		select {
		case <-e.done:
			if e.state != nil {
				out = append(out, e)
			}
		default:
		}
	}
	return out
}

// Paths returns the root of each open repo, sorted: one per open store.
func (s *Set) Paths() []string {
	out := []string{}
	for _, e := range s.ready() {
		out = append(out, e.root)
	}
	sort.Strings(out)
	return out
}

// Pending sums the open states' pending intents and finds the earliest of
// their next timers; ok is false when no state has a timer.
func (s *Set) Pending() (intents int, next time.Time, ok bool) {
	for _, e := range s.ready() {
		intents += e.state.PendingIntents()
		if t, has := e.state.NextTimer(); has && (!ok || t.Before(next)) {
			next, ok = t, true
		}
	}
	return intents, next, ok
}

// Close refuses further Gets, waits for the opens in flight, then closes
// every state and the store of each state that closed. A store whose state
// did not close in time is left open, and the error names it.
func (s *Set) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	all := s.open
	s.open = map[string]*entry{}
	s.mu.Unlock()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, e := range all {
		wg.Go(func() {
			var err error
			select {
			case <-e.done:
				if e.state == nil {
					return
				}
				if err = e.state.Close(ctx); err == nil {
					err = e.st.Close()
				} else {
					err = fmt.Errorf("its state did not close, so its store is left open: %w", err)
				}
			case <-ctx.Done():
				err = fmt.Errorf("still opening: %w", ctx.Err())
			}
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", e.root, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}
