//go:build linux || darwin

package pursuitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/enginestart"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/proc"
	"github.com/dphaener/physa/internal/workapi"
)

// goodSignal is a whole signal, read on a day well after any test runs.
const goodSignal = "# Try a quiet mode\n## Claim\nFewer complaints.\n## Direction\nFor: complaints fall\nAgainst: complaints stay\n## Window\n2099-01-01\n## Resolver\nann\n"

// noResolver is goodSignal without its Resolver part.
var noResolver = strings.Replace(goodSignal, "## Resolver\nann\n", "", 1)

// TestMain never lets the test binary act as an engine.
func TestMain(m *testing.M) {
	if i := slices.Index(os.Args, "engine"); i >= 0 && i+1 < len(os.Args) && os.Args[i+1] == "run" {
		fmt.Fprintln(os.Stderr, "pursuitcli test binary: refusing to run as an engine")
		os.Exit(3)
	}
	os.Exit(m.Run())
}

type tenv struct {
	t   *testing.T
	e   *ideacli.Env
	tmp string
}

// newTest runs an engine with the idea and pursuit methods in this process on a
// short temporary settings directory and works in a fresh git repo, both
// passed in the Env's process state. The person is "tester", nothing is
// interactive and nothing may spawn an engine. It sets TMPDIR, which the
// editor's temporary files are made in, so its tests are serial.
func newTest(t *testing.T) *tenv {
	t.Helper()
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	t.Setenv("TMPDIR", filepath.Join(tmp, "tmp"))
	if err := os.Mkdir(filepath.Join(tmp, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	dir := filepath.Join(tmp, "physa")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- engine.Run(ctx, engine.Options{Env: os.Environ(), Dir: dir, Idle: time.Minute, Version: "test", Methods: workapi.Methods(os.Environ(), func(context.Context, string) string { return "" })})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after its context was cancelled")
		}
	})
	p, err := engine.PathsIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		dctx, dcancel := context.WithTimeout(context.Background(), time.Second)
		c, err := client.Dial(dctx, p.Socket)
		if err == nil {
			err = c.Call(dctx, "engine.status", nil, nil)
			c.Close()
		}
		dcancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine did not answer within 5s: %v", err)
		}
	}
	te := &tenv{t: t, tmp: tmp}
	e := ideacli.New(proc.State{Dir: repo, Wd: repo, Env: []string{"XDG_CONFIG_HOME=" + tmp}}, nil)
	e.Person = func(string, forge.Forge, io.Writer) string { return "tester" }
	e.Start = enginestart.Options{Command: func(dir string, _ time.Duration) *exec.Cmd {
		t.Errorf("a command tried to start an engine on %s", dir)
		return exec.Command("false")
	}}
	e.IsInteractive = func() bool { return false }
	e.Pick = func(string, []picker.Item) (string, error) { t.Error("Pick called"); return "", picker.ErrAborted }
	e.Confirm = func(string) (bool, error) { t.Error("Confirm called"); return false, picker.ErrAborted }
	e.Stdin = strings.NewReader("")
	e.Stdio = [3]*os.File{}
	te.e = e
	return te
}

// setenv sets key to value in the commands' environment; the last value set wins.
func (te *tenv) setenv(key, value string) {
	te.e.Proc.Env = append(te.e.Proc.Env, key+"="+value)
}

// run runs a command line under physa: "pursue ..." or "pursuit ...".
func (te *tenv) run(args ...string) (int, string, string) {
	te.t.Helper()
	var out, errb bytes.Buffer
	root := command.New("physa", "", nil, nil, commandsFor(te.e)...)
	code := command.Execute(context.Background(), root, args, &out, &errb)
	return code, out.String(), errb.String()
}

func (te *tenv) pursuit(args ...string) (int, string, string) {
	te.t.Helper()
	return te.run(append([]string{"pursuit"}, args...)...)
}

func ok(t *testing.T, args []string, code int, out, errs string) string {
	t.Helper()
	if code != 0 {
		t.Fatalf("%v: code %d, stderr %q", args, code, errs)
	}
	return out
}

func refused(t *testing.T, args []string, code int, out, errs string) string {
	t.Helper()
	if code != 2 || out != "" {
		t.Fatalf("%v: code %d, stdout %q; want 2 and nothing", args, code, out)
	}
	return errs
}

