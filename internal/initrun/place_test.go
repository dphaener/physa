package initrun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/place"
)

// placeEnv is a repo whose PR 3 touches the sources the walk names and whose
// PR 4 touches only the README, which no capability lists, gathered, divided
// into areas and walked. The agent it then gives fails, and the returned
// count is how often anything asked for one.
func placeEnv(t *testing.T) (Env, *int) {
	t.Helper()
	dir := newRepo(t)
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "web/app.js", "let a\n")
	gitIn(t, dir, "add", "main.go", "web")
	gitIn(t, dir, "commit", "-q", "-m", "feat: Add the app (#3)")
	write(t, dir, "README.md", "# App\n")
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "docs: Add a readme (#4)")
	env := testEnv(t, mergedFake{prs: []forge.PR{{Number: 3, Title: "App", MergedAt: time.Now()}, {Number: 4, Title: "Readme", MergedAt: time.Now()}}}, nil)
	if code, _, errOut := runInit(env, "--gather"); code != 0 {
		t.Fatalf("gather: %d %q", code, errOut)
	}
	a := &walkAgent{next: &areaAgent{}}
	env.Agent = func(string) (adapter.Adapter, error) { return a, nil }
	for _, phase := range []string{"--areas", "--walk"} {
		if code, _, errOut := runInit(env, phase); code != 0 {
			t.Fatalf("%s: %d %s", phase, code, errOut)
		}
	}
	calls := 0
	env.Agent = func(string) (adapter.Adapter, error) {
		calls++
		return nil, errors.New("the place phase resolved an agent")
	}
	return env, &calls
}

func TestPlacePrintsTheCountsWithNoAgentCall(t *testing.T) {
	env, calls := placeEnv(t)
	code, out, errOut := runInit(env, "--place")
	if code != 0 || *calls != 0 {
		t.Fatalf("code %d, %d agent calls\nstdout %s\nstderr %s", code, *calls, out, errOut)
	}
	for _, want := range []string{"PRs placed: 1", "PRs placed nowhere, " + place.NoOwner + ": 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, ok, err := initrecord.Of(st).Place(context.Background()); !ok || err != nil {
		t.Errorf("place stored: %v %v", ok, err)
	}
	if spent, err := initrecord.Of(st).Spending(context.Background()); err != nil || spent[len(spent)-1].Phase == "place" {
		t.Errorf("a placement with no session spent something: %v %v", spent, err)
	}
}

func TestPlaceJSON(t *testing.T) {
	env, calls := placeEnv(t)
	code, out, errOut := runInit(env, "--place", "--json")
	var res place.Result
	if err := json.Unmarshal([]byte(out), &res); code != 0 || err != nil || *calls != 0 || res.PRsPlaced != 1 {
		t.Errorf("code %d, err %v, %d agent calls, %+v\nstdout %s\nstderr %s", code, err, *calls, res, out, errOut)
	}
	if len(res.PRsNowhere) != 2 || res.PRsNowhere[1].Reason != place.NoOwner || res.PRsNowhere[1].Count != 1 {
		t.Errorf("nowhere: %+v", res.PRsNowhere)
	}
}

func TestPlaceWithoutAWalkSaysToRunWalkFirst(t *testing.T) {
	env := areasEnv(t, &areaAgent{})
	calls := 0
	env.Agent = func(string) (adapter.Adapter, error) { calls++; return nil, errors.New("agent") }
	code, out, errOut := runInit(env, "--place")
	if code != 2 || out != "" || calls != 0 || !strings.Contains(errOut, "run physa init --walk first") {
		t.Errorf("code %d, %d agents\nstdout %s\nstderr %s", code, calls, out, errOut)
	}
}

func TestPlaceOnAStoppedGatherWarnsAndRuns(t *testing.T) {
	remoteRepo(t)
	a := &walkAgent{next: &areaAgent{}}
	env := fullEnv(t, nil, a)
	shortWait(&env, 3*time.Minute)
	f, _ := stoppingForge(fixedNow(&env))
	env.Forge = func(string) forge.Forge { return f }
	if code, _, errOut := runInit(env, "--gather"); code != 2 {
		t.Fatalf("gather: code %d\n%s", code, errOut)
	}
	for _, phase := range []string{"--areas", "--walk"} {
		if code, _, errOut := runInit(env, phase); code != 0 {
			t.Fatalf("%s: %d %s", phase, code, errOut)
		}
	}
	code, out, errOut := runInit(env, "--place")
	if code != 0 || !strings.Contains(out, "PRs placed") || !strings.Contains(errOut, "physa init: warning: the stored gather was stopped short by the forge") {
		t.Errorf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
}

func TestPlaceIsBadUsageWithOtherFlags(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--areas"}, {"--walk"}, {"--gather"}, {"--propose"}, {"--recover"}, {"--write"}, {"--push"}, {"--hooks"},
		{"--answers", "x"}, {"--yes"}, {"--only", "x"}, {"--since", "2026-01-01"}, {"--max-prs", "3"}} {
		if code, _, _ := runInit(testEnv(t, nil, nil), append([]string{"--place"}, args...)...); code != command.BadUsage {
			t.Errorf("--place %v: code %d, want bad usage", args, code)
		}
	}
}

// TestPlaceNeverReachesTheMap: the placements are kept in init's record
// under .git/physa and the repo is untouched.
func TestPlaceNeverReachesTheMap(t *testing.T) {
	env, _ := placeEnv(t)
	dir := "."
	before := personState(t, dir)
	if code, _, errOut := runInit(env, "--place"); code != 0 {
		t.Fatalf("place: %d %s", code, errOut)
	}
	if after := personState(t, dir); after != before {
		t.Errorf("--place changed the working copy:\nbefore %s\nafter  %s", before, after)
	}
	if got := gitIn(t, dir, "branch", "--list", "physa/*"); strings.TrimSpace(got) != "" {
		t.Errorf("--place made a branch: %s", got)
	}
}
