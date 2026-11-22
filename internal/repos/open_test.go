package repos

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/state"
)

// resolved resolves symlinks, since git reports the resolved path (macOS temp
// dirs are symlinks).
func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func newSet(t *testing.T, dir string, o state.Options) *Set {
	t.Helper()
	s := NewSet(dir, o)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func get(t *testing.T, s *Set, path string) *state.State {
	t.Helper()
	st, err := s.Get(context.Background(), path)
	if err != nil || st == nil {
		t.Fatalf("Get(%s) = %v, %v", path, st, err)
	}
	return st
}

func wantLines(t *testing.T, dir string, want ...string) {
	t.Helper()
	got, err := List(dir)
	if err != nil || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("repos file = %q, %v; want %q", got, err, want)
	}
}

func wantPaths(t *testing.T, s *Set, want ...string) {
	t.Helper()
	if got := s.Paths(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("Paths = %q; want %q", got, want)
	}
}

func TestSetGetRegisters(t *testing.T) {
	dir, repo := t.TempDir(), gitInit(t)
	s := newSet(t, dir, state.Options{})
	wantPaths(t, s)
	a := get(t, s, repo)
	if b := get(t, s, repo); a != b {
		t.Fatal("a second Get gave another state")
	}
	wantLines(t, dir, resolved(t, repo))
	wantPaths(t, s, resolved(t, repo))
	if _, err := s.Get(context.Background(), "relative/repo"); err == nil || !strings.Contains(err.Error(), "relative/repo") {
		t.Fatalf("relative Get = %v; want an error naming it", err)
	}
}

func TestSetReopensRegistered(t *testing.T) {
	dir, repo := t.TempDir(), gitInit(t)
	first := NewSet(dir, state.Options{})
	get(t, first, repo)
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := newSet(t, dir, state.Options{})
	if s.Opening() {
		t.Fatal("Opening before OpenRegistered")
	}
	if errs := s.OpenRegistered(context.Background()); len(errs) != 0 {
		t.Fatalf("OpenRegistered = %v", errs)
	}
	if s.Opening() {
		t.Fatal("Opening after OpenRegistered returned")
	}
	wantPaths(t, s, resolved(t, repo))
	wantLines(t, dir, resolved(t, repo))
}

func TestSetSkipsMissingRepo(t *testing.T) {
	dir, repo, plain := t.TempDir(), gitInit(t), t.TempDir()
	gone := filepath.Join(t.TempDir(), "gone")
	for _, p := range []string{plain, gone, resolved(t, repo)} {
		if _, err := Add(dir, p); err != nil {
			t.Fatal(err)
		}
	}
	s := newSet(t, dir, state.Options{})
	errs := s.OpenRegistered(context.Background())
	if len(errs) != 2 || !strings.Contains(errs[0].Error(), plain) || !strings.Contains(errs[1].Error(), gone) {
		t.Fatalf("OpenRegistered = %v; want two errors naming %s and %s", errs, plain, gone)
	}
	t.Logf("skipped: %v", errs)
	wantPaths(t, s, resolved(t, repo))
	wantLines(t, dir, plain, gone, resolved(t, repo))
}

func TestSetTwoPathsOneStore(t *testing.T) {
	dir, repo := t.TempDir(), gitInit(t)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	s := newSet(t, dir, state.Options{})
	if get(t, s, sub) != get(t, s, repo) {
		t.Fatal("a repo and its subdirectory got two states")
	}
	wantLines(t, dir, resolved(t, repo))
	wantPaths(t, s, resolved(t, repo))
}

func TestSetRefusesOtherShape(t *testing.T) {
	dir, bare := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	s := newSet(t, dir, state.Options{})
	_, err := s.Get(context.Background(), bare)
	want := bare + ": the engine serves only repos with a .git directory for now"
	if err == nil || err.Error() != want {
		t.Fatalf("Get(bare) = %v; want %q", err, want)
	}
	t.Logf("refused: %v", err)
	wantLines(t, dir)
	wantPaths(t, s)
}

func TestSetClosedRefusesGet(t *testing.T) {
	dir, repo := t.TempDir(), gitInit(t)
	s := NewSet(dir, state.Options{})
	st := get(t, s, repo)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), repo); !errors.Is(err, ErrClosed) {
		t.Fatalf("Get after Close = %v; want ErrClosed", err)
	}
	if errs := s.OpenRegistered(context.Background()); len(errs) != 1 || !errors.Is(errs[0], ErrClosed) {
		t.Fatalf("OpenRegistered after Close = %v; want ErrClosed", errs)
	}
	if err := st.Submit(context.Background(), func(*state.Tx) error { return nil }); !errors.Is(err, state.ErrClosed) {
		t.Fatalf("Submit on a closed Set's state = %v; want state.ErrClosed", err)
	}
	wantPaths(t, s)
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("second Close = %v", err)
	}
}

