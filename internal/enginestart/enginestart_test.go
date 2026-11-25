//go:build linux || darwin

package enginestart

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/engine"
)

// The test binary is the engine when PHYSA_ENGINESTART_ENGINE names its
// directory, so no test needs `go build`.
const (
	envEngine = "PHYSA_ENGINESTART_ENGINE"
	envIdle   = "PHYSA_ENGINESTART_IDLE"
	envFail   = "PHYSA_ENGINESTART_FAIL"
)

// testIdle is long enough that no engine exits idle during a test, and
// short enough that one left by a crashed test run does not stay long.
const testIdle = 30 * time.Second

func TestMain(m *testing.M) {
	if msg := os.Getenv(envFail); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(2)
	}
	if dir := os.Getenv(envEngine); dir != "" {
		idle, _ := time.ParseDuration(os.Getenv(envIdle))
		err := engine.Run(context.Background(), engine.Options{Dir: dir, Idle: idle, Version: "test"})
		var running *engine.RunningError
		if errors.As(err, &running) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// rig is one test's engine directory and every engine process it built.
type rig struct {
	t    *testing.T
	dir  string
	sock string
	mu   sync.Mutex
	cmds []*exec.Cmd
}

// newRig makes <os.MkdirTemp("", "px")>/physa. Cleanup stops every engine
// the test started, by pid, then removes that exact directory.
func newRig(t *testing.T) *rig {
	t.Helper()
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, dir: filepath.Join(tmp, "physa")}
	r.sock = filepath.Join(r.dir, "engine.sock")
	t.Cleanup(func() {
		if s, ok := answering(context.Background(), r.sock, pollTimeout); ok && s.PID != os.Getpid() {
			stopPID(t, s.PID)
		}
		r.mu.Lock()
		cmds := r.cmds
		r.mu.Unlock()
		for _, c := range cmds {
			if c.Process != nil && alive(c.Process.Pid) {
				stopPID(t, c.Process.Pid)
			}
		}
		os.RemoveAll(tmp)
	})
	return r
}

// command builds the test binary as the engine on dir and records it.
func (r *rig) command(dir string, idle time.Duration) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), envEngine+"="+dir, envIdle+"="+idle.String())
	r.mu.Lock()
	r.cmds = append(r.cmds, cmd)
	r.mu.Unlock()
	return cmd
}

func (r *rig) options() Options {
	return Options{Dir: r.dir, Idle: testIdle, Command: r.command}
}

// spawn starts an engine child directly, not through Start. The returned
// channel closes once it has been reaped.
func (r *rig) spawn() (*exec.Cmd, <-chan struct{}) {
	r.t.Helper()
	cmd := r.command(r.dir, testIdle)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	return cmd, done
}

// spawned returns the pids of every engine child started so far.
func (r *rig) spawned() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pids []int
	for _, c := range r.cmds {
		if c.Process != nil {
			pids = append(pids, c.Process.Pid)
		}
	}
	return pids
}

// connect calls Connect and returns its client and engine.status.
func (r *rig) connect() (*client.Client, engine.Status) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, err := Connect(ctx, r.options())
	if err != nil {
		r.t.Fatalf("Connect: %v", err)
	}
	var s engine.Status
	if err := c.Call(ctx, "engine.status", nil, &s); err != nil {
		c.Close()
		r.t.Fatalf("engine.status: %v", err)
	}
	return c, s
}

// alive reports whether pid is a live process (a zombie counts until
// it is reaped).
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err != syscall.ESRCH && err != syscall.EPERM
}

// waitGone waits up to d for pid to be gone.
func waitGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for alive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

// stopPID sends SIGTERM to pid, and SIGKILL after 3s.
func stopPID(t *testing.T, pid int) {
	syscall.Kill(pid, syscall.SIGTERM)
	if waitGone(pid, 3*time.Second) {
		return
	}
	syscall.Kill(pid, syscall.SIGKILL)
	if !waitGone(pid, 3*time.Second) {
		t.Errorf("engine pid %d is still running after SIGKILL", pid)
	}
}

