package initrun

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/initrecord"
)

const (
	areasSplit = "```json\n" + `{"tests":[],"docs":["*.md"],"other":[]}` + "\n```"
	areasReply = "```json\n" + `{"areas":[` +
		`{"name":"zz-area-marker","promise":"zz-promise-marker people see.","sources":["web/"]},` +
		`{"name":"zz-second-marker","promise":"zz-second-promise.","sources":["main.go"]}]}` + "\n```"
)

// areaAgent answers the areas phase's sessions, the split and the grouping,
// and gives every other session to next, when set.
type areaAgent struct {
	mu     sync.Mutex
	starts int
	next   adapter.Adapter
}

func (a *areaAgent) Name() string { return "fake" }

func (a *areaAgent) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	text := ""
	switch {
	case strings.HasPrefix(s.System, strings.TrimRight(areas.Sources, "\n")):
		text = areasSplit
	case strings.HasPrefix(s.System, strings.TrimRight(areas.Areas, "\n")):
		text = areasReply
	case a.next != nil:
		return a.next.Start(ctx, s)
	}
	a.mu.Lock()
	a.starts++
	a.mu.Unlock()
	return fakeSession{f: &fakeAgent{reports: []adapter.Tokens{{Input: 10, Output: 2}},
		result: adapter.Result{Text: text, Model: "fake-model", Tokens: adapter.Tokens{Input: 10, Output: 2}, SessionID: s.SessionID}}, usage: s.Usage}, nil
}

// areasEnv is a repo with source files in web/ and main.go and a README,
// committed, with a gather stored and agent a ready.
func areasEnv(t *testing.T, a adapter.Adapter) Env {
	t.Helper()
	dir := newRepo(t)
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "web/app.js", "let a\n")
	write(t, dir, "README.md", "# App\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: Add files")
	env := testEnv(t, dir, onePR(), nil)
	if code, _, errOut := runInit(env, "--gather"); code != 0 {
		t.Fatalf("gather: %d %q", code, errOut)
	}
	env.Agent = func(string) (adapter.Adapter, error) { return a, nil }
	return env
}

