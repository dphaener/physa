//go:build linux || darwin

package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/store"
)

// The test binary is the engine when PHYSA_CRASH_ENGINE names its
// directory, so the kill test needs no `go build`.
const envCrashEngine = "PHYSA_CRASH_ENGINE"

// crashIdle is long enough that no engine exits idle during a round, and
// short enough that one left by a crashed test run does not hold the lock
// for long.
const crashIdle = 30 * time.Second

func TestMain(m *testing.M) {
	dir := os.Getenv(envCrashEngine)
	if dir == "" {
		os.Exit(m.Run())
	}
	// SIGTERM stops the engine the way `physa engine` does; SIGKILL is the crash.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := Run(ctx, Options{Env: os.Environ(), Dir: dir, Idle: crashIdle, Version: "test", Methods: crashMethods})
	stop()
	var running *RunningError
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

// writeInput is test.write's input.
type writeInput struct {
	Repo string `json:"repo" desc:"The repo."`
	N    int    `json:"n" desc:"The write's number."`
}

// drainOutput is test.drain's output.
type drainOutput struct {
	Found int `json:"found" desc:"The repo's pending intents when the call arrived."`
}

// writeCmd is one write of the kill test: idea kill-<n>, its idea.added
// event and a log.append intent, in one command.
func writeCmd(n int) state.Command {
	return func(tx *state.Tx) error {
		at := tx.Now().UTC().Format(time.RFC3339)
		res, err := tx.Exec(`INSERT INTO ideas (slug, text, person, created_at, updated_at) VALUES (?, ?, 'test', ?, ?)`,
			"kill-"+strconv.Itoa(n), strconv.Itoa(n), at, at)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := tx.Event("idea.added", "idea", id, "test", map[string]any{"slug": "kill-" + strconv.Itoa(n)}); err != nil {
			return err
		}
		_, err = tx.Intent("log.append", map[string]string{"file": "kill.log", "text": strconv.Itoa(n)})
		return err
	}
}

// crashMethods are the spawned engine's test methods, added through
// Options.Methods: never part of the shipped table.
func crashMethods(get func(context.Context, string) (*state.State, error)) []api.Method {
	write := api.Define("test.write", "Writes idea kill-<n>, its event and a log.append intent.", nil, `{}`, `{}`,
		func(ctx context.Context, _ *api.Conn, in writeInput) (struct{}, error) {
			st, err := get(ctx, in.Repo)
			if err != nil {
				return struct{}{}, err
			}
			return struct{}{}, st.Submit(ctx, writeCmd(in.N))
		})
	drain := api.Define("test.drain", "Waits, up to 10s, until the repo has no pending intents.", nil, `{}`, `{"found":0}`,
		func(ctx context.Context, _ *api.Conn, in RepoInput) (drainOutput, error) {
			st, err := get(ctx, in.Repo)
			if err != nil {
				return drainOutput{}, err
			}
			out := drainOutput{Found: st.PendingIntents()}
			deadline := time.Now().Add(10 * time.Second)
			for st.PendingIntents() > 0 {
				if time.Now().After(deadline) {
					return out, fmt.Errorf("%d intents still pending after 10s", st.PendingIntents())
				}
				select {
				case <-ctx.Done():
					return out, ctx.Err()
				case <-time.After(5 * time.Millisecond):
				}
			}
			return out, nil
		})
	return []api.Method{write, drain}
}

// crashRig is the kill test's engine directory and every engine it spawned.
type crashRig struct {
	t    *testing.T
	dir  string
	log  *os.File
	mu   sync.Mutex
	cmds []*exec.Cmd
}

// child is one spawned engine; done closes once it has been reaped.
type child struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (c *child) pid() int { return c.cmd.Process.Pid }

// newCrashRig makes the engine directory in a MkdirTemp directory of its
// own. Cleanup kills every child still alive, by pid, before newDir's
// cleanup removes that directory.
func newCrashRig(t *testing.T) *crashRig {
	t.Helper()
	r := &crashRig{t: t, dir: newDir(t)}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(r.dir), "engines.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	r.log = f
	t.Cleanup(func() {
		r.mu.Lock()
		cmds := r.cmds
		r.mu.Unlock()
		for _, c := range cmds {
			if pid := c.Process.Pid; alive(pid) {
				syscall.Kill(pid, syscall.SIGKILL)
				if !waitGone(pid, 5*time.Second) {
					t.Errorf("engine pid %d is still running after SIGKILL", pid)
				}
			}
		}
		f.Close()
	})
	return r
}

// spawn starts the test binary as an engine on the rig's directory and
// waits, until ctx ends, for it to answer engine.status with its own pid.
func (r *crashRig) spawn(ctx context.Context) (*child, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), envCrashEngine+"="+r.dir)
	cmd.Stdout, cmd.Stderr = r.log, r.log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cmds = append(r.cmds, cmd)
	r.mu.Unlock()
	c := &child{cmd: cmd, done: make(chan struct{})}
	go func() { cmd.Wait(); close(c.done) }()
	sock := filepath.Join(r.dir, "engine.sock")
	for {
		if cl, err := dial(sock); err == nil {
			var s Status
			err = callWith(cl, "engine.status", nil, &s)
			cl.Close()
			if err == nil && s.PID == c.pid() {
				return c, nil
			}
		}
		select {
		case <-c.done:
			return nil, fmt.Errorf("engine pid %d exited before it answered (%v); see %s", c.pid(), cmd.ProcessState, r.log.Name())
		case <-ctx.Done():
			return c, fmt.Errorf("engine pid %d did not answer: %w", c.pid(), ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// kill sends SIGKILL to c by pid and waits until it is reaped.
func (c *child) kill() error {
	syscall.Kill(c.pid(), syscall.SIGKILL)
	select {
	case <-c.done:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("engine pid %d was not reaped after SIGKILL", c.pid())
	}
}

// stop sends SIGTERM to c by pid, SIGKILL after 3s, and waits until it has
// gone, so the next spawn never meets the lock.
func (c *child) stop() error {
	syscall.Kill(c.pid(), syscall.SIGTERM)
	select {
	case <-c.done:
		return nil
	case <-time.After(3 * time.Second):
	}
	return c.kill()
}

// alive reports whether pid is a live process (a zombie counts until it
// is reaped).
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

// envInt reads a whole number from the environment, or def when unset.
func envInt(t *testing.T, name string, def int64) int64 {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		t.Fatalf("%s=%q is not a whole number", name, v)
	}
	return n
}

// TestKillAnyMoment kills the engine with SIGKILL at a random moment while
// four clients write, then restarts it, lets it carry out every pending
// intent, and checks that state, events, intents and the log file agree.
// PHYSA_KILL_RUNS sets the rounds (20) and PHYSA_KILL_SEED the seed (the
// time). The seed drives only the kill delay: the moment the kill lands
// still depends on scheduling, so a seed narrows a failure, it does not
// replay it.
func TestKillAnyMoment(t *testing.T) {
	rounds := int(envInt(t, "PHYSA_KILL_RUNS", 20))
	seed := envInt(t, "PHYSA_KILL_SEED", time.Now().UnixNano())
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewSource(seed))
	repo := gitRepo(t)
	storeDir := filepath.Join(repo, ".git", "physa")
	r := newCrashRig(t)

	var next atomic.Int64
	var acked []int
	var midFlight, ahead int
	for round := range rounds {
		fail := func(format string, args ...any) {
			t.Helper()
			t.Fatalf("seed %d round %d: %s", seed, round, fmt.Sprintf(format, args...))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		got, left, lines := killRound(ctx, r, repo, storeDir, &next, rng, fail)
		acked = append(acked, got...)
		if left > 0 {
			midFlight++
		}
		if lines > 0 {
			ahead++
		}

		eng, err := r.spawn(ctx)
		if err != nil {
			cancel()
			fail("respawn: %v", err)
		}
		cl, err := client.Dial(ctx, filepath.Join(r.dir, "engine.sock"))
		if err != nil {
			cancel()
			fail("dial the respawned engine: %v", err)
		}
		var drained drainOutput
		err = cl.Call(ctx, "test.drain", RepoInput{Repo: repo}, &drained)
		cl.Close()
		cancel()
		if err != nil {
			fail("test.drain: %v", err)
		}
		if drained.Found > left {
			fail("the restarted engine found %d intents pending, more than the %d the kill left", drained.Found, left)
		}
		if err := eng.stop(); err != nil {
			fail("%v", err)
		}
		if err := checkRound(storeDir, acked); err != nil {
			fail("%v", err)
		}
	}
	if midFlight == 0 {
		t.Fatalf("seed %d: no round of %d was a kill mid-flight (no intent was pending at any kill)", seed, rounds)
	}
	st, err := store.Open(context.Background(), storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var ideas, done, already int
	err = st.DB().QueryRow(`SELECT (SELECT count(*) FROM ideas), (SELECT count(*) FROM intents WHERE state = 'done'),
		(SELECT count(*) FROM intents WHERE state = 'done' AND result = 'already written')`).Scan(&ideas, &done, &already)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("kill test: %d rounds, seed %d, %d writes acknowledged, %d ideas, %d intents done, %d kills mid-flight, %d lines ahead, %d intents found already written",
		rounds, seed, len(acked), ideas, done, midFlight, ahead, already)
}

// killRound spawns an engine, writes from four clients, kills the engine a
// random delay after the first acknowledged write, and reads the store as
// the kill left it. It returns the acknowledged n, the intents left pending,
// and the kill.log lines whose intent is not done.
func killRound(ctx context.Context, r *crashRig, repo, storeDir string, next *atomic.Int64, rng *rand.Rand, fail func(string, ...any)) (acked []int, left, lines int) {
	eng, err := r.spawn(ctx)
	if err != nil {
		fail("spawn: %v", err)
	}
	const writers, perWriter = 4, 50
	conns := make([]*client.Client, writers)
	for i := range conns {
		if conns[i], err = client.Dial(ctx, filepath.Join(r.dir, "engine.sock")); err != nil {
			fail("dial: %v", err)
		}
		defer conns[i].Close()
	}
	var killed atomic.Bool
	var first sync.Once
	firstAck := make(chan struct{})
	got := make([][]int, writers)
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i, c := range conns {
		wg.Go(func() {
			for range perWriter {
				n := int(next.Add(1))
				if err := c.Call(ctx, "test.write", writeInput{Repo: repo, N: n}, nil); err != nil {
					if !killed.Load() {
						errs[i] = fmt.Errorf("test.write %d before the kill: %w", n, err)
					}
					return
				}
				got[i] = append(got[i], n)
				first.Do(func() { close(firstAck) })
			}
		})
	}
	select {
	case <-firstAck:
	case <-ctx.Done():
		killed.Store(true)
		eng.kill()
		wg.Wait()
		fail("no write was acknowledged: %v (%v)", ctx.Err(), errors.Join(errs...))
	}
	time.Sleep(time.Duration(rng.Int63n(int64(40*time.Millisecond) + 1)))
	killed.Store(true)
	if err := eng.kill(); err != nil {
		fail("%v", err)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		fail("%v", err)
	}
	for _, g := range got {
		acked = append(acked, g...)
	}
	left, lines, err = asKilled(storeDir)
	if err != nil {
		fail("reading the store as the kill left it: %v", err)
	}
	return acked, left, lines
}

// asKilled opens the store with no engine running and counts the pending
// intents and the kill.log lines whose intent is not done.
func asKilled(storeDir string) (pending, ahead int, err error) {
	st, err := store.Open(context.Background(), storeDir)
	if err != nil {
		return 0, 0, err
	}
	defer st.Close()
	if err := st.DB().QueryRow(`SELECT count(*) FROM intents WHERE state = 'pending'`).Scan(&pending); err != nil {
		return 0, 0, err
	}
	notDone := map[int64]bool{}
	rows, err := st.DB().Query(`SELECT id FROM intents WHERE state != 'done'`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, 0, err
		}
		notDone[id] = true
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	lines, err := logLines(storeDir)
	for _, l := range lines {
		if notDone[l.id] {
			ahead++
		}
	}
	return pending, ahead, err
}

// logLine is one line of kill.log: "<intent id> <text>".
type logLine struct {
	id   int64
	text string
}

// logLines reads kill.log; a missing file has no lines.
func logLines(storeDir string) ([]logLine, error) {
	b, err := os.ReadFile(filepath.Join(storeDir, "logs", "kill.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var out []logLine
	for i, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if l == "" && len(b) == 0 {
			break
		}
		idText, text, ok := strings.Cut(l, " ")
		id, err := strconv.ParseInt(idText, 10, 64)
		if !ok || err != nil {
			return nil, fmt.Errorf("kill.log line %d %q is not \"<id> <text>\"", i+1, l)
		}
		out = append(out, logLine{id, text})
	}
	return out, nil
}

// checkRound opens the store with no engine running and reports every way
// state, events, intents and kill.log disagree, for the acknowledged n.
func checkRound(storeDir string, acked []int) error {
	st, err := store.Open(context.Background(), storeDir)
	if err != nil {
		return err
	}
	defer st.Close()
	db := st.DB()
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	ideas := map[int64]string{} // id to text (its n)
	byText := map[string]int64{}
	if err := scanRows(db.Query(`SELECT id, text FROM ideas`))(func(scan func(...any) error) error {
		var id int64
		var text string
		err := scan(&id, &text)
		ideas[id], byText[text] = text, id
		return err
	}); err != nil {
		return err
	}
	for _, n := range acked {
		if _, ok := byText[strconv.Itoa(n)]; !ok {
			bad("write %d was acknowledged and idea kill-%d is missing", n, n)
		}
	}

	added, doneEvents := map[int64]int{}, map[int64]int{}
	if err := scanRows(db.Query(`SELECT kind, coalesce(subject_id, 0) FROM events WHERE kind IN ('idea.added', 'intent.done')`))(func(scan func(...any) error) error {
		var kind string
		var id int64
		err := scan(&kind, &id)
		if kind == "idea.added" {
			added[id]++
		} else {
			doneEvents[id]++
		}
		return err
	}); err != nil {
		return err
	}
	for id, text := range ideas {
		if added[id] != 1 {
			bad("idea kill-%s has %d idea.added events, want 1", text, added[id])
		}
	}
	for id := range added {
		if _, ok := ideas[id]; !ok {
			bad("an idea.added event names idea %d, which does not exist", id)
		}
	}

	type intent struct{ state, text string }
	intents := map[int64]intent{}
	perIdea := map[string]int{}
	if err := scanRows(db.Query(`SELECT id, state, payload FROM intents`))(func(scan func(...any) error) error {
		var id int64
		var in intent
		var payload string
		if err := scan(&id, &in.state, &payload); err != nil {
			return err
		}
		var p struct{ Text string }
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		in.text = p.Text
		intents[id] = in
		perIdea[p.Text]++
		return nil
	}); err != nil {
		return err
	}
	for _, text := range ideas {
		if perIdea[text] != 1 {
			bad("idea kill-%s has %d intents, want 1", text, perIdea[text])
		}
	}
	for id, in := range intents {
		if _, ok := byText[in.text]; !ok {
			bad("intent %d names text %q, which no idea has", id, in.text)
		}
		if in.state != "done" {
			bad("intent %d is %s, want done", id, in.state)
		}
		if doneEvents[id] != 1 {
			bad("intent %d has %d intent.done events, want 1", id, doneEvents[id])
		}
	}
	for id := range doneEvents {
		if _, ok := intents[id]; !ok {
			bad("an intent.done event names intent %d, which does not exist", id)
		}
	}

	lines, err := logLines(storeDir)
	if err != nil {
		return err
	}
	written := map[int64]int{}
	for _, l := range lines {
		if written[l.id]++; written[l.id] == 2 {
			bad("kill.log has intent %d's line more than once", l.id)
		}
		if in, ok := intents[l.id]; !ok || in.text != l.text {
			bad("kill.log line %d %q does not match intent %d", l.id, l.text, l.id)
		}
	}
	for id, in := range intents {
		if in.state == "done" && written[id] == 0 {
			bad("intent %d is done and kill.log has no line for it", id)
		}
	}

	var timers int
	if err := db.QueryRow(`SELECT count(*) FROM timers WHERE kind = 'events.expire' AND key = 'retention'`).Scan(&timers); err != nil {
		return err
	}
	if timers != 1 {
		bad("the retention timer row is missing")
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	if len(problems) > 10 {
		problems = append(problems[:10], fmt.Sprintf("and %d more", len(problems)-10))
	}
	return errors.New(strings.Join(problems, "; "))
}

// scanRows runs each over every row of a query, then closes the rows.
func scanRows(rows *sql.Rows, err error) func(each func(scan func(...any) error) error) error {
	return func(each func(scan func(...any) error) error) error {
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := each(rows.Scan); err != nil {
				return err
			}
		}
		return rows.Err()
	}
}

// goodStore makes a store, in a MkdirTemp directory of its own, holding
// one write carried out by a real State: one that checkRound accepts.
func goodStore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(newDir(t), "store")
	st, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, err := state.Open(st, state.Options{StoreDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer s.Close(ctx)
	if err := s.Submit(ctx, writeCmd(1)); err != nil {
		t.Fatal(err)
	}
	for s.PendingIntents() > 0 {
		if ctx.Err() != nil {
			t.Fatal("the intent was not carried out within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return dir
}

// TestKillCheckCatches proves checkRound reports each broken store.
func TestKillCheckCatches(t *testing.T) {
	if err := checkRound(goodStore(t), []int{1}); err != nil {
		t.Fatalf("a good store is reported: %v", err)
	}
	cases := []struct {
		name  string
		acked []int
		sql   string
		line  bool
		want  string
	}{
		{name: "acknowledged with no idea", acked: []int{1, 2}, want: "write 2 was acknowledged and idea kill-2 is missing"},
		{name: "two idea.added events", sql: `INSERT INTO events (at, kind, subject, subject_id, data) SELECT at, kind, subject, subject_id, data FROM events WHERE kind = 'idea.added'`,
			want: "idea kill-1 has 2 idea.added events, want 1"},
		{name: "pending intent", sql: `UPDATE intents SET state = 'pending'`, want: "is pending, want done"},
		{name: "line written twice", line: true, want: "more than once"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := goodStore(t)
			if c.sql != "" {
				st, err := store.Open(context.Background(), dir)
				if err != nil {
					t.Fatal(err)
				}
				_, err = st.DB().Exec(c.sql)
				st.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if c.line {
				path := filepath.Join(dir, "logs", "kill.log")
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(b, b...), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			acked := c.acked
			if acked == nil {
				acked = []int{1}
			}
			err := checkRound(dir, acked)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("checkRound = %v, want an error containing %q", err, c.want)
			}
		})
	}
}
