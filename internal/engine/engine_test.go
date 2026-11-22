//go:build linux || darwin

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/apiclient"
	"github.com/dphaener/physa/internal/configdir"
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
	p, err := PathsIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{paths: p, cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- Run(ctx, Options{Dir: dir, Idle: idle, Version: "test"}) }()
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

func dial(socket string) (*apiclient.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return apiclient.Dial(ctx, socket)
}

func call(c *apiclient.Client, method string, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Call(ctx, method, nil, result)
}

func mustDial(t *testing.T, socket string) *apiclient.Client {
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
	if got := strings.Join(names, " "); got != "api.describe api.list engine.status engine.stop" {
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