// must runs physa pursuit with args and wants it to succeed.
func (te *tenv) must(args ...string) string {
	te.t.Helper()
	code, out, errs := te.pursuit(args...)
	return ok(te.t, args, code, out, errs)
}

// fails runs physa pursuit with args and wants it refused.
func (te *tenv) fails(args ...string) string {
	te.t.Helper()
	code, out, errs := te.pursuit(args...)
	return refused(te.t, args, code, out, errs)
}

// pursueArgs is physa pursue with args, reading goodSignal from a file
// unless args say where the signal comes from.
func (te *tenv) pursueArgs(args ...string) []string {
	if !slices.Contains(args, "--signal-from") && te.e.Getenv("EDITOR") == "" {
		args = append(args, "--signal-from", te.file("signal.md", goodSignal))
	}
	return append([]string{"pursue"}, args...)
}

// pursue runs physa pursue with args and wants it to succeed.
func (te *tenv) pursue(args ...string) string {
	te.t.Helper()
	full := te.pursueArgs(args...)
	code, out, errs := te.run(full...)
	return ok(te.t, full, code, out, errs)
}

// pursueFails runs physa pursue with args and wants it refused.
func (te *tenv) pursueFails(args ...string) string {
	te.t.Helper()
	full := te.pursueArgs(args...)
	code, out, errs := te.run(full...)
	return refused(te.t, full, code, out, errs)
}

func (te *tenv) file(name, content string) string {
	te.t.Helper()
	p := filepath.Join(te.tmp, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		te.t.Fatal(err)
	}
	return p
}

// idea adds an idea through the engine.
func (te *tenv) idea(text, slug string) {
	te.t.Helper()
	repo, err := te.e.Repo()
	if err != nil {
		te.t.Fatal(err)
	}
	if err := te.e.Call("idea.add", client.AddIn{Repo: repo, Text: text, Slug: slug, Person: "ann"}, nil); err != nil {
		te.t.Fatal(err)
	}
}

func (te *tenv) list() []client.PursuitOut {
	te.t.Helper()
	var bs []client.PursuitOut
	if err := json.Unmarshal([]byte(te.must("list", "--json")), &bs); err != nil {
		te.t.Fatal(err)
	}
	return bs
}

func TestPursueWithAndWithoutAppetite(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	te.idea("Show pursuits on the phone", "phone-pursuits")
	out := te.pursue("try-a-quiet-mode", "--appetite", "1.5m")
	if out != "Pursuing \"Try a quiet mode\" as try-a-quiet-mode, appetite 1.5m, read on 2099-01-01\n" {
		t.Errorf("pursue output %q", out)
	}
	var o client.PursuitOut
	if err := json.Unmarshal([]byte(te.pursue("phone-pursuits", "--json")), &o); err != nil {
		t.Fatal(err)
	}
	if o.Appetite != 0 || o.State != "active" || o.Person != "tester" || o.Signal == nil || o.Signal.Claim != "Fewer complaints." ||
		o.Signal.For != "complaints fall" || o.Signal.Against != "complaints stay" || o.Signal.Window != "2099-01-01" || o.Signal.Resolver != "ann" {
		t.Errorf("pursuit with no appetite: %+v %+v", o, o.Signal)
	}
}

func TestSecondActivePursuitRefused(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	te.pursue("try-a-quiet-mode", "--appetite", "200k")
	errs := te.pursueFails("try-a-quiet-mode", "--appetite", "100k")
	if !strings.Contains(errs, "try-a-quiet-mode") || len(te.list()) != 1 {
		t.Errorf("stderr %q, pursuits %d", errs, len(te.list()))
	}
}

func TestPursueRefusesABadSignalOrAppetite(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	errs := te.pursueFails("try-a-quiet-mode", "--signal-from", te.file("bad.md", noResolver+"## Window\nnext week\n## Wishes\ny\n"))
	lines := strings.Split(strings.TrimSpace(errs), "\n")
	if !strings.HasPrefix(lines[0], "physa pursue: the signal has problems: ") || !strings.Contains(lines[0], `missing part "Resolver"`) ||
		!strings.Contains(lines[0], `repeated heading "Window"`) || !strings.Contains(lines[0], `unknown heading "Wishes"`) {
		t.Errorf("every problem on one line: %q", errs)
	}
	te.e.Stdin = strings.NewReader(strings.Replace(goodSignal, "2099-01-01", "2020-01-01", 1))
	if errs := te.pursueFails("try-a-quiet-mode", "--signal-from", "-"); !strings.Contains(errs, "window 2020-01-01 is before today") {
		t.Errorf("a window in the past: %q", errs)
	}
	if errs := te.pursueFails("try-a-quiet-mode", "--appetite", "lots"); !strings.Contains(errs, "lots") {
		t.Errorf("stderr %q", errs)
	}
	te.pursueFails("try-a-quiet-mode", "--signal-from", filepath.Join(te.tmp, "missing.md"))
	if len(te.list()) != 0 {
		t.Error("a refused pursue started a pursuit")
	}
}