func TestAreasPrintsTheAreas(t *testing.T) {
	t.Parallel()
	a := &areaAgent{}
	env := areasEnv(t, a)
	code, out, errOut := runInit(env, "--areas")
	if code != 0 || a.starts != 2 {
		t.Fatalf("code %d, %d sessions\nstdout %s\nstderr %s", code, a.starts, out, errOut)
	}
	for _, want := range []string{"Listed 4 files", "2 docs", "2 source", "- zz-area-marker (1 file): zz-promise-marker people see.", "- zz-second-marker (1 file)", "Model: fake-model"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "session") {
		t.Errorf("stdout names a session:\n%s", out)
	}
	st, err := env.Store(context.Background(), env.Proc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, ok, err := initrecord.Of(st).Areas(context.Background()); !ok || err != nil {
		t.Errorf("areas stored: %v %v", ok, err)
	}
	if spent, err := initrecord.Of(st).Spending(context.Background()); err != nil || len(spent) == 0 || spent[len(spent)-1].Phase != "areas" {
		t.Errorf("spending %v %v", spent, err)
	}
}

func TestAreasJSON(t *testing.T) {
	t.Parallel()
	env := areasEnv(t, &areaAgent{})
	code, out, errOut := runInit(env, "--areas", "--json")
	var doc struct {
		Source int
		Areas  []struct {
			Name  string
			Files int
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); code != 0 || err != nil || doc.Source != 2 || len(doc.Areas) != 2 || doc.Areas[0].Name != "zz-area-marker" {
		t.Errorf("code %d, err %v, doc %+v\nstdout %s\nstderr %s", code, err, doc, out, errOut)
	}
}

func TestAreasWithoutAGatherSaysToGatherFirst(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	a := &areaAgent{}
	code, out, errOut := runInit(testEnv(t, dir, nil, a), "--areas")
	if code == 0 || out != "" || a.starts != 0 || !strings.Contains(errOut, "run physa init --gather first") {
		t.Errorf("code %d, %d sessions\nstdout %s\nstderr %s", code, a.starts, out, errOut)
	}
}

func TestAreasOnAStoppedGatherWarnsAndRuns(t *testing.T) {
	t.Parallel()
	dir, _ := remoteRepo(t)
	a := &areaAgent{}
	env := fullEnv(t, dir, nil, a)
	shortWait(&env, 3*time.Minute)
	f, _ := stoppingForge(fixedNow(&env))
	env.Forge = func(string) forge.Forge { return f }
	if code, _, errOut := runInit(env, "--gather"); code != 2 {
		t.Fatalf("gather: code %d\n%s", code, errOut)
	}
	code, out, errOut := runInit(env, "--areas")
	if code != 0 || !strings.Contains(out, "- zz-") || !strings.Contains(errOut, "physa init: warning: the stored gather was stopped short by the forge") {
		t.Errorf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
}

func TestAreasRefusesAMappedRepoBeforeAnySession(t *testing.T) {
	t.Parallel()
	dir := mappedRepo(t, ".physa/product/index.md")
	a := &areaAgent{}
	code, out, errOut := runInit(fullEnv(t, dir, onePR(), a), "--areas")
	if code != 2 || out != "" || a.starts != 0 || !strings.Contains(errOut, "the repo already has a map") {
		t.Errorf("code %d, %d sessions\nstdout %s\nstderr %s", code, a.starts, out, errOut)
	}
}

func TestAreasIsBadUsageWithOtherFlags(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--gather"}, {"--propose"}, {"--recover"}, {"--write"}, {"--push"}, {"--pr"}, {"--hooks"},
		{"--answers", "x"}, {"--yes"}, {"--only", "x"}, {"--since", "2026-01-01"}, {"--max-prs", "3"}} {
		if code, _, _ := runInit(testEnv(t, t.TempDir(), nil, nil), append([]string{"--areas"}, args...)...); code != command.BadUsage {
			t.Errorf("--areas %v: code %d, want bad usage", args, code)
		}
	}
}

// TestAreasNeverReachTheMap is acceptance 4's failure case: areas with
// distinctive names and promises leave the repo untouched, and the map
// written from what init stored holds none of their words.
func TestAreasNeverReachTheMap(t *testing.T) {
	t.Parallel()
	dir, _ := remoteRepo(t)
	write(t, dir, "web/app.js", "let a\n")
	gitIn(t, dir, "add", "web/app.js")
	gitIn(t, dir, "commit", "-q", "-m", "feat: Add web", "--", "web/app.js")
	a := &areaAgent{next: newRoutedAgent()}
	env := fullEnv(t, dir, &prForge{mergedFake: mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}}, a)
	if code, _, errOut := runInit(env, "--gather"); code != 0 {
		t.Fatalf("gather: %d %s", code, errOut)
	}
	before := personState(t, dir)
	if code, out, errOut := runInit(env, "--areas"); code != 0 || !strings.Contains(out, "zz-area-marker") {
		t.Fatalf("areas: %d\n%s\n%s", code, out, errOut)
	}
	if after := personState(t, dir); after != before {
		t.Errorf("--areas changed the working copy:\nbefore %s\nafter  %s", before, after)
	}
	if got := gitIn(t, dir, "branch", "--list", "physa/*"); strings.TrimSpace(got) != "" {
		t.Errorf("--areas made a branch: %s", got)
	}
	if got := gitIn(t, dir, "ls-files", ".physa"); got != "" {
		t.Errorf(".physa is tracked: %s", got)
	}
	for _, args := range [][]string{{"--propose"}, {"--recover", "--write"}} {
		if code, _, errOut := runInit(env, args...); code != 0 {
			t.Fatalf("%v: %d %s", args, code, errOut)
		}
	}
	st, err := env.Store(context.Background(), env.Proc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := initrecord.Of(st).Areas(context.Background())
	st.Close()
	if !ok || err != nil {
		t.Fatalf("the areas were not kept through propose and recover: %v %v", ok, err)
	}
	files := strings.Fields(gitIn(t, dir, "ls-tree", "-r", "--name-only", "physa/init"))
	if len(files) < 3 {
		t.Fatalf("physa/init holds %v", files)
	}
	for _, f := range files {
		body := gitIn(t, dir, "show", "physa/init:"+f)
		for _, marker := range []string{"zz-area-marker", "zz-second-marker", "zz-promise-marker", "zz-second-promise"} {
			if strings.Contains(body, marker) || strings.Contains(f, marker) {
				t.Errorf("%s holds %q", f, marker)
			}
		}
	}
}
