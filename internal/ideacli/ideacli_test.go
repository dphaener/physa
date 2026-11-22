//go:build linux || darwin

package ideacli

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

	"github.com/dphaener/physa/internal/apiclient"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/enginestart"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/workapi"
)

const goodPitch = "# Try a quiet mode\n## Problem\nToo noisy.\n## Appetite\n200k\n## Solution\nA flag.\n## Scope\nCLI only.\n## Signal\nFewer complaints.\n## Rabbit holes\nColour themes.\n## No-gos\nNo config file.\n"

// noSignal is goodPitch without its Signal part.
var noSignal = strings.Replace(goodPitch, "## Signal\nFewer complaints.\n", "", 1)

// TestMain never lets the test binary act as an engine.
func TestMain(m *testing.M) {
	if i := slices.Index(os.Args, "engine"); i >= 0 && i+1 < len(os.Args) && os.Args[i+1] == "run" {
		fmt.Fprintln(os.Stderr, "ideacli test binary: refusing to run as an engine")
		os.Exit(3)
	}
	os.Exit(m.Run())
}

type tenv struct {
	t    *testing.T
	e    *Env
	env  map[string]string
	repo string
	tmp  string
}

// newTest runs an engine with the idea and bet methods in this process on a
// short temporary settings directory, makes a git repo and works in it. The
// person is "tester", nothing is interactive and nothing may spawn an engine.
func newTest(t *testing.T) *tenv {
	t.Helper()
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("TMPDIR", filepath.Join(tmp, "tmp"))
	if err := os.Mkdir(filepath.Join(tmp, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if repo, err = filepath.EvalSymlinks(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(repo, "sub"))
	dir := filepath.Join(tmp, "physa")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- engine.Run(ctx, engine.Options{Dir: dir, Idle: time.Minute, Version: "test", Methods: workapi.Methods})
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
		c, err := apiclient.Dial(dctx, p.Socket)
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
	te := &tenv{t: t, env: map[string]string{}, repo: repo, tmp: tmp}
	e := New(nil)
	e.Person = func(string, forge.Forge, io.Writer) string { return "tester" }
	e.Start = enginestart.Options{Command: func(dir string, _ time.Duration) *exec.Cmd {
		t.Errorf("a command tried to start an engine on %s", dir)
		return exec.Command("false")
	}}
	e.Getenv = func(k string) string { return te.env[k] }
	e.IsInteractive = func() bool { return false }
	e.Pick = func(string, []picker.Item) (string, error) { t.Error("Pick called"); return "", picker.ErrAborted }
	e.Confirm = func(string) (bool, error) { t.Error("Confirm called"); return false, picker.ErrAborted }
	e.Stdin = strings.NewReader("")
	e.Stdio = [3]*os.File{}
	te.e = e
	return te
}

func (te *tenv) run(args ...string) (int, string, string) {
	te.t.Helper()
	var out, errb bytes.Buffer
	root := &cli.Command{Name: "physa", Subs: []*cli.Command{te.e.command()}}
	code := cli.Dispatch(root, args, &out, &errb)
	return code, out.String(), errb.String()
}

func (te *tenv) must(args ...string) string {
	te.t.Helper()
	code, out, errs := te.run(args...)
	if code != 0 {
		te.t.Fatalf("%v: code %d, stderr %q", args, code, errs)
	}
	return out
}

func (te *tenv) fails(args ...string) string {
	te.t.Helper()
	code, out, errs := te.run(args...)
	if code != 2 || out != "" {
		te.t.Fatalf("%v: code %d, stdout %q; want 2 and nothing", args, code, out)
	}
	return errs
}

func (te *tenv) file(name, content string) string {
	te.t.Helper()
	p := filepath.Join(te.tmp, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		te.t.Fatal(err)
	}
	return p
}

func TestAddShowsUniqueSlugs(t *testing.T) {
	te := newTest(t)
	if out := te.must("idea", "add", "Try a quiet mode"); out != "Added idea try-a-quiet-mode\n" {
		t.Errorf("add: %q", out)
	}
	if out := te.must("idea", "add", "Try a quiet mode"); out != "Added idea try-a-quiet-mode-2\n" {
		t.Errorf("second add: %q", out)
	}
	var o workapi.IdeaOut
	if err := json.Unmarshal([]byte(te.must("idea", "add", "Show bets on the phone", "--slug", "phone-bets", "--json")), &o); err != nil {
		t.Fatal(err)
	}
	if o.Slug != "phone-bets" || o.Person != "tester" || o.Text != "Show bets on the phone" {
		t.Errorf("add --json: %+v", o)
	}
	if errs := te.fails("idea", "add", "Again", "--slug=phone-bets"); !strings.Contains(errs, "phone-bets") {
		t.Errorf("taken slug: %q", errs)
	}
	if errs := te.fails("idea", "add", "x", "--slug", "list"); !strings.Contains(errs, "list") || strings.Contains(errs, "bad_request") {
		t.Errorf("reserved slug: %q", errs)
	}
}

func TestAddBadUsage(t *testing.T) {
	te := newTest(t)
	for _, args := range [][]string{
		{"idea", "add"}, {"idea", "add", "a", "b"}, {"idea", "add", "x", "-s", "y"},
		{"idea", "add", "x", "--slug"}, {"idea", "add", "x", "--from", "f"}, {"idea", "list", "extra"},
	} {
		code, out, errs := te.run(args...)
		if code != 2 || out != "" || !strings.Contains(errs, "usage: physa idea") {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
}

func TestNoPersonAddsNothing(t *testing.T) {
	te := newTest(t)
	te.e.Person = func(string, forge.Forge, io.Writer) string { return "" }
	te.fails("idea", "add", "Nobody's idea")
	if out := te.must("idea", "list", "--json"); strings.TrimSpace(out) != "[]" {
		t.Errorf("list after a refused add: %q", out)
	}
}

func TestListAndShow(t *testing.T) {
	te := newTest(t)
	if out := te.must("idea"); !strings.Contains(out, "No ideas yet") {
		t.Errorf("empty list: %q", out)
	}
	te.must("idea", "add", "Try a quiet mode")
	te.must("idea", "add", "Show bets on the phone", "--slug", "phone-bets")
	te.must("idea", "shape", "try-a-quiet-mode", "--from", te.file("pitch.md", goodPitch))

	out := te.must("idea", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "Slug") || !strings.HasPrefix(lines[1], "phone-bets") ||
		!strings.HasSuffix(lines[1], "not shaped") || !strings.HasSuffix(lines[2], "shaped") || !strings.Contains(lines[2], "tester") {
		t.Errorf("list:\n%s", out)
	}
	if bare := te.must("idea"); bare != out {
		t.Errorf("bare idea %q, list %q", bare, out)
	}
	var ideas []workapi.IdeaOut
	if err := json.Unmarshal([]byte(te.must("idea", "list", "--json")), &ideas); err != nil {
		t.Fatal(err)
	}
	if len(ideas) != 2 || ideas[0].Slug != "phone-bets" || ideas[1].Pitches != 1 {
		t.Errorf("list --json: %+v", ideas)
	}

	out = te.must("idea", "show", "try-a-quiet-mode")
	for _, want := range []string{"Try a quiet mode\n", "slug    try-a-quiet-mode", "person  tester", "facts   shaped",
		"bets    none", "Pitch, shaped by tester on ", "Problem\n  Too noisy.", "Appetite\n  200k", "No-gos\n  No config file."} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("plain show carries escapes: %q", out)
	}
	var o workapi.IdeaOut
	if err := json.Unmarshal([]byte(te.must("idea", "show", "try-a-quiet-mode", "--json")), &o); err != nil {
		t.Fatal(err)
	}
	if o.Pitch == nil || o.Pitch.Appetite != 200000 || o.Pitch.Signal != "Fewer complaints." {
		t.Errorf("show --json: %+v", o)
	}
	if errs := te.fails("idea", "show", "nope"); !strings.Contains(errs, "nope") {
		t.Errorf("unknown slug: %q", errs)
	}
}

func TestShapeFromStdinAndBadPitch(t *testing.T) {
	te := newTest(t)
	te.must("idea", "add", "Try a quiet mode")
	te.e.Stdin = strings.NewReader(goodPitch)
	if out := te.must("idea", "shape", "try-a-quiet-mode", "--from", "-"); !strings.Contains(out, "Shaped idea try-a-quiet-mode") ||
		!strings.Contains(out, "appetite  200k") {
		t.Errorf("shape --from -: %q", out)
	}
	te.e.Stdin = strings.NewReader(noSignal)
	if errs := te.fails("idea", "shape", "try-a-quiet-mode", "--from", "-"); !strings.Contains(errs, "Signal") {
		t.Errorf("a pitch with no Signal: %q", errs)
	}
	if errs := te.fails("idea", "shape", "try-a-quiet-mode", "--from", te.file("bad.md", "## Problem\nx\n## Wishes\ny\n")); !strings.Contains(errs, "Wishes") {
		t.Errorf("an unknown part: %q", errs)
	}
	te.fails("idea", "shape", "try-a-quiet-mode", "--from", filepath.Join(te.tmp, "missing.md"))
	var o workapi.IdeaOut
	if err := json.Unmarshal([]byte(te.must("idea", "show", "try-a-quiet-mode", "--json")), &o); err != nil || o.Pitches != 1 {
		t.Errorf("after refused pitches: %+v, %v", o, err)
	}
}

func TestMissingSlugOffTerminal(t *testing.T) {
	te := newTest(t)
	code, _, errs := te.run("idea", "show")
	if code != 2 || !strings.Contains(errs, "name an idea; there are none yet") {
		t.Errorf("no ideas: code %d, stderr %q", code, errs)
	}
	te.must("idea", "add", "Try a quiet mode")
	te.must("idea", "add", "Show bets on the phone", "--slug", "phone-bets")
	code, out, errs := te.run("idea", "show")
	want := "physa idea show: name an idea; the ideas are:\n  phone-bets        Show bets on the phone\n  try-a-quiet-mode  Try a quiet mode\nusage: physa idea show"
	if code != 2 || out != "" || !strings.HasPrefix(errs, want) {
		t.Errorf("code %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	code, _, errs = te.run("idea", "shape", "try-a-quiet-mode")
	if code != 2 || !strings.Contains(errs, "--from <file>") || !strings.Contains(errs, "--from -") {
		t.Errorf("shape off a terminal: code %d, stderr %q", code, errs)
	}
}

func TestPickerChoosesSlugOnTerminal(t *testing.T) {
	te := newTest(t)
	te.must("idea", "add", "Try a quiet mode")
	te.must("idea", "add", "Show bets on the phone", "--slug", "phone-bets")
	te.e.IsInteractive = func() bool { return true }
	var got []picker.Item
	te.e.Pick = func(title string, items []picker.Item) (string, error) {
		if title != "Choose an idea" {
			t.Errorf("title %q", title)
		}
		got = items
		return "try-a-quiet-mode", nil
	}
	out := te.must("idea", "show")
	if len(got) != 2 || got[0].Slug != "phone-bets" || got[1].Detail != "not shaped" || !strings.Contains(out, "slug    try-a-quiet-mode") {
		t.Errorf("items %+v, show %q", got, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("styled output into a buffer carries escapes: %q", out)
	}
	te.e.Pick = func(string, []picker.Item) (string, error) { return "", picker.ErrAborted }
	te.fails("idea", "show")
}

// editor sets $EDITOR to a script, with an argument, that copies the file
// it is given to seen, then writes the first of pitches not yet written
// over it, and fails once every pitch was written.
func (te *tenv) editor(seen string, pitches ...string) {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n[ \"$1\" = --wait ] || exit 1\ncp \"$2\" " + seen + "\n")
	for i, p := range pitches {
		mark := filepath.Join(te.tmp, fmt.Sprintf("edited-%d", i))
		fmt.Fprintf(&b, "if [ ! -e %s ]; then touch %s; cp %s \"$2\"; exit 0; fi\n", mark, mark, te.file(fmt.Sprintf("pitch-%d.md", i), p))
	}
	b.WriteString("exit 1\n")
	te.env["EDITOR"] = te.file("editor.sh", b.String()) + " --wait"
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

func TestShapeInEditor(t *testing.T) {
	te := newTest(t)
	te.must("idea", "add", "Try a quiet mode")
	te.must("idea", "shape", "try-a-quiet-mode", "--from", te.file("first.md", strings.Replace(goodPitch, "Too noisy.", "Far too noisy.", 1)))
	te.e.IsInteractive = func() bool { return true }
	seen := filepath.Join(te.tmp, "seen.md")
	te.editor(seen, goodPitch)
	te.env["VISUAL"] = ""
	out := te.must("idea", "shape", "try-a-quiet-mode")
	if !strings.Contains(out, "Shaped idea try-a-quiet-mode") || !strings.Contains(out, "shaped 2 times") {
		t.Errorf("shape: %q", out)
	}
	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# Try a quiet mode\n") || !strings.Contains(string(b), "Far too noisy.") || !strings.Contains(string(b), "<!--") {
		t.Errorf("the template was not prefilled from the latest pitch:\n%s", b)
	}
	if ts := te.temps(); len(ts) != 0 {
		t.Errorf("temporary files left: %v", ts)
	}
}

func TestShapeInEditorAgainAfterProblems(t *testing.T) {
	te := newTest(t)
	te.must("idea", "add", "Try a quiet mode")
	te.e.IsInteractive = func() bool { return true }
	seen := filepath.Join(te.tmp, "seen.md")
	te.editor(seen, noSignal, goodPitch)
	asked := 0
	te.e.Confirm = func(q string) (bool, error) { asked++; return true, nil }
	code, out, errs := te.run("idea", "shape", "try-a-quiet-mode", "--json")
	if code != 0 || asked != 1 || !strings.Contains(errs, "Signal") {
		t.Fatalf("code %d, asked %d, stderr %q", code, asked, errs)
	}
	var o workapi.IdeaOut
	if err := json.Unmarshal([]byte(out), &o); err != nil || o.Pitch == nil || o.Pitch.Signal != "Fewer complaints." {
		t.Errorf("shape --json: %q, %v", out, err)
	}
	if b, _ := os.ReadFile(seen); !strings.Contains(string(b), "## Problem\nToo noisy.") {
		t.Errorf("the second edit did not reopen the edited file:\n%s", b)
	}
	if ts := te.temps(); len(ts) != 0 {
		t.Errorf("temporary files left: %v", ts)
	}
}

func TestShapeInEditorStopKeepsFile(t *testing.T) {
	te := newTest(t)
	te.must("idea", "add", "Try a quiet mode")
	te.e.IsInteractive = func() bool { return true }
	te.editor(filepath.Join(te.tmp, "seen.md"), noSignal)
	te.e.Confirm = func(string) (bool, error) { return false, nil }
	errs := te.fails("idea", "shape", "try-a-quiet-mode")
	ts := te.temps()
	if len(ts) != 1 || !strings.Contains(errs, "Signal") || !strings.Contains(errs, "the pitch is kept at "+ts[0]) {
		t.Fatalf("temps %v, stderr %q", ts, errs)
	}
	if b, _ := os.ReadFile(ts[0]); string(b) != noSignal {
		t.Errorf("kept file: %q", b)
	}
	// An editor that fails keeps the file too.
	te.env["EDITOR"] = "false"
	if errs := te.fails("idea", "shape", "try-a-quiet-mode"); !strings.Contains(errs, "the editor false failed") || !strings.Contains(errs, "kept at") {
		t.Errorf("failing editor: %q", errs)
	}
}

func TestParse(t *testing.T) {
	for _, c := range []struct {
		args    []string
		allowed []string
		want    Flags
		ok      bool
	}{
		{[]string{"a", "--json"}, []string{"json"}, Flags{JSON: true, Args: []string{"a"}}, true},
		{[]string{"--from", "-", "s"}, []string{"from"}, Flags{From: "-", Args: []string{"s"}}, true},
		{[]string{"--appetite=200k", "--reason", "why not", "--yes"}, []string{"appetite", "reason", "yes"},
			Flags{Appetite: "200k", Reason: "why not", Yes: true}, true},
		{[]string{"-"}, nil, Flags{Args: []string{"-"}}, true},
		{[]string{"--slug", "a", "--slug", "b"}, []string{"slug"}, Flags{}, false},
		{[]string{"--slug="}, []string{"slug"}, Flags{}, false},
		{[]string{"--slug"}, []string{"slug"}, Flags{}, false},
		{[]string{"--json=true"}, []string{"json"}, Flags{}, false},
		{[]string{"-j"}, []string{"json"}, Flags{}, false},
		{[]string{"--reason", "x"}, []string{"json"}, Flags{}, false},
	} {
		got, ok := Parse(c.args, c.allowed...)
		if ok != c.ok || (ok && fmt.Sprint(got) != fmt.Sprint(c.want)) {
			t.Errorf("Parse(%q, %q) = %+v, %v; want %+v, %v", c.args, c.allowed, got, ok, c.want, c.ok)
		}
	}
}

func TestRepoIsTheRootAbove(t *testing.T) {
	te := newTest(t)
	if got, err := te.e.Repo(); err != nil || got != te.repo {
		t.Errorf("Repo() = %q, %v; want %q", got, err, te.repo)
	}
	t.Chdir(te.tmp)
	if _, err := te.e.Repo(); err == nil || !strings.Contains(err.Error(), "not inside a repo") {
		t.Errorf("outside a repo: %v", err)
	}
}