func TestPursueOffTerminalNeedsSignalFrom(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	code, out, errs := te.run("pursue", "try-a-quiet-mode", "--appetite", "1k")
	if code != 2 || out != "" || !strings.Contains(errs, "--signal-from <file>") || !strings.Contains(errs, "--signal-from -") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

// editor sets $EDITOR to a script, with an argument, that copies the file
// it is given to seen, then writes the first of signals not yet written
// over it, and fails once every signal was written.
func (te *tenv) editor(seen string, signals ...string) {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n[ \"$1\" = --wait ] || exit 1\ncp \"$2\" " + seen + "\n")
	for i, sig := range signals {
		mark := filepath.Join(te.tmp, fmt.Sprintf("edited-%d", i))
		fmt.Fprintf(&b, "if [ ! -e %s ]; then touch %s; cp %s \"$2\"; exit 0; fi\n", mark, mark, te.file(fmt.Sprintf("signal-%d.md", i), sig))
	}
	b.WriteString("exit 1\n")
	te.setenv("EDITOR", te.file("editor.sh", b.String())+" --wait")
}

func (te *tenv) temps() []string {
	te.t.Helper()
	es, err := os.ReadDir(filepath.Join(te.tmp, "tmp"))
	if err != nil {
		te.t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, filepath.Join(te.tmp, "tmp", e.Name()))
	}
	return names
}

func TestPursueInEditor(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	te.e.IsInteractive = func() bool { return true }
	seen := filepath.Join(te.tmp, "seen.md")
	te.editor(seen, goodSignal)
	out := te.pursue("try-a-quiet-mode", "--appetite", "200k")
	if !strings.Contains(out, "Pursuing \"Try a quiet mode\" as try-a-quiet-mode") {
		t.Errorf("pursue: %q", out)
	}
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Try a quiet mode\n", "## Claim\n", "## Direction\n", "For:\nAgainst:\n", "## Window\n", "## Resolver\n", "<!--"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the template lacks %q:\n%s", want, b)
		}
	}
	if ts := te.temps(); len(ts) != 0 {
		t.Errorf("temporary files left: %v", ts)
	}
	if l := te.list(); len(l) != 1 || l[0].Appetite != 200000 {
		t.Errorf("pursuits %+v", l)
	}
}

func TestPursueInEditorAgainAfterProblems(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	te.e.IsInteractive = func() bool { return true }
	seen := filepath.Join(te.tmp, "seen.md")
	te.editor(seen, noResolver, goodSignal)
	asked := ""
	te.e.Confirm = func(q string) (bool, error) { asked = q; return true, nil }
	code, out, errs := te.run("pursue", "try-a-quiet-mode", "--json")
	if code != 0 || asked != "Edit the signal again?" || !strings.Contains(errs, `missing part "Resolver"`) {
		t.Fatalf("code %d, asked %q, stderr %q", code, asked, errs)
	}
	var o client.PursuitOut
	if err := json.Unmarshal([]byte(out), &o); err != nil || o.Signal == nil || o.Signal.Resolver != "ann" {
		t.Errorf("pursue --json: %q, %v", out, err)
	}
	if b, _ := os.ReadFile(seen); string(b) != noResolver {
		t.Errorf("the second edit did not reopen the edited file:\n%s", b)
	}
	if ts := te.temps(); len(ts) != 0 {
		t.Errorf("temporary files left: %v", ts)
	}
}

