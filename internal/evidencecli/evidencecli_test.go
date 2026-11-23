//go:build linux || darwin

package evidencecli

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

	"github.com/dphaener/physa/internal/apiclient"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/enginestart"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/state"
	stateevidence "github.com/dphaener/physa/internal/state/evidence"
	"github.com/dphaener/physa/internal/workapi"
)

// TestMain never lets the test binary act as an engine.
func TestMain(m *testing.M) {
	if i := slices.Index(os.Args, "engine"); i >= 0 && i+1 < len(os.Args) && os.Args[i+1] == "run" {
		fmt.Fprintln(os.Stderr, "evidencecli test binary: refusing to run as an engine")
		os.Exit(3)
	}
	os.Exit(m.Run())
}

// newTest runs an engine that polls version control (no forge) on a short
// temporary settings directory, in a fresh git repo that already holds the
// commits with the given messages, one new file each. Nothing is interactive.
func newTest(t *testing.T, messages ...string) *ideacli.Env {
	t.Helper()
	tmp, err := os.MkdirTemp("", "px")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	t.Setenv("XDG_CONFIG_HOME", tmp)
	repo := filepath.Join(tmp, "repo")
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	for i, m := range messages {
		name := fmt.Sprintf("f%d.txt", i)
		if err := os.WriteFile(filepath.Join(repo, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", name)
		git("commit", "-q", "-m", m)
	}
	t.Chdir(repo)
	dir := filepath.Join(tmp, "physa")
	timers, intents := stateevidence.Options(func(string) forge.Forge { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- engine.Run(ctx, engine.Options{Dir: dir, Idle: time.Minute, Version: "test", Methods: workapi.Methods,
			State: state.Options{Timers: timers, Intents: intents}})
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
	e := ideacli.New(nil)
	e.Start = enginestart.Options{Command: func(dir string, _ time.Duration) *exec.Cmd {
		t.Errorf("a command tried to start an engine on %s", dir)
		return exec.Command("false")
	}}
	e.Getenv = func(string) string { return "" }
	e.IsInteractive = func() bool { return false }
	return e
}

func evidence(e *ideacli.Env, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	root := &cli.Command{Name: "physa", Subs: []*cli.Command{commandFor(e)}}
	code := cli.Dispatch(root, append([]string{"evidence"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

// waitFor runs the command until its output holds want: the engine polls on
// its own a moment after it opens.
func waitFor(t *testing.T, e *ideacli.Env, want string, args ...string) string {
	t.Helper()
	var out string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		code, o, errs := evidence(e, args...)
		if code != 0 {
			t.Fatalf("%v: code %d, stderr %q", args, code, errs)
		}
		if out = o; strings.Contains(out, want) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("%v: output never held %q:\n%s", args, want, out)
		}
	}
}

func TestNoEvidenceYet(t *testing.T) {
	e := newTest(t)
	code, out, errs := evidence(e)
	if code != 0 || strings.TrimSpace(out) != "No evidence yet" || errs != "" {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, _ = evidence(e, "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Errorf("json: code %d, stdout %q", code, out)
	}
}

func TestListsFactsPlainAndFiltered(t *testing.T) {
	e := newTest(t, "feat: a\n\nCapability: alpha", "feat: b\n\nCapability: beta")
	out := waitFor(t, e, "beta")
	for _, want := range []string{"Fact", "Capability", "landed", "alpha", "beta", "UTC"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("escapes off a terminal:\n%s", out)
	}
	_, out, _ = evidence(e, "--capability", "alpha")
	if !strings.Contains(out, "alpha") || strings.Contains(out, "beta") {
		t.Errorf("--capability alpha:\n%s", out)
	}
	var facts []workapi.EvidenceOut
	_, out, _ = evidence(e, "--limit=1", "--json")
	if err := json.Unmarshal([]byte(out), &facts); err != nil || len(facts) != 1 {
		t.Errorf("--limit 1 --json: %v, %q", err, out)
	}
	if _, out, _ = evidence(e, "--pursuit", "no-such-pursuit"); strings.TrimSpace(out) != "No evidence yet" {
		t.Errorf("--pursuit no-such-pursuit:\n%s", out)
	}
}

func TestBadUsageExitsTwo(t *testing.T) {
	e := newTest(t)
	for _, args := range [][]string{{"--bogus"}, {"extra"}, {"--limit", "x"}, {"--limit", "0"}, {"--limit"}, {"--json=1"}, {"--pursuit", "a", "--pursuit", "b"}} {
		code, out, errs := evidence(e, args...)
		if code != 2 || out != "" || !strings.Contains(errs, "physa evidence [--capability <slug>]") {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
}

func TestCellsAndDuration(t *testing.T) {
	secs := int64(3*3600 + 5*60)
	cases := []struct {
		f    workapi.EvidenceOut
		want string
	}{
		{workapi.EvidenceOut{Kind: "lead_time", PR: 12, Commit: "3f9a2c1d", Capability: "cli", At: "2026-11-22T10:00:00Z", Seconds: &secs},
			"lead time  #12  cli  2026-11-22 10:00 UTC  lead time 3h 5m"},
		{workapi.EvidenceOut{Kind: "reverted", Commit: "3f9a2c1d", ByCommit: "aabbccdd", At: "2026-11-22T10:00:00Z"},
			"reverted  3f9a2c1d  -  2026-11-22 10:00 UTC  by aabbccdd"},
		{workapi.EvidenceOut{Kind: "ci_passed_after_failing", PR: 3, At: "2026-11-22T10:00:00Z"},
			"CI passed after failing  #3  -  2026-11-22 10:00 UTC"},
	}
	for _, c := range cases {
		if got := Line(c.f); got != c.want {
			t.Errorf("Line = %q, want %q", got, c.want)
		}
	}
	for secs, want := range map[int64]string{30: "30s", 720: "12m", 11100: "3h 5m", 2*86400 + 4*3600: "2d 4h"} {
		if got := Duration(secs); got != want {
			t.Errorf("Duration(%d) = %q, want %q", secs, got, want)
		}
	}
}
