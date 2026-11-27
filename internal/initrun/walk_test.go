package initrun

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

var walkAreaOf = regexp.MustCompile("## The area `([^`]+)`")

// walkReplies answers each area of areasReply by its name.
var walkReplies = map[string]string{
	"zz-area-marker":   "```json\n" + `{"capabilities":[{"name":"zz-cap-marker","promise":"zz-cap-promise.","sources":["web/app.js"],"tests":[],"docs":[]}],"shared":[]}` + "\n```",
	"zz-second-marker": "```json\n" + `{"capabilities":[{"name":"zz-cap-second","promise":"zz-cap-second-promise.","sources":["main.go"],"tests":[],"docs":[]}],"shared":[]}` + "\n```",
}

// walkAgent answers the walk phase's sessions by the area their message names
// and gives every other session to next, when set.
type walkAgent struct {
	mu     sync.Mutex
	starts int
	next   adapter.Adapter
}

func (a *walkAgent) Name() string { return "fake" }

func (a *walkAgent) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	if !strings.HasPrefix(s.System, strings.TrimRight(walk.Walk, "\n")) {
		return a.next.Start(ctx, s)
	}
	a.mu.Lock()
	a.starts++
	a.mu.Unlock()
	m := walkAreaOf.FindStringSubmatch(s.Prompt)
	text := ""
	if m != nil {
		text = walkReplies[m[1]]
	}
	return fakeSession{f: &fakeAgent{reports: []adapter.Tokens{{Input: 10, Output: 2}},
		result: adapter.Result{Text: text, Model: "fake-model", Tokens: adapter.Tokens{Input: 10, Output: 2}, SessionID: s.SessionID}}, usage: s.Usage}, nil
}

// walkEnv is areasEnv with the areas phase already run.
func walkEnv(t *testing.T) (Env, *walkAgent) {
	t.Helper()
	a := &walkAgent{next: &areaAgent{}}
	env := areasEnv(t, a)
	if code, _, errOut := runInit(env, "--areas"); code != 0 {
		t.Fatalf("areas: %d %s", code, errOut)
	}
	return env, a
}

