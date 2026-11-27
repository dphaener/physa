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

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/enginestart"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/proc"
	"github.com/dphaener/physa/internal/workapi"
)

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
	repo string
	tmp  string
}

// newTest runs an engine with the idea and pursuit methods in this process on a
// short temporary settings directory, makes a git repo and works in its
// subdirectory sub, both passed in the Env's process state. The person is
// "tester", nothing is interactive and nothing may spawn an engine. It sets
// TMPDIR, which the editor's temporary files are made in, so its tests are
// serial.
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
	if repo, err = filepath.EvalSymlinks(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
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
	te := &tenv{t: t, repo: repo, tmp: tmp}
	sub := filepath.Join(repo, "sub")
	e := New(proc.State{Dir: sub, Wd: sub, Env: []string{"XDG_CONFIG_HOME=" + tmp}}, nil)
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

func (te *tenv) run(args ...string) (int, string, string) {
	te.t.Helper()
	var out, errb bytes.Buffer
	root := command.New("physa", "", nil, nil, te.e.command())
	code := command.Execute(context.Background(), root, args, &out, &errb)
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

func TestAddShowsUniqueSlugs(t *testing.T) {
	te := newTest(t)
	if out := te.must("idea", "add", "Try a quiet mode"); out != "Added idea try-a-quiet-mode\n" {
		t.Errorf("add: %q", out)
	}
	if out := te.must("idea", "add", "Try a quiet mode"); out != "Added idea try-a-quiet-mode-2\n" {
		t.Errorf("second add: %q", out)
	}
	var o client.IdeaOut
	if err := json.Unmarshal([]byte(te.must("idea", "add", "Show pursuits on the phone", "--slug", "phone-pursuits", "--json")), &o); err != nil {
		t.Fatal(err)
	}
	if o.Slug != "phone-pursuits" || o.Person != "tester" || o.Text != "Show pursuits on the phone" {
		t.Errorf("add --json: %+v", o)
	}
	if errs := te.fails("idea", "add", "Again", "--slug=phone-pursuits"); !strings.Contains(errs, "phone-pursuits") {
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
		{"idea", "add", "x", "--slug="}, {"idea", "add", "x", "--json=maybe"}, {"idea", "add", "x", "--json=false"},
		{"idea", "list", "--slug", "a"}, {"idea", "show", "a", "b"}, {"idea", "show", "-h"}, {"idea", "-j"},
	} {
		code, out, errs := te.run(args...)
		if code != 2 || out != "" || !strings.Contains(errs, "usage: physa idea") {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
}

func TestIdeaFlagsAreDeclared(t *testing.T) {
	te := newTest(t)
	te.must("idea", "add", "First idea", "--slug", "first", "--slug=second")
	for _, args := range [][]string{
		{"idea", "--json"}, {"idea", "list", "--json", "--json"}, {"idea", "show", "--json", "second"},
		{"idea", "show", "--", "second"}, {"idea", "show", "second", "--json"},
	} {
		code, out, errs := te.run(args...)
		if code != 0 || errs != "" || out == "" {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
	if out := te.must("idea", "list", "--json"); !strings.Contains(out, `"slug": "second"`) && !strings.Contains(out, `"slug":"second"`) {
		t.Errorf("the last --slug wins: %q", out)
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
	te.must("idea", "add", "Show pursuits on the phone", "--slug", "phone-pursuits")

	out := te.must("idea", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "Slug") || !strings.HasPrefix(lines[1], "phone-pursuits") ||
		!strings.HasSuffix(lines[1], "not pursued") || !strings.HasSuffix(lines[2], "not pursued") || !strings.Contains(lines[2], "tester") {
		t.Errorf("list:\n%s", out)
	}
	if bare := te.must("idea"); bare != out {
		t.Errorf("bare idea %q, list %q", bare, out)
	}
	var ideas []client.IdeaOut
	if err := json.Unmarshal([]byte(te.must("idea", "list", "--json")), &ideas); err != nil {
		t.Fatal(err)
	}
	if len(ideas) != 2 || ideas[0].Slug != "phone-pursuits" || ideas[1].Pursuit != "" {
		t.Errorf("list --json: %+v", ideas)
	}

	out = te.must("idea", "show", "try-a-quiet-mode")
	for _, want := range []string{"Try a quiet mode\n", "slug      try-a-quiet-mode", "person    tester", "facts     not pursued", "pursuits  none"} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("plain show carries escapes: %q", out)
	}
	var o client.IdeaOut
	if err := json.Unmarshal([]byte(te.must("idea", "show", "try-a-quiet-mode", "--json")), &o); err != nil {
		t.Fatal(err)
	}
	if o.Slug != "try-a-quiet-mode" || o.Pursuit != "" || len(o.Pursuits) != 0 {
		t.Errorf("show --json: %+v", o)
	}
	if strings.Contains(out, "itch") || strings.Contains(out, "shap") {
		t.Errorf("show speaks of pitches or shaping:\n%s", out)
	}
	if errs := te.fails("idea", "show", "nope"); !strings.Contains(errs, "nope") {
		t.Errorf("unknown slug: %q", errs)
	}
}

func TestMissingSlugOffTerminal(t *testing.T) {
	te := newTest(t)
	code, _, errs := te.run("idea", "show")
	if code != 2 || !strings.Contains(errs, "name an idea; there are none yet") {
		t.Errorf("no ideas: code %d, stderr %q", code, errs)
	}
	te.must("idea", "add", "Try a quiet mode")
	te.must("idea", "add", "Show pursuits on the phone", "--slug", "phone-pursuits")
	code, out, errs := te.run("idea", "show")
	want := "physa idea show: name an idea; the ideas are:\n  phone-pursuits    Show pursuits on the phone\n  try-a-quiet-mode  Try a quiet mode\nusage: physa idea show"
	if code != 2 || out != "" || !strings.HasPrefix(errs, want) {
		t.Errorf("code %d, stdout %q, stderr:\n%s", code, out, errs)
	}
	if code, _, errs := te.run("idea", "shape", "try-a-quiet-mode"); code != 2 || !strings.Contains(errs, "usage: physa idea") {
		t.Errorf("idea shape is gone: code %d, stderr %q", code, errs)
	}
}

func TestPickerChoosesSlugOnTerminal(t *testing.T) {
	te := newTest(t)
	te.must("idea", "add", "Try a quiet mode")
	te.must("idea", "add", "Show pursuits on the phone", "--slug", "phone-pursuits")
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
	if len(got) != 2 || got[0].Slug != "phone-pursuits" || got[1].Detail != "not pursued" || !strings.Contains(out, "slug      try-a-quiet-mode") {
		t.Errorf("items %+v, show %q", got, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("styled output into a buffer carries escapes: %q", out)
	}
	te.e.Pick = func(string, []picker.Item) (string, error) { return "", picker.ErrAborted }
	te.fails("idea", "show")
}

func TestRepoIsTheRootAbove(t *testing.T) {
	te := newTest(t)
	if got, err := te.e.Repo(); err != nil || got != te.repo {
		t.Errorf("Repo() = %q, %v; want %q", got, err, te.repo)
	}
	te.e.Proc.Dir, te.e.Proc.Wd = te.tmp, te.tmp
	if _, err := te.e.Repo(); err == nil || !strings.Contains(err.Error(), "not inside a repo") {
		t.Errorf("outside a repo: %v", err)
	}
}