// killPID sends SIGKILL to pid and waits until it is reaped.
func killPID(t *testing.T, pid int) {
	t.Helper()
	syscall.Kill(pid, syscall.SIGKILL)
	if !waitGone(pid, 5*time.Second) {
		t.Fatalf("engine pid %d is still running after SIGKILL", pid)
	}
}

// checkStatus requires a correct engine.status from a live engine that is
// none of the given pids.
func checkStatus(t *testing.T, s engine.Status, not ...int) {
	t.Helper()
	if !alive(s.PID) {
		t.Fatalf("status pid %d is not running", s.PID)
	}
	for _, pid := range not {
		if s.PID == pid {
			t.Fatalf("status pid %d belongs to an engine that was killed", pid)
		}
	}
	if s.APIRevision != 1 || s.Version != "test" || s.Repos == nil || len(s.Repos) != 0 {
		t.Fatalf("status = %+v, want api_revision 1, version test, repos []", s)
	}
}

func TestConnectStarts(t *testing.T) {
	r := newRig(t)
	c, s := r.connect()
	defer c.Close()
	pids := r.spawned()
	if len(pids) != 1 || pids[0] != s.PID {
		t.Fatalf("spawned %v, status pid %d: want one child that answers", pids, s.PID)
	}
	checkStatus(t, s)
	if fi, err := os.Stat(filepath.Join(r.dir, "engine.log")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("engine.log: %v, %v; want mode 0600", fi, err)
	}
}

func TestConnectReuses(t *testing.T) {
	r := newRig(t)
	c1, s1 := r.connect()
	c1.Close()
	c2, s2 := r.connect()
	defer c2.Close()
	if s1.PID != s2.PID {
		t.Fatalf("second Connect reached pid %d, want %d", s2.PID, s1.PID)
	}
	if n := len(r.spawned()); n != 1 {
		t.Fatalf("spawned %d children, want 1", n)
	}
	started, err := Start(context.Background(), r.options())
	if err != nil || started {
		t.Fatalf("Start against a running engine = %v, %v; want false, nil", started, err)
	}
}

func TestConcurrentStarts(t *testing.T) {
	r := newRig(t)
	const n = 8
	var wg sync.WaitGroup
	started := make([]bool, n)
	pids := make([]int, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if started[i], errs[i] = Start(ctx, r.options()); errs[i] != nil {
				return
			}
			s, ok := answering(ctx, r.sock, callTimeout)
			if !ok {
				errs[i] = errors.New("no engine answers after Start")
			}
			pids[i] = s.PID
		}()
	}
	wg.Wait()
	winners := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("Start %d: %v", i, errs[i])
		}
		if pids[i] != pids[0] {
			t.Fatalf("statuses report pids %v, want one", pids)
		}
		if started[i] {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d Starts report started, want 1", winners)
	}
	for _, pid := range r.spawned() {
		if pid != pids[0] && !waitGone(pid, 3*time.Second) {
			t.Fatalf("spawned child %d is still running beside engine %d", pid, pids[0])
		}
	}
	t.Logf("%d children spawned, one engine (pid %d)", len(r.spawned()), pids[0])
}

func TestStopTwice(t *testing.T) {
	r := newRig(t)
	c, s := r.connect()
	c.Close()
	ctx := context.Background()
	stopped, err := Stop(ctx, r.dir)
	if err != nil || !stopped {
		t.Fatalf("Stop = %v, %v; want true, nil", stopped, err)
	}
	if alive(s.PID) {
		t.Fatalf("engine pid %d is still running after Stop", s.PID)
	}
	if _, ok := answering(ctx, r.sock, pollTimeout); ok {
		t.Fatal("the socket still answers after Stop")
	}
	stopped, err = Stop(ctx, r.dir)
	if err != nil || stopped {
		t.Fatalf("second Stop = %v, %v; want false, nil", stopped, err)
	}
}