func TestPursueInEditorStopKeepsFile(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	te.e.IsInteractive = func() bool { return true }
	te.editor(filepath.Join(te.tmp, "seen.md"), noResolver)
	te.e.Confirm = func(string) (bool, error) { return false, nil }
	errs := te.pursueFails("try-a-quiet-mode")
	ts := te.temps()
	if len(ts) != 1 || !strings.Contains(errs, "Resolver") || !strings.Contains(errs, "the signal is kept at "+ts[0]) {
		t.Fatalf("temps %v, stderr %q", ts, errs)
	}
	if b, _ := os.ReadFile(ts[0]); string(b) != noResolver {
		t.Errorf("kept file: %q", b)
	}
	// An editor that fails keeps the file too.
	te.setenv("EDITOR", "false")
	if errs := te.pursueFails("try-a-quiet-mode"); !strings.Contains(errs, "the editor false failed") || !strings.Contains(errs, "kept at") {
		t.Errorf("failing editor: %q", errs)
	}
	if len(te.list()) != 0 {
		t.Error("a stopped edit started a pursuit")
	}
}

func TestListAndShow(t *testing.T) {
	te := newTest(t)
	if out := te.must("list"); !strings.Contains(out, "No pursuits yet. Start one with physa pursue <idea slug>.") {
		t.Errorf("empty list %q", out)
	}
	te.idea("Try a quiet mode", "")
	te.idea("Show pursuits on the phone", "phone-pursuits")
	te.pursue("try-a-quiet-mode", "--appetite", "200k")
	te.pursue("phone-pursuits")
	out := te.must("list")
	if strings.Contains(out, "\x1b") || strings.Index(out, "phone-pursuits") > strings.Index(out, "try-a-quiet-mode") {
		t.Errorf("list should be plain and newest first:\n%s", out)
	}
	for _, want := range []string{"Slug", "Idea", "Appetite", "Spent", "State", "Person", "200k", "none", "active", "tester"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if bare := te.must(); bare != out {
		t.Errorf("bare pursuit %q, list %q", bare, out)
	}
	if bs := te.list(); len(bs) != 2 || bs[0].Slug != "phone-pursuits" {
		t.Errorf("json list %+v", bs)
	}
	show := te.must("show", "try-a-quiet-mode")
	for _, want := range []string{"Try a quiet mode", "state", "active", "appetite  200k", "Signal", "claim     Fewer complaints.",
		"for       complaints fall", "against   complaints stay", "window    2099-01-01", "resolver  ann"} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %q:\n%s", want, show)
		}
	}
	if show := te.must("show", "phone-pursuits"); !strings.Contains(show, "appetite  none") || !strings.Contains(show, "spent     0") {
		t.Errorf("show with no appetite:\n%s", show)
	}
	var o client.PursuitOut
	if err := json.Unmarshal([]byte(te.must("show", "try-a-quiet-mode", "--json")), &o); err != nil || o.Signal == nil {
		t.Errorf("show json %+v, %v", o, err)
	}
}

func TestShowAPursuitFromBeforeSignals(t *testing.T) {
	te := newTest(t)
	// The engine leaves signal out for a pursuit started before signals.
	o := client.PursuitOut{Slug: "old", Idea: "old", IdeaText: "An old one", Appetite: 1000, State: "active", CreatedAt: "2026-11-01T10:00:00Z"}
	var out bytes.Buffer
	showPursuit(te.e.View(&out), o)
	if got := out.String(); !strings.Contains(got, "signal    none (started before signals)") || strings.Contains(got, "claim") {
		t.Errorf("show:\n%s", got)
	}
}

func TestStopWithReasonAndStopTwice(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	te.pursue("try-a-quiet-mode", "--appetite", "200k")
	out := te.must("stop", "try-a-quiet-mode", "--reason", "learned enough")
	if !strings.Contains(out, "Stopped pursuit try-a-quiet-mode") {
		t.Errorf("stop output %q", out)
	}
	show := te.must("show", "try-a-quiet-mode")
	if !strings.Contains(show, "stopped") || !strings.Contains(show, "stop reason  learned enough") {
		t.Errorf("show:\n%s", show)
	}
	if errs := te.fails("stop", "try-a-quiet-mode"); !strings.Contains(errs, "try-a-quiet-mode") {
		t.Errorf("stopping a stopped pursuit: %q", errs)
	}
	// The idea may be pursued again once its pursuit has stopped.
	te.pursue("try-a-quiet-mode", "--appetite", "10k")
}

