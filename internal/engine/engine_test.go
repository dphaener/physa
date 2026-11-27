//go:build linux || darwin

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/configdir"
	"github.com/dphaener/physa/internal/repos"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/store"
)

// newDir returns <os.MkdirTemp("", "px")>/physa, not yet created, and
// removes that exact temporary directory when the test ends.
func newDir(t *testing.T) string {
	t.Helper()
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	return filepath.Join(tmp, "physa")
}

// running is an engine Run in a goroutine.
type running struct {
	paths  Paths
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
	err    error
}

// start runs an engine on dir and waits until it answers engine.status.
// Cleanup cancels it and waits for Run to return.
func start(t *testing.T, dir string, idle time.Duration) *running {
	t.Helper()
	return startWith(t, Options{Dir: dir, Idle: idle})
}

// startWith is start with every option; Version is always "test".
func startWith(t *testing.T, o Options) *running {
	t.Helper()
	p, err := PathsIn(o.Dir)
	if err != nil {
		t.Fatal(err)
	}
	o.Version = "test"
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{paths: p, cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- Run(ctx, o) }()
	t.Cleanup(func() {
		cancel()
		if _, ok := r.result(5 * time.Second); !ok {
			t.Error("Run did not return after its context was cancelled")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, err := dial(p.Socket); err == nil {
			var s Status
			err = call(c, "engine.status", &s)
			c.Close()
			if err == nil {
				return r
			}
		}
		if _, ok := r.result(20 * time.Millisecond); ok {
			t.Fatalf("Run returned before it answered: %v", r.err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the engine did not answer within 5s")
		}
	}
}

// result waits up to d for Run's return; ok is false if it has not.
func (r *running) result(d time.Duration) (error, bool) {
	select {
	case err := <-r.done:
		r.once.Do(func() { r.err = err; close(r.done) })
		return r.err, true
	case <-time.After(d):
		return nil, false
	}
}

func dial(socket string) (*client.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return client.Dial(ctx, socket)
}

func call(c *client.Client, method string, result any) error {
	return callWith(c, method, nil, result)
}

func callWith(c *client.Client, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Call(ctx, method, params, result)
}

func mustDial(t *testing.T, socket string) *client.Client {
	t.Helper()
	c, err := dial(socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestStatus(t *testing.T) {
	r := start(t, newDir(t), 0)
	c := mustDial(t, r.paths.Socket)
	var raw json.RawMessage
	if err := call(c, "engine.status", &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"repos":[]`) {
		t.Errorf("repos is not [] in %s", raw)
	}
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.PID != os.Getpid() || s.APIRevision != 1 || s.Version != "test" {
		t.Errorf("status = %+v, want pid %d, api_revision 1, version test", s, os.Getpid())
	}
	at, err := time.Parse(time.RFC3339, s.StartedAt)
	if err != nil || !strings.HasSuffix(s.StartedAt, "Z") || time.Since(at) > time.Minute {
		t.Errorf("started_at = %q (%v), want a recent RFC 3339 UTC time", s.StartedAt, err)
	}
	var list []api.MethodSummary
	if err := call(c, "api.list", &list); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range list {
		names = append(names, m.Name)
	}
	if got := strings.Join(names, " "); got != "api.describe api.list engine.events engine.status engine.stop" {
		t.Errorf("api.list names = %s", got)
	}
}

func TestModes(t *testing.T) {
	dir := newDir(t)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := start(t, dir, 0)
	for path, want := range map[string]fs.FileMode{dir: 0o700, r.paths.Socket: 0o600, r.paths.Lock: 0o600} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want {
			t.Errorf("%s has mode %o, want %o", path, st.Mode().Perm(), want)
		}
	}
	b, err := os.ReadFile(r.paths.Lock)
	if err != nil || strings.TrimSpace(string(b)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("lock file holds %q (%v), want our pid", b, err)
	}
}

func TestAlreadyRunning(t *testing.T) {
	dir := newDir(t)
	start(t, dir, 0)
	err := Run(context.Background(), Options{Dir: dir, Version: "test"})
	var re *RunningError
	if !errors.As(err, &re) {
		t.Fatalf("second Run = %v, want *RunningError", err)
	}
	if re.PID != os.Getpid() {
		t.Errorf("RunningError.PID = %d, want %d", re.PID, os.Getpid())
	}
	want := "an engine is already running (pid " + strconv.Itoa(os.Getpid()) + ")"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if (&RunningError{}).Error() != "an engine is already running" {
		t.Errorf("unknown pid error = %q", (&RunningError{}).Error())
	}
}

func TestStaleSocket(t *testing.T) {
	stale := map[string]func(t *testing.T, socket string){
		"file": func(t *testing.T, socket string) {
			if err := os.WriteFile(socket, []byte("left over"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"listener": func(t *testing.T, socket string) {
			ln, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			ln.(*net.UnixListener).SetUnlinkOnClose(false)
			ln.Close()
		},
	}
	for name, leave := range stale {
		t.Run(name, func(t *testing.T) {
			dir := newDir(t)
			if err := configdir.Ensure(dir); err != nil {
				t.Fatal(err)
			}
			p, _ := PathsIn(dir)
			leave(t, p.Socket)
			if _, err := os.Lstat(p.Socket); err != nil {
				t.Fatalf("no stale socket left: %v", err)
			}
			start(t, dir, 0) // fails the test unless it answers
		})
	}
}

func TestStop(t *testing.T) {
	r := start(t, newDir(t), 0)
	other := mustDial(t, r.paths.Socket)
	c := mustDial(t, r.paths.Socket)
	var out json.RawMessage
	if err := call(c, "engine.stop", &out); err != nil {
		t.Fatalf("engine.stop: %v", err)
	}
	if string(out) != "{}" {
		t.Errorf("engine.stop replied %s, want {}", out)
	}
	err, ok := r.result(5 * time.Second)
	if !ok {
		t.Fatal("Run did not return after engine.stop")
	}
	if err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if _, err := os.Lstat(r.paths.Socket); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("socket still there: %v", err)
	}
	if _, err := os.Stat(r.paths.Lock); err != nil {
		t.Errorf("lock file gone: %v", err)
	}
	select {
	case _, open := <-other.Events():
		if open {
			t.Error("an event came instead of the close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the other client was not closed")
	}
	if err := call(other, "engine.status", nil); err == nil {
		t.Error("a call on the closed client succeeded")
	}
}

// awaitIdleExit requires Run to return nil no sooner than idle after from,
// and within 5s.
func awaitIdleExit(t *testing.T, r *running, from time.Time, idle time.Duration) {
	t.Helper()
	err, ok := r.result(5 * time.Second)
	if !ok {
		t.Fatal("the engine did not exit idle within 5s")
	}
	if err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	if took := time.Since(from); took < idle {
		t.Errorf("exited after %v, before its idle %v", took, idle)
	}
	if _, err := os.Lstat(r.paths.Socket); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("socket still there: %v", err)
	}
}

func TestIdleExit(t *testing.T) {
	const idle = 200 * time.Millisecond
	r := start(t, newDir(t), idle) // start's client has disconnected
	awaitIdleExit(t, r, time.Now(), idle/2)
}

func TestClientKeepsUp(t *testing.T) {
	const idle = 200 * time.Millisecond
	r := start(t, newDir(t), idle)
	c, err := dial(r.paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, done := r.result(4 * idle); done {
		c.Close()
		t.Fatalf("the engine exited with a client connected: %v", r.err)
	}
	if err := call(c, "engine.status", nil); err != nil {
		t.Fatalf("engine.status after idle: %v", err)
	}
	if _, done := r.result(2 * idle); done {
		c.Close()
		t.Fatalf("the engine exited with a client connected: %v", r.err)
	}
	c.Close()
	awaitIdleExit(t, r, time.Now(), idle/2)
}

func TestLongPath(t *testing.T) {
	long := "/" + strings.Repeat("d", 100)
	_, err := PathsIn(long)
	if err == nil {
		t.Fatal("PathsIn accepted a socket path over the limit")
	}
	sock := filepath.Join(long, "engine.sock")
	for _, want := range []string{sock, strconv.Itoa(len(sock)) + " bytes", "104 bytes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if _, err := PathsIn("relative/physa"); err == nil {
		t.Error("PathsIn accepted a relative directory")
	}
	p, err := PathsIn("/tmp/px/physa")
	if err != nil || p.Socket != "/tmp/px/physa/engine.sock" || p.Lock != "/tmp/px/physa/engine.lock" || p.Log != "/tmp/px/physa/engine.log" {
		t.Errorf("PathsIn = %+v, %v", p, err)
	}
}

// gitRepo makes a git repo in its own temporary directory, with git's
// settings kept in that directory too, and returns its root with symlinks
// resolved, the way the engine reports it.
func gitRepo(t *testing.T) string {
	t.Helper()
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	root := filepath.Join(tmp, "repo")
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// RepoInput is the input of the test methods: the repo to act on.
type RepoInput struct {
	Repo string `json:"repo" desc:"The repo."`
}

// submitting adds the method name, which submits cmd to the named repo's
// state.
func submitting(name string, cmd state.Command) func(func(context.Context, string) (*state.State, error)) []api.Method {
	return func(get func(context.Context, string) (*state.State, error)) []api.Method {
		return []api.Method{api.Define(name, "Submits a test command.", nil, `{}`, `{}`,
			func(ctx context.Context, _ *api.Conn, in RepoInput) (struct{}, error) {
				st, err := get(ctx, in.Repo)
				if err != nil {
					return struct{}{}, err
				}
				return struct{}{}, st.Submit(ctx, cmd)
			})}
	}
}

func repoStatus(t *testing.T, c *client.Client) []string {
	t.Helper()
	var s Status
	if err := call(c, "engine.status", &s); err != nil {
		t.Fatal(err)
	}
	return s.Repos
}

// storeCount counts the rows of the repo's store that query selects, once
// no engine has the store open.
func storeCount(t *testing.T, repo, query string, args ...any) int {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(repo, ".git", "physa"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var n int
	if err := st.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEventsMethod(t *testing.T) {
	repo := gitRepo(t)
	dir := newDir(t)
	r := startWith(t, Options{Dir: dir, Methods: submitting("test.event", func(tx *state.Tx) error {
		return tx.Event("test.happened", "test", 7, "ann", map[string]int{"n": 1})
	})})
	c := mustDial(t, r.paths.Socket)
	if got := repoStatus(t, c); len(got) != 0 {
		t.Fatalf("repos before any call = %v, want none", got)
	}
	var raw json.RawMessage
	if err := callWith(c, "engine.events", map[string]any{"repo": repo}, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Errorf("events of a new repo = %s, want []", raw)
	}
	if got := repoStatus(t, c); len(got) != 1 || got[0] != repo {
		t.Errorf("repos = %v, want [%s]", got, repo)
	}
	if list, err := repos.List(dir); err != nil || len(list) != 1 || list[0] != repo {
		t.Errorf("registered repos = %v (%v), want [%s]", list, err, repo)
	}
	if err := callWith(c, "test.event", map[string]any{"repo": repo}, nil); err != nil {
		t.Fatal(err)
	}
	var evs []EventOut
	if err := callWith(c, "engine.events", map[string]any{"repo": filepath.Join(repo, "."), "limit": 5}, &evs); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %+v, want one", evs)
	}
	ev := evs[0]
	at, err := time.Parse(time.RFC3339, ev.At)
	if ev.Kind != "test.happened" || ev.Subject != "test" || ev.Person != "ann" || string(ev.Data) != `{"n":1}` ||
		err != nil || !strings.HasSuffix(ev.At, "Z") || time.Since(at) > time.Minute {
		t.Errorf("event = %+v (%v)", ev, err)
	}
}

func TestEventsBadRepo(t *testing.T) {
	notRepo, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(notRepo) })
	t.Setenv("XDG_CONFIG_HOME", notRepo)
	dir := newDir(t)
	r := start(t, dir, 0)
	c := mustDial(t, r.paths.Socket)
	for _, path := range []string{"some/repo", notRepo} {
		err := callWith(c, "engine.events", map[string]any{"repo": path}, nil)
		var ae *api.Error
		if !errors.As(err, &ae) || ae.Code != api.CodeBadRequest || !strings.HasPrefix(ae.Message, path+": ") {
			t.Errorf("engine.events on %s = %v, want bad_request naming it", path, err)
		}
		t.Logf("%s: %v", path, err)
	}
	if got := repoStatus(t, c); len(got) != 0 {
		t.Errorf("repos = %v, want none", got)
	}
	if list, err := repos.List(dir); err != nil || len(list) != 0 {
		t.Errorf("registered repos = %v (%v), want none", list, err)
	}
}

func TestRegisteredRepoReopened(t *testing.T) {
	repo := gitRepo(t)
	dir := newDir(t)
	r := start(t, dir, 0)
	c := mustDial(t, r.paths.Socket)
	if err := callWith(c, "engine.events", map[string]any{"repo": repo}, nil); err != nil {
		t.Fatal(err)
	}
	if err := call(c, "engine.stop", nil); err != nil {
		t.Fatal(err)
	}
	if err, ok := r.result(5 * time.Second); !ok || err != nil {
		t.Fatalf("Run after engine.stop = %v (returned %v)", err, ok)
	}
	r = start(t, dir, 0)
	c = mustDial(t, r.paths.Socket)
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := repoStatus(t, c)
		if len(got) == 1 && got[0] == repo {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("repos after the restart = %v, want [%s]", got, repo)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestIdleWaitsForIntent(t *testing.T) {
	const idle = 200 * time.Millisecond
	repo := gitRepo(t)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	block := state.IntentKind{Name: "test.block", Repeat: "It only waits, so doing it twice changes nothing.",
		Do: func(ctx context.Context, _ state.Intent) (string, error) {
			select {
			case <-release:
				return "ok", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}}
	r := startWith(t, Options{Dir: newDir(t), Idle: idle, State: state.Options{Intents: []state.IntentKind{block}},
		Methods: submitting("test.intent", func(tx *state.Tx) error {
			id, err := tx.Intent("test.block", nil)
			if err != nil {
				return err
			}
			return tx.Event("test.intended", "intent", id, "", nil)
		})})
	c, err := dial(r.paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := callWith(c, "test.intent", map[string]any{"repo": repo}, nil); err != nil {
		c.Close()
		t.Fatal(err)
	}
	c.Close()
	if _, done := r.result(3 * idle); done {
		t.Fatalf("the engine exited idle with an intent pending: %v", r.err)
	}
	free()
	awaitIdleExit(t, r, time.Now(), 0)
	if n := storeCount(t, repo, `SELECT count(*) FROM intents WHERE kind = 'test.block' AND state = 'done'`); n != 1 {
		t.Errorf("%d test.block intents done, want 1", n)
	}
}

func TestIdleCountsTimer(t *testing.T) {
	const idle = 400 * time.Millisecond
	repo := gitRepo(t)
	ring := state.TimerKind{Name: "test.ring", Fire: func(tx *state.Tx, tm state.Timer) error {
		return tx.Event("test.rang", "timer", tm.ID, "", nil)
	}}
	r := startWith(t, Options{Dir: newDir(t), Idle: idle, State: state.Options{Timers: []state.TimerKind{ring}},
		Methods: submitting("test.timer", func(tx *state.Tx) error {
			if err := tx.SetTimer("test.ring", "once", tx.Now().Add(600*time.Millisecond), nil); err != nil {
				return err
			}
			return tx.Event("test.timer_set", "timer", 0, "", nil)
		})})
	set := time.Now()
	c, err := dial(r.paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	err = callWith(c, "test.timer", map[string]any{"repo": repo}, nil)
	c.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, done := r.result(time.Until(set.Add(500 * time.Millisecond))); done {
		t.Fatalf("the engine exited idle with a timer due within its idle: %v", r.err)
	}
	awaitIdleExit(t, r, set, 600*time.Millisecond)
	if n := storeCount(t, repo, `SELECT count(*) FROM events WHERE kind = 'test.rang'`); n != 1 {
		t.Errorf("%d test.rang events, want 1", n)
	}
}

func TestStopWhileOpening(t *testing.T) {
	repo := gitRepo(t)
	dir := newDir(t)
	if err := configdir.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Add(dir, repo); err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	r := start(t, dir, 0)
	c, err := dial(r.paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	err = call(c, "engine.stop", nil)
	c.Close()
	if err != nil {
		t.Fatalf("engine.stop: %v", err)
	}
	if err, ok := r.result(5 * time.Second); !ok || err != nil {
		t.Fatalf("Run after engine.stop = %v (returned %v)", err, ok)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines after Run returned, %d before it started", runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.RemoveAll(filepath.Dir(repo)); err != nil {
		t.Errorf("removing the repo: %v", err)
	}
}
