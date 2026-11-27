//go:build linux || darwin

package enginecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/enginestart"
)

// TestMain never lets the test binary act as an engine, and gives the
// command tests use for an unexpected start a process that exits 1.
func TestMain(m *testing.M) {
	if i := slices.Index(os.Args, "engine"); i >= 0 && i+1 < len(os.Args) && os.Args[i+1] == "run" {
		fmt.Fprintln(os.Stderr, "enginecli test binary: refusing to run as an engine")
		os.Exit(3)
	}
	if os.Getenv("PHYSA_ENGINECLI_EXIT1") != "" {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// testEngine is an engine run in this process for one test.
type testEngine struct {
	dir  string // the engine's directory, $XDG_CONFIG_HOME/physa
	root *cobra.Command
}

// newEngine points XDG_CONFIG_HOME at a new short temporary directory,
// runs an engine on its physa directory and waits until it answers.
// Cleanup stops it and waits for Run to return. The commands' start
// Command fails the test: nothing may be spawned.
func newEngine(t *testing.T) *testEngine {
	t.Helper()
	te := idleDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx, engine.Options{Dir: te.dir, Idle: time.Minute, Version: "test"}) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after its context was cancelled")
		}
	})
	p, err := engine.PathsIn(te.dir)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		dctx, dcancel := context.WithTimeout(context.Background(), time.Second)
		c, err := client.Dial(dctx, p.Socket)
		if err == nil {
			err = c.Call(dctx, "engine.status", nil, nil)
			c.Close()
		}
		dcancel()
		if err == nil {
			return te
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine did not answer within 5s: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// idleDir sets up the directory and commands with no engine running.
func idleDir(t *testing.T) *testEngine {
	t.Helper()
	return idleDirEnv(t, os.Getenv)
}

// idleDirEnv is idleDir with getenv as how the commands find an agent's
// session.
func idleDirEnv(t *testing.T, getenv func(string) string) *testEngine {
	t.Helper()
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	t.Setenv("XDG_CONFIG_HOME", tmp)
	opts := enginestart.Options{Command: func(dir string, _ time.Duration) *exec.Cmd {
		t.Errorf("a command tried to start an engine on %s", dir)
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), "PHYSA_ENGINECLI_EXIT1=1")
		return cmd
	}}
	root := command.New("physa", "", nil, nil, commands("test", opts, getenv)...)
	return &testEngine{dir: filepath.Join(tmp, "physa"), root: root}
}

func (te *testEngine) run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := command.Execute(context.Background(), te.root, args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestStatusJSON(t *testing.T) {
	te := newEngine(t)
	code, out, errs := te.run("engine", "status", "--json")
	if code != 0 || errs != "" {
		t.Fatalf("code %d, stderr %q", code, errs)
	}
	var s engine.Status
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if s.Version != "test" || s.PID != os.Getpid() || s.APIRevision != 1 || s.Repos == nil || len(s.Repos) != 0 {
		t.Errorf("status %+v", s)
	}
	if _, err := time.Parse(time.RFC3339, s.StartedAt); err != nil {
		t.Errorf("started_at %q: %v", s.StartedAt, err)
	}
}

func TestStatusPlain(t *testing.T) {
	te := newEngine(t)
	for _, args := range [][]string{{"engine", "status"}, {"engine"}} {
		code, out, errs := te.run(args...)
		if code != 0 || errs != "" {
			t.Fatalf("%v: code %d, stderr %q", args, code, errs)
		}
		if strings.Contains(out, "\x1b[") {
			t.Errorf("%v: plain output has an ANSI escape: %q", args, out)
		}
		for _, want := range []string{"version       test\n", fmt.Sprintf("pid           %d\n", os.Getpid()), "API revision  1\n", "repos         none\n", "started  "} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: output lacks %q:\n%s", args, want, out)
			}
		}
	}
}

func TestAPIList(t *testing.T) {
	te := newEngine(t)
	for _, args := range [][]string{{"api", "list"}, {"api"}} {
		code, out, errs := te.run(args...)
		if code != 0 || errs != "" {
			t.Fatalf("%v: code %d, stderr %q", args, code, errs)
		}
		if !strings.HasPrefix(out, "Method") {
			t.Errorf("%v: no header:\n%s", args, out)
		}
		for _, m := range []string{"api.describe", "api.list", "engine.events", "engine.status", "engine.stop"} {
			if !strings.Contains(out, "\n"+m+" ") {
				t.Errorf("%v: lacks %s:\n%s", args, m, out)
			}
		}
	}
	code, out, _ := te.run("api", "list", "--json")
	var list []struct{ Name, Summary string }
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) != 5 {
		t.Errorf("--json: code %d, %q", code, out)
	}
}