func TestMissingSlugOffTerminalListsChoices(t *testing.T) {
	te := newTest(t)
	if errs := te.fails("show"); !strings.Contains(errs, "name a pursuit; there are none yet") {
		t.Errorf("no pursuits: %q", errs)
	}
	te.idea("Try a quiet mode", "")
	te.idea("Show pursuits on the phone", "phone-pursuits")
	te.pursue("phone-pursuits", "--appetite", "1k")
	errs := te.pursueFails()
	want := "physa pursue: name an idea; the ideas are:\n  try-a-quiet-mode  Try a quiet mode\nusage: physa pursue"
	if !strings.HasPrefix(errs, want) {
		t.Errorf("ideas without an active pursuit:\n%s", errs)
	}
	if errs := te.fails("stop"); !strings.Contains(errs, "phone-pursuits  Show pursuits on the phone") || strings.Contains(errs, "try-a") {
		t.Errorf("active pursuits:\n%s", errs)
	}
}

func TestAgentSessionRefusedBeforeAnything(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	te.pursue("try-a-quiet-mode", "--appetite", "1k")
	te.setenv("CLAUDE_CODE_SESSION_ID", "x")
	te.setenv("CLAUDECODE", "1")
	te.e.Person = func(string, forge.Forge, io.Writer) string { t.Error("person asked"); return "" }
	for _, args := range [][]string{{"try-a-quiet-mode", "--appetite", "1k"}, {"a", "b"}} {
		if errs := te.pursueFails(args...); !strings.Contains(errs, "A person must start this pursuit: physa is running inside a Claude Code session.") {
			t.Errorf("%v: %q", args, errs)
		}
	}
	// A flag pflag rejects is a usage error before the job runs, so it is not refused.
	if errs := te.pursueFails("--bogus"); !strings.HasPrefix(errs, "usage: physa pursue") {
		t.Errorf("--bogus: %q", errs)
	}
	if errs := te.fails("stop", "try-a-quiet-mode", "--yes"); !strings.Contains(errs, "A person must stop this pursuit: physa is running inside a Claude Code session.") {
		t.Errorf("stop: %q", errs)
	}
	if bs := te.list(); len(bs) != 1 || bs[0].State != "active" {
		t.Errorf("an agent changed pursuits: %+v", bs)
	}
	te.must("show", "try-a-quiet-mode")
}

func TestStopConfirmedOnTerminal(t *testing.T) {
	te := newTest(t)
	te.idea("Try a quiet mode", "")
	te.pursue("try-a-quiet-mode", "--appetite", "1k")
	te.e.IsInteractive = func() bool { return true }
	var asked string
	answer := false
	te.e.Confirm = func(q string) (bool, error) { asked = q; return answer, nil }
	if errs := te.fails("stop", "try-a-quiet-mode"); !strings.Contains(errs, "not stopped") || asked != "Stop pursuit try-a-quiet-mode?" {
		t.Errorf("declined: %q, asked %q", errs, asked)
	}
	te.e.Confirm = func(string) (bool, error) { return false, picker.ErrAborted }
	te.fails("stop", "try-a-quiet-mode")
	if te.list()[0].State != "active" {
		t.Error("a declined confirm stopped the pursuit")
	}
	asked, answer = "", true
	te.e.Confirm = func(q string) (bool, error) { asked = q; return answer, nil }
	te.must("stop", "try-a-quiet-mode")
	if te.list()[0].State != "stopped" {
		t.Error("a confirmed stop did not stop the pursuit")
	}
	te.pursue("try-a-quiet-mode", "--appetite", "1k")
	te.e.Confirm = func(string) (bool, error) { t.Error("--yes still asked"); return false, nil }
	te.must("stop", "try-a-quiet-mode-2", "--yes")
}

const twoChanges = `{"changes":[
 {"name":"first-step","capability":"alpha","what":"Do the first.","acceptance":["failure case: a bad input exits 2"],"must_not":["touch beta"],"after":[]},
 {"name":"second-step","capability":"beta","what":"Do the second.","acceptance":["no failure case: it only reads"],"must_not":[],"after":["first-step"]}]}`