func TestWalkPrintsTheFootprints(t *testing.T) {
	t.Parallel()
	env, a := walkEnv(t)
	code, out, errOut := runInit(env, "--walk")
	if code != 0 || a.starts != 2 {
		t.Fatalf("code %d, %d sessions\nstdout %s\nstderr %s", code, a.starts, out, errOut)
	}
	for _, want := range []string{"zz-cap-marker", "zz-cap-second", "Model: fake-model"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	st, err := env.Store(context.Background(), env.Proc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if rec, ok, err := walk.Load(t.Context(), initrecord.Of(st)); !ok || err != nil || len(rec.Capabilities) != 2 {
		t.Errorf("walk stored: %+v %v %v", rec, ok, err)
	}
	if spent, err := initrecord.Of(st).Spending(context.Background()); err != nil || len(spent) == 0 || spent[len(spent)-1].Phase != "walk" {
		t.Errorf("spending %v %v", spent, err)
	}
}

func TestWalkJSON(t *testing.T) {
	t.Parallel()
	env, _ := walkEnv(t)
	code, out, errOut := runInit(env, "--walk", "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); code != 0 || err != nil || !strings.Contains(out, "zz-cap-marker") {
		t.Errorf("code %d, err %v\nstdout %s\nstderr %s", code, err, out, errOut)
	}
}

func TestWalkWithoutAreasSaysToRunAreasFirst(t *testing.T) {
	t.Parallel()
	env := areasEnv(t, &areaAgent{})
	a := &areaAgent{}
	env.Agent = func(string) (adapter.Adapter, error) { a.starts++; return a, nil }
	code, out, errOut := runInit(env, "--walk")
	if code != 2 || out != "" || a.starts != 0 || !strings.Contains(errOut, "run physa init --areas first") {
		t.Errorf("code %d, %d agents\nstdout %s\nstderr %s", code, a.starts, out, errOut)
	}
}

func TestWalkOnAStoppedGatherWarnsAndRuns(t *testing.T) {
	t.Parallel()
	dir, _ := remoteRepo(t)
	a := &walkAgent{next: &areaAgent{}}
	env := fullEnv(t, dir, nil, a)
	shortWait(&env, 3*time.Minute)
	f, _ := stoppingForge(fixedNow(&env))
	env.Forge = func(string) forge.Forge { return f }
	if code, _, errOut := runInit(env, "--gather"); code != 2 {
		t.Fatalf("gather: code %d\n%s", code, errOut)
	}
	if code, _, errOut := runInit(env, "--areas"); code != 0 {
		t.Fatalf("areas: %d %s", code, errOut)
	}
	code, out, errOut := runInit(env, "--walk")
	if code != 0 || !strings.Contains(out, "zz-cap-") || !strings.Contains(errOut, "physa init: warning: the stored gather was stopped short by the forge") {
		t.Errorf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
}

func TestWalkIsBadUsageWithOtherFlags(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--areas"}, {"--gather"}, {"--propose"}, {"--recover"}, {"--write"}, {"--push"}, {"--hooks"},
		{"--answers", "x"}, {"--yes"}, {"--only", "x"}, {"--since", "2026-01-01"}, {"--max-prs", "3"}} {
		if code, _, _ := runInit(testEnv(t, t.TempDir(), nil, nil), append([]string{"--walk"}, args...)...); code != command.BadUsage {
			t.Errorf("--walk %v: code %d, want bad usage", args, code)
		}
	}
}

// TestWalkNeverReachesTheMap: capabilities and file paths with distinctive
// words are kept in init's record under .git/physa, the repo is untouched, and
// the map written from what init stored holds none of them.
func TestWalkNeverReachesTheMap(t *testing.T) {
	t.Parallel()
	dir, _ := remoteRepo(t)
	write(t, dir, "web/app.js", "let a\n")
	gitIn(t, dir, "add", "web/app.js")
	gitIn(t, dir, "commit", "-q", "-m", "feat: Add web", "--", "web/app.js")
	a := &walkAgent{next: &areaAgent{next: newRoutedAgent()}}
	env := fullEnv(t, dir, &prForge{mergedFake: mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}}, a)
	env.Store = func(context.Context, string) (*store.Store, error) {
		return store.Open(context.Background(), filepath.Join(dir, ".git", "physa"))
	}
	for _, args := range [][]string{{"--gather"}, {"--areas"}} {
		if code, _, errOut := runInit(env, args...); code != 0 {
			t.Fatalf("%v: %d %s", args, code, errOut)
		}
	}
	before := personState(t, dir)
	if code, out, errOut := runInit(env, "--walk"); code != 0 || !strings.Contains(out, "zz-cap-marker") {
		t.Fatalf("walk: %d\n%s\n%s", code, out, errOut)
	}
	if after := personState(t, dir); after != before {
		t.Errorf("--walk changed the working copy:\nbefore %s\nafter  %s", before, after)
	}
	if got := gitIn(t, dir, "branch", "--list", "physa/*"); strings.TrimSpace(got) != "" {
		t.Errorf("--walk made a branch: %s", got)
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
	rec, ok, err := walk.Load(t.Context(), initrecord.Of(st))
	st.Close()
	if !ok || err != nil || len(rec.Capabilities) != 2 {
		t.Fatalf("the footprints were not kept through propose and recover: %+v %v %v", rec, ok, err)
	}
	files := strings.Fields(gitIn(t, dir, "ls-tree", "-r", "--name-only", "physa/init"))
	if len(files) < 3 {
		t.Fatalf("physa/init holds %v", files)
	}
	for _, f := range files {
		body := gitIn(t, dir, "show", "physa/init:"+f)
		for _, word := range []string{"zz-cap-marker", "zz-cap-second", "zz-cap-promise", "web/app.js"} {
			if strings.Contains(body, word) || strings.Contains(f, word) {
				t.Errorf("%s holds %q", f, word)
			}
		}
	}
}
