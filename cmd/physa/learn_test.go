package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

// prForge answers one change with a PR, and no PR for the others.
type prForge struct {
	change string
	pr     forge.PR
}

func (f prForge) Name() string               { return "fake" }
func (f prForge) PRExists(int) (bool, error) { return false, nil }
func (f prForge) ChangePR(id string) (forge.PR, bool, error) {
	return f.pr, id == f.change, nil
}

func runLearn(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"map", "learn"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func learnRepo(t *testing.T) (dir, head string) {
	t.Helper()
	dir = trailersRepo(t)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitIn(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return dir, strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD~3"))
}

func TestLearnText(t *testing.T) {
	dir, id := learnRepo(t)
	t.Chdir(dir)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := prForge{change: id, pr: forge.PR{
		Number:   7,
		Comments: []forge.Comment{{Author: "ana", At: at, Kind: forge.KindReview, Body: "rename this"}},
		Commits:  []forge.Commit{{ID: id, At: at.Add(time.Hour)}},
	}}
	old := newForge
	newForge = func(string) forge.Forge { return f }
	t.Cleanup(func() { newForge = old })
	code, out, errOut := runLearn(t, "--signals")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"Forge: fake", "correction", "feat: ok", "Nothing to learn"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
}

func TestLearnJSON(t *testing.T) {
	dir, _ := learnRepo(t)
	t.Chdir(dir)
	code, out, _ := runLearn(t, "--signals", "--json")
	var got struct{ Version int }
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || got.Version != 1 {
		t.Errorf("code %d, version %d, stdout %q", code, got.Version, out)
	}
}

func TestLearnForgeUnavailable(t *testing.T) {
	dir, _ := learnRepo(t)
	t.Chdir(dir)
	useNoGH(t)
	code, out, _ := runLearn(t, "--signals")
	if code != 0 || !strings.Contains(out, "Forge: unavailable") {
		t.Errorf("code %d, stdout %q", code, out)
	}
}

func TestLearnWithoutSignals(t *testing.T) {
	code, out, errOut := runLearn(t)
	if code != 2 || out != "" || !strings.Contains(errOut, "drafting is planned") {
		t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestLearnOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	if code, _, errOut := runLearn(t, "--signals"); code != 2 || !strings.HasPrefix(errOut, "physa map learn: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestLearnBadSince(t *testing.T) {
	dir, _ := learnRepo(t)
	t.Chdir(dir)
	code, _, errOut := runLearn(t, "--signals", "--since=nosuch")
	if code != 2 || !strings.HasPrefix(errOut, "physa map learn: ") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestLearnBadUsage(t *testing.T) {
	for _, args := range [][]string{{"--since"}, {"--nosuch"}, {"x"}} {
		if _, ok := parseLearn(args); ok {
			t.Errorf("parseLearn(%v) ok, want not", args)
		}
	}
	o, ok := parseLearn([]string{"--signals", "--since", "abc", "--json"})
	if !ok || o.since != "abc" || !o.json || !o.signals {
		t.Errorf("got %+v, %v", o, ok)
	}
}

func TestLearnHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run([]string{"map", "learn", "--help"}, &stdout, &stderr)
	if !strings.Contains(stdout.String()+stderr.String(), "physa map learn --signals [--since <change>] [--json]") {
		t.Errorf("help lacks usage: %q %q", stdout.String(), stderr.String())
	}
}