// planRepo gives the test repo a map of two capabilities, a first commit and
// an active pursuit of 100k on the idea "try-a-quiet-mode".
func (te *tenv) planRepo() {
	te.t.Helper()
	repo, err := te.e.Repo()
	if err != nil {
		te.t.Fatal(err)
	}
	for _, c := range []string{"alpha", "beta"} {
		dir := filepath.Join(repo, ".physa", "capabilities", c)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			te.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("# "+c+"\n\nPromise of "+c+".\n"), 0o644); err != nil {
			te.t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "chore: map"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			te.t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	te.idea("Try a quiet mode", "")
	te.pursue("try-a-quiet-mode", "--appetite", "100k")
}

func (te *tenv) planFile(body string) string {
	te.t.Helper()
	p := filepath.Join(te.t.TempDir(), "plan.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		te.t.Fatal(err)
	}
	return p
}

func TestPlanSetFromFileAndStdinThenShow(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	out := te.must("plan", "set", "try-a-quiet-mode", "--from", te.planFile(twoChanges))
	if out != "Plan for try-a-quiet-mode set: revision 1, 2 changes\n" {
		t.Errorf("plan set: %q", out)
	}
	te.e.Stdin = strings.NewReader(twoChanges)
	if errs := te.fails("plan", "set", "try-a-quiet-mode"); !strings.Contains(errs, "needs a reason") {
		t.Errorf("second revision without reason: %q", errs)
	}
	te.e.Stdin = strings.NewReader(twoChanges)
	out = te.must("plan", "set", "try-a-quiet-mode", "--reason", "missed a step")
	if out != "Plan for try-a-quiet-mode set: revision 2, 2 changes, 1 replan\n" {
		t.Errorf("replan: %q", out)
	}
	te.e.Stdin = strings.NewReader(twoChanges)
	var po client.PlanOut
	if err := json.Unmarshal([]byte(te.must("plan", "set", "try-a-quiet-mode", "--reason", "again", "--from", "-", "--json")), &po); err != nil || po.N != 3 || po.Replans != 2 {
		t.Errorf("json plan set %+v, %v", po, err)
	}
	show := te.must("plan", "show", "try-a-quiet-mode")
	for _, want := range []string{"Plan for try-a-quiet-mode", "revision", "again", "first-step", "capability  alpha", "failure case: a bad input exits 2", "touch beta", "after       first-step"} {
		if !strings.Contains(show, want) {
			t.Errorf("plan show lacks %q:\n%s", want, show)
		}
	}
	if strings.Contains(show, "History") {
		t.Errorf("plan show without --history:\n%s", show)
	}
	hist := te.must("plan", "show", "try-a-quiet-mode", "--history")
	if !strings.Contains(hist, "History") {
		t.Errorf("history:\n%s", hist)
	}
	if i, j := strings.Index(hist, "reason: missed a step"), strings.Index(hist, "reason: again"); i < 0 || j < i {
		t.Errorf("history is not oldest first:\n%s", hist)
	}
}

func TestPlanSetRefusals(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	bad := `{"changes":[{"name":"a","capability":"alpha, beta","what":"x","acceptance":["failure case: y"],"after":[]}]}`
	errs := te.fails("plan", "set", "try-a-quiet-mode", "--from", te.planFile(bad))
	if !strings.HasPrefix(errs, "physa pursuit plan set: the plan has problems:") || !strings.Contains(errs, "names two capabilities") {
		t.Errorf("bad plan: %q", errs)
	}
	// Nothing on stdin and no --from off a terminal is an empty plan: bad usage.
	if errs := te.fails("plan", "set", "try-a-quiet-mode"); !strings.Contains(errs, "plan is empty") {
		t.Errorf("empty stdin: %q", errs)
	}
	te.e.IsInteractive = func() bool { return true }
	if errs := te.fails("plan", "set", "try-a-quiet-mode"); !strings.Contains(errs, "--from") {
		t.Errorf("terminal without --from: %q", errs)
	}
	te.e.IsInteractive = func() bool { return false }
	if errs := te.fails("plan", "set", "--from", te.planFile(twoChanges), "--bogus"); errs == "" {
		t.Error("bogus flag accepted")
	}
	if errs := te.fails("plan", "show", "try-a-quiet-mode"); !strings.Contains(errs, "try-a-quiet-mode") {
		t.Errorf("show without a plan: %q", errs)
	}
}

func TestPlanSetMissingPursuitListsActivePursuits(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	te.e.Stdin = strings.NewReader(twoChanges)
	errs := te.fails("plan", "set")
	if !strings.Contains(errs, "name a pursuit; the pursuits are:") || !strings.Contains(errs, "try-a-quiet-mode") {
		t.Errorf("missing slug: %q", errs)
	}
}

func TestPlanAndWorkAllowedInAgentSessions(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	te.setenv("CLAUDE_CODE_SESSION_ID", "sess-1")
	te.setenv("CLAUDECODE", "1")
	te.must("plan", "set", "try-a-quiet-mode", "--from", te.planFile(twoChanges))
	te.must("work", "try-a-quiet-mode/first-step")
}

func TestWorkAndCurrent(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	te.must("plan", "set", "try-a-quiet-mode", "--from", te.planFile(twoChanges))
	te.setenv("CLAUDE_CODE_SESSION_ID", "sess-1")
	if out := te.must("work", "--current"); out != "" {
		t.Errorf("--current before work: %q", out)
	}
	if out := te.must("work", "try-a-quiet-mode/first-step"); out != "Session sess-1 works on try-a-quiet-mode/first-step\n" {
		t.Errorf("work: %q", out)
	}
	if out := te.must("work", "--current"); out != "try-a-quiet-mode/first-step\n" {
		t.Errorf("--current: %q", out)
	}
	var wo client.WorkOut
	if err := json.Unmarshal([]byte(te.must("work", "try-a-quiet-mode/second-step", "--json")), &wo); err != nil || wo.Change != "second-step" {
		t.Errorf("work json %+v, %v", wo, err)
	}
	if out := te.must("work", "--current"); out != "try-a-quiet-mode/second-step\n" {
		t.Errorf("--current after a second work: %q", out)
	}
	te.setenv("CLAUDE_CODE_SESSION_ID", "other")
	if out := te.must("work", "--current"); out != "" {
		t.Errorf("--current for another session: %q", out)
	}
	te.setenv("CLAUDE_CODE_SESSION_ID", "sess-1")
	if errs := te.fails("work", "try-a-quiet-mode/nope"); !strings.Contains(errs, "nope") {
		t.Errorf("unknown change: %q", errs)
	}
	te.setenv("CLAUDE_CODE_SESSION_ID", "")
	if out := te.must("work", "--current"); out != "" {
		t.Errorf("--current with no session: %q", out)
	}
}

func TestWorkWithoutSessionAndBadArguments(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	want := "physa pursuit work: no agent session: run it inside the session that works on the change\n"
	if errs := te.fails("work", "try-a-quiet-mode/first-step"); errs != want {
		t.Errorf("no session: %q", errs)
	}
	te.setenv("CLAUDE_CODE_SESSION_ID", "sess-1")
	for _, args := range [][]string{{"work"}, {"work", "nochange"}, {"work", "a/"}, {"work", "/b"}, {"work", "a/b/c"}, {"work", "a/b", "--current"}, {"work", "--current", "--json"}} {
		code, out, _ := te.pursuit(args...)
		if code != 2 || out != "" {
			t.Errorf("%v: code %d, stdout %q", args, code, out)
		}
	}
}

func TestCurrentWithNoStoreOrRepoPrintsNothing(t *testing.T) {
	te := newTest(t)
	te.setenv("CLAUDE_CODE_SESSION_ID", "sess-1")
	// A repo whose engine never ran has no store, and --current must not make one.
	repo, err := te.e.Repo()
	if err != nil {
		t.Fatal(err)
	}
	code, out, errs := te.pursuit("work", "--current")
	if code != 0 || out != "" || errs != "" {
		t.Errorf("no store: %d %q %q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "physa")); !os.IsNotExist(err) {
		t.Errorf("--current made a store: %v", err)
	}
	notRepo := t.TempDir()
	te.e.Proc.Dir, te.e.Proc.Wd = notRepo, notRepo
	if code, out, errs := te.pursuit("work", "--current"); code != 0 || out != "" || errs != "" {
		t.Errorf("not a repo: %d %q %q", code, out, errs)
	}
}

func TestShowProgressAsFactsAndNeedsYou(t *testing.T) {
	te := newTest(t)
	te.planRepo()
	plain := te.must("show", "try-a-quiet-mode")
	if !strings.Contains(plain, "Progress") || !strings.Contains(plain, "planned       none yet") || strings.Contains(plain, "landed") {
		t.Errorf("no plan:\n%s", plain)
	}
	te.must("plan", "set", "try-a-quiet-mode", "--from", te.planFile(twoChanges))
	te.setenv("CLAUDE_CODE_SESSION_ID", "sess-1")
	te.must("work", "try-a-quiet-mode/first-step")
	spend := func(change string, tokens int64) {
		t.Helper()
		repo, _ := te.e.Repo()
		in := client.SpendIn{Repo: repo, Pursuit: "try-a-quiet-mode", Change: change, Session: "sess-1", Tokens: tokens}
		if err := te.e.Call("pursuit.spend", in, nil); err != nil {
			t.Fatal(err)
		}
	}
	spend("first-step", 30000)
	out := te.must("show", "try-a-quiet-mode")
	for _, want := range []string{"Progress", "planned       2 changes", "landed        0 of 2: none", "in work       first-step",
		"asks open     none yet", "spent         30k of 100k", "replans       0", "Spend per change", "first-step  30k"} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Needs you") {
		t.Errorf("Needs you before 80%%:\n%s", out)
	}
	spend("", 55000)
	out = te.must("show", "try-a-quiet-mode")
	if !strings.Contains(out, "Needs you") || !strings.Contains(out, "has spent 85k of its 100k appetite") || !strings.Contains(out, "(pursuit)  55k") {
		t.Errorf("after 80%%:\n%s", out)
	}
	spend("first-step", 45000)
	out = te.must("show", "try-a-quiet-mode")
	if !strings.Contains(out, "the circuit breaker stopped the pursuit") || !strings.Contains(out, "stopped") {
		t.Errorf("after 100%%:\n%s", out)
	}
	if strings.Contains(out, "%") {
		t.Errorf("a percentage in the output:\n%s", out)
	}
	var o client.PursuitOut
	if err := json.Unmarshal([]byte(te.must("show", "try-a-quiet-mode", "--json")), &o); err != nil || o.Progress == nil || len(o.NeedsYou) != 2 {
		t.Errorf("show json %+v, %v", o, err)
	}
}

func TestHelpListsPlanAndWork(t *testing.T) {
	te := newTest(t)
	var out bytes.Buffer
	root := command.New("physa", "", nil, nil, commandsFor(te.e)...)
	command.Execute(t.Context(), root, []string{"pursuit", "--help"}, &out, io.Discard)
	for _, want := range []string{"physa pursuit plan set", "physa pursuit plan show", "physa pursuit work"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, out.String())
		}
	}
}