func TestSetOpenRegisteredAndGetOnce(t *testing.T) {
	for i := range 5 {
		dir, repo := t.TempDir(), gitInit(t)
		if _, err := Add(dir, resolved(t, repo)); err != nil {
			t.Fatal(err)
		}
		s := newSet(t, dir, state.Options{})
		var wg sync.WaitGroup
		var errs []error
		wg.Go(func() { errs = s.OpenRegistered(context.Background()) })
		got := get(t, s, repo)
		wg.Wait()
		if len(errs) != 0 {
			t.Fatalf("round %d: OpenRegistered = %v", i, errs)
		}
		if again := get(t, s, repo); again != got {
			t.Fatalf("round %d: OpenRegistered and Get gave two states", i)
		}
		wantPaths(t, s, resolved(t, repo))
		wantLines(t, dir, resolved(t, repo))
	}
}

func TestSetGetConcurrent(t *testing.T) {
	dir, repo := t.TempDir(), gitInit(t)
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	s := newSet(t, dir, state.Options{})
	got := make([]*state.State, 8)
	var wg sync.WaitGroup
	for i := range got {
		p := repo
		if i%2 == 1 {
			p = sub
		}
		wg.Go(func() {
			st, err := s.Get(context.Background(), p)
			if err != nil {
				t.Errorf("Get(%s): %v", p, err)
			}
			got[i] = st
		})
	}
	wg.Wait()
	for i, st := range got {
		if st == nil || st != got[0] {
			t.Fatalf("Get %d gave state %p; want %p", i, st, got[0])
		}
	}
	wantLines(t, dir, resolved(t, repo))
	wantPaths(t, s, resolved(t, repo))
}

func TestSetPending(t *testing.T) {
	release := make(chan struct{})
	block := state.IntentKind{
		Name:   "test.block",
		Repeat: "the test kind changes nothing",
		Do: func(ctx context.Context, in state.Intent) (string, error) {
			select {
			case <-release:
				return "ok", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	s := newSet(t, t.TempDir(), state.Options{Intents: []state.IntentKind{block}})
	if n, _, ok := s.Pending(); n != 0 || ok {
		t.Fatalf("empty Set Pending = %d, %v; want 0, false", n, ok)
	}
	start := time.Now()
	for _, repo := range []string{gitInit(t), gitInit(t)} {
		st := get(t, s, repo)
		err := st.Submit(context.Background(), func(tx *state.Tx) error {
			_, err := tx.Intent("test.block", nil)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	n, next, ok := s.Pending()
	if n != 2 || !ok {
		t.Fatalf("Pending = %d, %v, %v; want 2 intents and a next timer", n, next, ok)
	}
	// The retention timer of each fresh store is due a day after it opened.
	if day := start.Add(24 * time.Hour); next.Before(day.Add(-time.Second)) || next.After(time.Now().Add(24*time.Hour+time.Second)) {
		t.Fatalf("next timer %v; want about %v", next, day)
	}
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for n, _, _ := s.Pending(); n != 0; n, _, _ = s.Pending() {
		if time.Now().After(deadline) {
			t.Fatalf("Pending still %d after release", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSetCloseDuringGet(t *testing.T) {
	for i := range 10 {
		dir, repo := t.TempDir(), gitInit(t)
		s := NewSet(dir, state.Options{})
		var wg sync.WaitGroup
		var st *state.State
		var err error
		wg.Go(func() { st, err = s.Get(context.Background(), repo) })
		if cerr := s.Close(context.Background()); cerr != nil {
			t.Fatalf("round %d: Close = %v", i, cerr)
		}
		wg.Wait()
		if err == nil {
			// The open finished before Close: Close closed it.
			if serr := st.Submit(context.Background(), func(*state.Tx) error { return nil }); !errors.Is(serr, state.ErrClosed) {
				t.Fatalf("round %d: a state kept open after Close: %v", i, serr)
			}
		} else if !errors.Is(err, ErrClosed) {
			t.Fatalf("round %d: Get = %v; want ErrClosed or a closed state", i, err)
		}
		wantPaths(t, s)
	}
}