func TestStopInProcess(t *testing.T) {
	r := newRig(t)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- engine.Run(ctx, engine.Options{Dir: r.dir, Idle: testIdle, Version: "test"}) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if s, ok := answering(ctx, r.sock, pollTimeout); ok && s.PID == os.Getpid() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the in-process engine did not answer within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	begun := time.Now()
	stopped, err := Stop(context.Background(), r.dir)
	if err != nil || !stopped {
		t.Fatalf("Stop = %v, %v; want true, nil", stopped, err)
	}
	if d := time.Since(begun); d > 3*time.Second {
		t.Fatalf("Stop took %s", d)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Stop")
	}
	if stopped, err := Stop(context.Background(), r.dir); err != nil || stopped {
		t.Fatalf("second Stop = %v, %v; want false, nil", stopped, err)
	}
}

func TestStartFailure(t *testing.T) {
	r := newRig(t)
	o := r.options()
	o.Command = func(dir string, idle time.Duration) *exec.Cmd {
		cmd := r.command(dir, idle)
		cmd.Env = append(cmd.Env, envFail+"=the engine could not open its store")
		return cmd
	}
	_, err := Start(context.Background(), o)
	if err == nil {
		t.Fatal("Start succeeded with a child that fails")
	}
	for _, want := range []string{"code 2", "the engine could not open its store", filepath.Join(r.dir, "engine.log")} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
	if n := len(r.spawned()); n != 1 {
		t.Fatalf("spawned %d children after a real failure, want 1", n)
	}
}

// firstAnswer measures how long a spawned child takes to answer its
// first engine.status, then kills it.
func firstAnswer(t *testing.T, r *rig) (time.Duration, int) {
	t.Helper()
	begun := time.Now()
	cmd, done := r.spawn()
	for {
		if s, ok := answering(context.Background(), r.sock, pollTimeout); ok && s.PID == cmd.Process.Pid {
			break
		}
		if time.Since(begun) > 10*time.Second {
			t.Fatal("the child did not answer within 10s")
		}
		time.Sleep(2 * time.Millisecond)
	}
	took := time.Since(begun)
	t.Logf("a child answers its first engine.status after %s", took)
	return took, killChild(t, cmd, done)
}

// killChild sends SIGKILL to a child spawned directly, by its pid, and
// waits until it is reaped.
func killChild(t *testing.T, cmd *exec.Cmd, done <-chan struct{}) int {
	t.Helper()
	cmd.Process.Signal(syscall.SIGKILL)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("child %d was not reaped after SIGKILL", cmd.Process.Pid)
	}
	return cmd.Process.Pid
}

func seeded(t *testing.T) *rand.Rand {
	seed := time.Now().UnixNano()
	t.Logf("seed %d", seed)
	return rand.New(rand.NewSource(seed))
}

func TestKillDuringStart(t *testing.T) {
	r := newRig(t)
	took, prev := firstAnswer(t, r)
	rng := seeded(t)
	for i := range 20 {
		if alive(prev) {
			killPID(t, prev)
		}
		if _, ok := answering(context.Background(), r.sock, pollTimeout); ok {
			t.Fatalf("iteration %d: an engine still answers after its kill", i)
		}
		delay := time.Duration(rng.Int63n(int64(took)*3/2 + 1))
		cmd, done := r.spawn()
		time.Sleep(delay)
		killed := killChild(t, cmd, done)
		c, s := r.connect()
		c.Close()
		checkStatus(t, s, killed, prev)
		t.Logf("iteration %d: killed child %d after %s; engine %d answers", i, killed, delay, s.PID)
		prev = s.PID
	}
}

func TestKillDuringCall(t *testing.T) {
	r := newRig(t)
	took, _ := firstAnswer(t, r)
	rng := seeded(t)
	for i := range 20 {
		c, s := r.connect()
		calls := make(chan int, 1)
		go func() {
			n := 0
			for {
				ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
				err := c.Call(ctx, "engine.status", nil, nil)
				cancel()
				if err != nil {
					calls <- n
					return
				}
				n++
			}
		}()
		delay := time.Duration(rng.Int63n(int64(took) + 1))
		time.Sleep(delay)
		killPID(t, s.PID)
		var n int
		select {
		case n = <-calls:
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: a call did not fail after the engine was killed", i)
		}
		c.Close()
		c2, s2 := r.connect()
		c2.Close()
		checkStatus(t, s2, s.PID)
		t.Logf("iteration %d: killed engine %d after %s and %d calls; engine %d answers", i, s.PID, delay, n, s2.PID)
	}
}