func TestShowPrintsEvidenceAfterProgress(t *testing.T) {
	te := newTest(t)
	secs := int64(7500)
	o := client.PursuitOut{
		Progress: &client.ProgressOut{},
		Evidence: []client.EvidenceOut{
			{Kind: "lead_time", PR: 12, Commit: "3f9a2c1d", Capability: "cli", At: "2026-11-22T10:00:00Z", Seconds: &secs},
			{Kind: "landed", Commit: "3f9a2c1d", Capability: "cli", At: "2026-11-22T09:00:00Z"},
		},
		NeedsYou: []client.NeedOut{{Text: "ask"}},
	}
	var out bytes.Buffer
	showProgress(te.e.View(&out), o)
	got := out.String()
	for _, want := range []string{"Evidence", "lead time  #12  cli  2026-11-22 10:00 UTC  lead time 2h 5m", "landed  3f9a2c1d  cli  2026-11-22 09:00 UTC"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if p, e, n := strings.Index(got, "Progress"), strings.Index(got, "Evidence"), strings.Index(got, "Needs you"); !(p < e && e < n) {
		t.Errorf("order wrong (%d %d %d):\n%s", p, e, n, got)
	}
	out.Reset()
	o.Evidence = nil
	showProgress(te.e.View(&out), o)
	if strings.Contains(out.String(), "Evidence") {
		t.Errorf("an empty Evidence section:\n%s", out.String())
	}
}

func TestProgressWithNoAppetiteShowsSpendAlone(t *testing.T) {
	te := newTest(t)
	o := client.PursuitOut{Progress: &client.ProgressOut{Planned: []string{"a"}, Spent: 5000}}
	var out bytes.Buffer
	showProgress(te.e.View(&out), o)
	if got := out.String(); !strings.Contains(got, "spent         5k, no appetite") || strings.Contains(got, "5k of") {
		t.Errorf("progress:\n%s", got)
	}
}