func TestAPIDescribe(t *testing.T) {
	te := newEngine(t)
	code, out, errs := te.run("api", "describe", "engine.status")
	if code != 0 || errs != "" {
		t.Fatalf("code %d, stderr %q", code, errs)
	}
	for _, want := range []string{
		"Reports the engine's version", "\nMethod: engine.status\n",
		"\nInput schema:\n  {\n", "\nOutput schema:\n  {\n", `"started_at"`,
		"\nRequest example:\n  {}\n", "\nResponse example:\n  {\n", "\nNotes:\n- pid is",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("describe lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = te.run("api", "describe", "engine.status", "--json")
	var d struct{ Input, Output map[string]any }
	if code != 0 || json.Unmarshal([]byte(out), &d) != nil || d.Input["type"] != "object" || d.Output["properties"] == nil {
		t.Errorf("--json: code %d, %q", code, out)
	}
	code, _, errs = te.run("api", "describe", "nope")
	if code != 2 || !strings.HasPrefix(errs, "physa api describe: bad_request: no method \"nope\"") {
		t.Errorf("unknown method: code %d, stderr %q", code, errs)
	}
}

func TestAPICall(t *testing.T) {
	te := newEngine(t)
	for _, args := range [][]string{{"api", "call", "engine.status"}, {"api", "call", "engine.status", "{}"}} {
		code, out, errs := te.run(args...)
		if code != 0 || errs != "" {
			t.Fatalf("%v: code %d, stderr %q", args, code, errs)
		}
		var s engine.Status
		if err := json.Unmarshal([]byte(out), &s); err != nil || s.PID != os.Getpid() || !strings.Contains(out, "\n  \"pid\": ") {
			t.Errorf("%v: %v %q", args, err, out)
		}
	}
	code, _, errs := te.run("api", "call", "engine.status", "{nope")
	if code != 2 || errs != "physa api call: the params are not valid JSON\n" {
		t.Errorf("bad JSON: code %d, stderr %q", code, errs)
	}
}

func TestCallStdin(t *testing.T) {
	te := newEngine(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("{}")
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
	code, out, errs := te.run("api", "call", "engine.status", "-")
	var s engine.Status
	if code != 0 || errs != "" || json.Unmarshal([]byte(out), &s) != nil || s.Version != "test" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestBadRequest(t *testing.T) {
	te := newEngine(t)
	code, out, errs := te.run("api", "call", "api.describe", `{"nope":1}`)
	if code != 2 || out != "" || !strings.HasPrefix(errs, "physa api call: bad_request: ") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

func TestStopTwice(t *testing.T) {
	te := newEngine(t)
	code, out, errs := te.run("engine", "stop")
	if code != 0 || out != "Engine stopped\n" || errs != "" {
		t.Fatalf("first stop: code %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, errs = te.run("engine", "stop")
	if code != 0 || out != "No engine running\n" || errs != "" {
		t.Errorf("second stop: code %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, _ = te.run("engine", "stop", "--json")
	if code != 0 || out != "{\n  \"stopped\": false\n}\n" {
		t.Errorf("--json: code %d, stdout %q", code, out)
	}
}

func TestUsage(t *testing.T) {
	te := idleDir(t)
	for _, args := range [][]string{
		{"engine", "status", "extra"}, {"engine", "--bogus"}, {"engine", "status", "-j"},
		{"engine", "start", "--idle"}, {"engine", "start", "--idle", "soon"}, {"engine", "start", "--idle", "-5s"},
		{"engine", "start", "--dir", "/x"}, {"engine", "run", "--json"}, {"engine", "run", "--dir"},
		{"engine", "run", "--idle", "0"}, {"engine", "run", "--dir="}, {"engine", "stop", "now"}, {"engine", "stop", "--json=yes"},
		{"api", "list", "extra"}, {"api", "describe"}, {"api", "describe", "a", "b"},
		{"api", "call"}, {"api", "call", "m", "{}", "more"}, {"api", "call", "m", "--json"},
	} {
		code, out, errs := te.run(args...)
		if code != 2 || out != "" || !strings.HasPrefix(errs, "usage: physa "+args[0]) {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
	code, _, errs := te.run("engine", "run", "--dir", "relative/physa")
	if code != 2 || errs != "physa engine run: --dir \"relative/physa\" is not an absolute path\n" {
		t.Errorf("relative --dir: code %d, stderr %q", code, errs)
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	te := newEngine(t)
	code, out, errs := te.run("engine", "start")
	if code != 0 || errs != "" || !strings.HasPrefix(out, "Engine already running\n") || strings.Contains(out, "idle") {
		t.Errorf("start: code %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, _ = te.run("engine", "start", "--idle", "5s")
	if code != 0 || !strings.HasSuffix(out, "The running engine keeps its own idle time; --idle applies only to an engine that starts.\n") {
		t.Errorf("start --idle: code %d, stdout %q", code, out)
	}
	code, out, _ = te.run("engine", "start", "--json")
	var r struct {
		Started *bool
		Status  engine.Status
	}
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || r.Started == nil || *r.Started || r.Status.PID != os.Getpid() {
		t.Errorf("--json: code %d, stdout %q", code, out)
	}
}

func TestRunAlreadyRunning(t *testing.T) {
	te := newEngine(t)
	code, out, errs := te.run("engine", "run", "--dir", te.dir)
	want := fmt.Sprintf("physa engine run: an engine is already running (pid %d)\n", os.Getpid())
	if code != 3 || out != "" || errs != want {
		t.Errorf("code %d, stdout %q, stderr %q, want stderr %q", code, out, errs, want)
	}
}

func TestStopNeverStarts(t *testing.T) {
	te := idleDir(t)
	code, out, errs := te.run("engine", "stop")
	if code != 0 || out != "No engine running\n" || errs != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

// TestAPICallRefusesPursuitsInASession shows a pursuit is refused, before any engine
// starts, inside an agent's session, and that other methods are not.
func TestAPICallRefusesPursuitsInASession(t *testing.T) {
	// idleDirEnv fails the test if anything tries to start an engine.
	te := idleDirEnv(t, func(k string) string {
		if k == "CLAUDE_CODE_SESSION_ID" {
			return "abc"
		}
		return ""
	})
	for method, want := range map[string]string{
		"pursuit.start": "physa api call: A person must start this pursuit: physa is running inside a Claude Code session.\n",
		"pursuit.stop":  "physa api call: A person must stop this pursuit: physa is running inside a Claude Code session.\n",
	} {
		code, out, errs := te.run("api", "call", method, `{"repo":"/x","slug":"a","person":"p"}`)
		if code != 2 || out != "" || errs != want {
			t.Errorf("%s: code %d, stdout %q, stderr %q", method, code, out, errs)
		}
	}
}
