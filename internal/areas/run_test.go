package areas

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs/git"
)

// fakeAgent records every Spec and replies through reply, which sees the
// spec and how many specs came before it.
type fakeAgent struct {
	mu       sync.Mutex
	specs    []adapter.Spec
	reply    func(s adapter.Spec, n int) string
	noResume bool
	window   int
	late     int    // sessions started with a done ctx
	cancel   func() // when set, the first session cancels
}

type fakeSession struct{ res adapter.Result }

func (s fakeSession) Wait() (adapter.Result, error) { return s.res, nil }

func (f *fakeAgent) Name() string { return "fake" }

func (f *fakeAgent) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx.Err() != nil {
		f.late++ // the fake takes it, so a session started after a cancel shows
	}
	if f.cancel != nil {
		f.cancel()
	}
	if s.Resume && f.noResume {
		return nil, adapter.ErrNoResume
	}
	n := len(f.specs)
	f.specs = append(f.specs, s)
	return fakeSession{adapter.Result{Text: f.reply(s, n), Model: "fake-model", SessionID: s.SessionID,
		Tokens: adapter.Tokens{Input: 10, CachedInput: 2, Output: 3}}}, nil
}

// kind names what a recorded spec was for.
func kind(s adapter.Spec) string {
	switch {
	case s.Resume:
		return "resume"
	case strings.HasPrefix(s.System, skill(Sources)):
		return "split"
	case strings.HasPrefix(s.System, skill(Merge)):
		return "merge"
	case strings.Contains(s.System, AcrossParts):
		return "part"
	case strings.HasPrefix(s.System, skill(Areas)) && strings.Contains(s.Prompt, "did not land in exactly one area"):
		return "fresh-fix"
	case strings.HasPrefix(s.System, skill(Areas)):
		return "areas"
	}
	return "unknown"
}

func (f *fakeAgent) of(k string) []adapter.Spec {
	var out []adapter.Spec
	for _, s := range f.specs {
		if kind(s) == k {
			out = append(out, s)
		}
	}
	return out
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// sourceFiles are the fixture's source files: every file the split below
// leaves as source, Makefile among them though it matches no pattern.
var sourceFiles = []string{
	"Makefile", "app/auth/login.go", "app/auth/session.go", "app/billing/invoice.go",
	"app/billing/pay.go", "cmd/main.go", "web/cart/cart.js", "web/login/form.js",
}

// leftOut are the fixture's files that never reach a session.
var leftOut = map[string]string{
	"vendor/lib/x.go": sourceset.LeftVendored, "gen/api.pb.go": sourceset.LeftGenerated,
	"assets/logo.png": sourceset.LeftBinary, "yarn.lock": sourceset.LeftLock,
}

// fixture builds a committed git repo with source files in several
// folders, tests, docs, other files, and files to leave out.
func fixture(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	files := map[string]string{
		".gitattributes":       "vendor/** linguist-vendored\n",
		"gen/.gitattributes":   "api.pb.go linguist-generated=true\n",
		"spec/billing_test.go": "package spec\n",
		"docs/guide.md":        "# Guide\n",
		"README.md":            "# App\n",
		"config/ci.yml":        "on: push\n",
		"vendor/lib/x.go":      "package lib\n",
		"gen/api.pb.go":        "package gen\n",
		"assets/logo.png":      "\x89PNG\x00\x00\x01",
		"yarn.lock":            "# lock\n",
	}
	for _, f := range sourceFiles {
		files[f] = "code\nmore\n"
	}
	for rel, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	return dir
}

const splitAnswer = "```json\n" + `{"tests":["spec/","*_test.go","e2e/"],"docs":["docs/","*.md"],"other":["config/",".gitattributes"]}` + "\n```"

func fenceJSON(s string) string { return "Here.\n\n```json\n" + s + "\n```\n" }

// setup returns the store holding a gather and the session for dir.
func setup(t *testing.T, dir string, f *fakeAgent) (*initrecord.Store, Session) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.PutGather(context.Background(), store.Gather{Run: store.GatherRun{At: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}}); err != nil {
		t.Fatal(err)
	}
	repo, err := git.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return initrecord.Of(st), Session{Agent: func() (adapter.Adapter, error) { return f, nil }, Root: dir, Repo: repo,
		Now: func() time.Time { return time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC) }}
}

func load(t *testing.T, st *initrecord.Store) (Record, bool) {
	t.Helper()
	r, ok, err := Load(t.Context(), st)
	if err != nil {
		t.Fatal(err)
	}
	return r, ok
}

// exactlyOnce fails unless every source file is in exactly one stored area.
func exactlyOnce(t *testing.T, r Record) map[string]string {
	t.Helper()
	where := map[string]string{}
	for _, a := range r.Areas {
		if !sort.StringsAreSorted(a.Files) {
			t.Errorf("area %s files not sorted: %v", a.Name, a.Files)
		}
		for _, p := range a.Files {
			if where[p] != "" {
				t.Errorf("%s in %s and %s", p, where[p], a.Name)
			}
			where[p] = a.Name
		}
	}
	for _, p := range sourceFiles {
		if where[p] == "" {
			t.Errorf("%s in no area", p)
		}
	}
	if len(where) != len(sourceFiles) {
		t.Errorf("areas hold %d files, want %d", len(where), len(sourceFiles))
	}
	return where
}

// firstAreas leaves Makefile out and claims web/login/form.js in two areas,
// each by its exact path.
const firstAreas = `{"areas":[
 {"name":"Billing","promise":"People pay.","sources":["app/billing/","web/cart/"]},
 {"name":"sign-in","promise":"People sign in.","sources":["app/auth/","web/login/form.js"]},
 {"name":"accounts","promise":"People keep accounts.","sources":["web/login/form.js","cmd/"]}]}`

const fixAnswer = `{"add":{"sign-in":["web/login/form.js"],"build":["./Makefile"]},"new_areas":[{"name":"build","promise":"The app builds.","sources":["Makefile"]}]}`

func TestRunFixRoundResumesTheSession(t *testing.T) {
	dir := fixture(t)
	f := &fakeAgent{reply: func(s adapter.Spec, n int) string {
		switch kind(s) {
		case "split":
			return splitAnswer
		case "areas":
			return fenceJSON(firstAreas)
		}
		return fenceJSON(fixAnswer)
	}}
	st, s := setup(t, dir, f)
	var progress strings.Builder
	s.Progress = &progress
	res, err := Run(context.Background(), st, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.specs) != 3 {
		t.Fatalf("got %d sessions, want split, areas, fix", len(f.specs))
	}
	group, fix := f.specs[1], f.specs[2]
	if !fix.Resume || fix.SessionID != group.SessionID || group.SessionID == "" {
		t.Errorf("fix is not a resume of the areas session: %+v", fix)
	}
	for _, sp := range f.specs {
		if sp.Dir != dir || !sp.ReadOnly {
			t.Errorf("session not read-only in the root: %+v", sp)
		}
	}
	for _, p := range sourceFiles {
		named := strings.Contains(fix.Prompt, p)
		if want := p == "Makefile" || p == "web/login/form.js"; named != want {
			t.Errorf("fix prompt names %s: %v, want %v", p, named, want)
		}
	}
	r, ok := load(t, st)
	if !ok {
		t.Fatal("nothing stored")
	}
	where := exactlyOnce(t, r)
	if where["web/login/form.js"] != "sign-in" || where["Makefile"] != "build" || where["app/billing/pay.go"] != "billing" {
		t.Errorf("placed: %v", where)
	}
	if res.FixRounds != 1 || res.Parts != 1 || res.Source != len(sourceFiles) || res.Listed != 18 {
		t.Errorf("result: %+v", res)
	}
	if res.Tokens.Input != 30 || res.Model != "fake-model" {
		t.Errorf("tokens %v model %q", res.Tokens, res.Model)
	}
	a, _, _ := st.Areas(context.Background())
	if a.Input != 30 || a.Output != 9 || a.Model != "fake-model" || !a.GatherAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("stored row: %+v", a)
	}
	if len(r.Sessions) != 2 {
		t.Errorf("sessions: %v", r.Sessions)
	}
	if got := strings.Count(progress.String(), "physa init: areas: "); got != 3 {
		t.Errorf("progress lines %d:\n%s", got, progress.String())
	}
}

func TestRunNeverFixedStoresNothing(t *testing.T) {
	dir := fixture(t)
	f := &fakeAgent{reply: func(s adapter.Spec, n int) string {
		switch kind(s) {
		case "split":
			return splitAnswer
		case "areas":
			return fenceJSON(firstAreas)
		}
		return fenceJSON(`{"add":{},"new_areas":[]}`)
	}}
	st, s := setup(t, dir, f)
	_, err := Run(context.Background(), st, s)
	if err == nil {
		t.Fatal("no error")
	}
	t.Log("error: " + err.Error())
	for _, want := range []string{"2 source files", "Makefile", "web/login/form.js"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if got := len(f.of("resume")); got != MaxFixRounds {
		t.Errorf("%d fix rounds, want %d", got, MaxFixRounds)
	}
	if _, ok := load(t, st); ok {
		t.Error("areas stored after a failed run")
	}
}

func TestRunSplitIsWhatCodeExpands(t *testing.T) {
	dir := fixture(t)
	f := &fakeAgent{reply: func(s adapter.Spec, n int) string {
		if kind(s) == "split" {
			return splitAnswer
		}
		return fenceJSON(`{"areas":[{"name":"all","promise":"Everything.","sources":["app/","web/","cmd/","Makefile"]}]}`)
	}}
	st, s := setup(t, dir, f)
	res, err := Run(context.Background(), st, s)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := load(t, st)
	want := map[sourceset.Kind][]string{
		sourceset.Test:   {"spec/billing_test.go"},
		sourceset.Doc:    {"README.md", "docs/guide.md"},
		sourceset.Other:  {".gitattributes", "config/ci.yml", "gen/.gitattributes"},
		sourceset.Source: sourceFiles,
	}
	for k, w := range want {
		got := append([]string{}, r.Split.Files[k]...)
		sort.Strings(got)
		if strings.Join(got, " ") != strings.Join(w, " ") {
			t.Errorf("%s: got %v, want %v", k, got, w)
		}
	}
	if where := exactlyOnce(t, r); where["Makefile"] != "all" {
		t.Errorf("Makefile, matching no pattern, is not in an area: %v", where)
	}
	if strings.Join(r.Split.Unused, " ") != "e2e/" || strings.Join(res.Unused, " ") != "e2e/" {
		t.Errorf("unused: %v", r.Split.Unused)
	}
	for p, reason := range leftOut {
		for _, sp := range f.specs {
			if strings.Contains(sp.System+sp.Prompt, p) {
				t.Errorf("%s reached a prompt", p)
			}
		}
		for k, files := range r.Split.Files {
			for _, x := range files {
				if x == p {
					t.Errorf("%s is %s in the split", p, k)
				}
			}
		}
		if l := r.Left[reason]; l.Count != 1 || len(l.Paths) != 1 || l.Paths[0] != p {
			t.Errorf("left %s: %+v, want %s", reason, l, p)
		}
		if res.Left[reason] != 1 {
			t.Errorf("result left %s: %d", reason, res.Left[reason])
		}
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Errorf("working tree changed: %s %v", out, err)
	}
}

// partsAnswer names every area by folder each time; each part's session
// claims only that part's files with it.
const partsAnswer = `{"areas":[
 {"name":"billing","promise":"People pay.","sources":["app/billing/","web/cart/"]},
 {"name":"sign-in","promise":"People sign in.","sources":["app/auth/","web/login/"]},
 {"name":"build","promise":"The app builds.","sources":["Makefile","cmd/"]}]}`

func TestRunInParts(t *testing.T) {
	dir := fixture(t)
	f := &fakeAgent{reply: func(s adapter.Spec, n int) string {
		switch kind(s) {
		case "split":
			return splitAnswer
		case "merge":
			return fenceJSON(`{"merge":[{"into":"sign-in","from":["build","nowhere"],"promise":"People sign in and build.","why":"one family"}]}`)
		}
		return fenceJSON(partsAnswer)
	}}
	st, s := setup(t, dir, f)
	s.PartChars = 60
	res, err := Run(context.Background(), st, s)
	if err != nil {
		t.Fatal(err)
	}
	parts := f.of("part")
	if res.Parts < 2 || len(parts) != res.Parts {
		t.Fatalf("parts %d, part sessions %d", res.Parts, len(parts))
	}
	if len(f.of("areas")) != 0 || res.FixRounds != 0 {
		t.Errorf("a single-session prompt or fix rounds ran: %d, %d", len(f.of("areas")), res.FixRounds)
	}
	for i, sp := range parts {
		if !strings.Contains(sp.System+sp.Prompt, AcrossParts) {
			t.Errorf("part %d lacks AcrossParts", i+1)
		}
		earlier := !strings.Contains(sp.Prompt, "### Areas so far\n\nnone")
		if earlier != (i > 0) || (i > 0 && !strings.Contains(sp.Prompt, "- `")) {
			t.Errorf("part %d: holds earlier briefs %v", i+1, earlier)
		}
	}
	merges := f.of("merge")
	if len(merges) != 1 || !merges[0].PromptOnly {
		t.Fatalf("merge sessions: %+v", merges)
	}
	r, _ := load(t, st)
	where := exactlyOnce(t, r)
	if where["Makefile"] != "sign-in" || where["cmd/main.go"] != "sign-in" || where["app/billing/pay.go"] != "billing" {
		t.Errorf("placed: %v", where)
	}
	if len(r.Merges) != 1 || strings.Join(r.Merges[0].From, " ") != "build" || r.Parts != res.Parts {
		t.Errorf("merges %+v parts %d", r.Merges, r.Parts)
	}
	if len(r.Areas) != 2 || res.MergeKept != "" {
		t.Errorf("areas %+v, kept %q", r.Areas, res.MergeKept)
	}
}

func TestRunInPartsKeepsAreasApartOnABadMerge(t *testing.T) {
	dir := fixture(t)
	f := &fakeAgent{reply: func(s adapter.Spec, n int) string {
		switch kind(s) {
		case "split":
			return splitAnswer
		case "merge":
			return "no json here"
		}
		return fenceJSON(partsAnswer)
	}}
	st, s := setup(t, dir, f)
	s.PartChars = 60
	res, err := Run(context.Background(), st, s)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := load(t, st)
	exactlyOnce(t, r)
	if len(r.Areas) != 3 || !strings.Contains(res.MergeKept, "kept apart") {
		t.Errorf("areas %d, kept %q", len(r.Areas), res.MergeKept)
	}
}

func TestRunWindowCutsParts(t *testing.T) {
	if got := partSize(0, 0, ""); got != DefaultPartChars {
		t.Errorf("default: %d", got)
	}
	if got := partSize(500, 0, ""); got != 500 {
		t.Errorf("chars: %d", got)
	}
	small := partSize(0, 40_000, "")
	if small >= DefaultPartChars || small < minPartChars {
		t.Errorf("a 40,000-token window gives %d", small)
	}
	if got := partSize(0, 1, ""); got != minPartChars {
		t.Errorf("a tiny window gives %d", got)
	}
}

func TestRunFixWithoutResumeStartsAFreshSession(t *testing.T) {
	dir := fixture(t)
	f := &fakeAgent{noResume: true, reply: func(s adapter.Spec, n int) string {
		switch kind(s) {
		case "split":
			return splitAnswer
		case "areas":
			return fenceJSON(firstAreas)
		}
		return fenceJSON(fixAnswer)
	}}
	st, s := setup(t, dir, f)
	if _, err := Run(context.Background(), st, s); err != nil {
		t.Fatal(err)
	}
	fresh := f.of("fresh-fix")
	if len(fresh) != 1 || fresh[0].SessionID == f.of("areas")[0].SessionID {
		t.Fatalf("fresh fix sessions: %+v", fresh)
	}
	if !strings.Contains(fresh[0].Prompt, "## Areas so far") || !strings.Contains(fresh[0].Prompt, "`billing`") {
		t.Errorf("fresh prompt lacks the briefs:\n%s", fresh[0].Prompt)
	}
	r, _ := load(t, st)
	exactlyOnce(t, r)
}

func TestRunNoSourceLeft(t *testing.T) {
	t.Run("only pattern matches everything", func(t *testing.T) {
		dir := fixture(t)
		f := &fakeAgent{reply: func(s adapter.Spec, n int) string {
			if kind(s) == "split" {
				return fenceJSON(`{"tests":["**"]}`)
			}
			return fenceJSON(`{"areas":[{"name":"all","promise":"Everything.","sources":["**"]}]}`)
		}}
		st, s := setup(t, dir, f)
		res, err := Run(context.Background(), st, s)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(res.Dropped, " ") != "**" || res.Kinds[sourceset.Test] != 0 {
			t.Errorf("dropped %v kinds %v", res.Dropped, res.Kinds)
		}
	})
	t.Run("nothing left", func(t *testing.T) {
		dir := fixture(t)
		f := &fakeAgent{reply: func(s adapter.Spec, n int) string {
			return fenceJSON(`{"tests":["**"],"docs":["*.md"],"other":["*.go","*.js","*.yml","Makefile",".gitattributes"]}`)
		}}
		st, s := setup(t, dir, f)
		_, err := Run(context.Background(), st, s)
		if err == nil || !strings.Contains(err.Error(), "no source file") || !strings.Contains(err.Error(), "**") {
			t.Fatalf("error: %v", err)
		}
		t.Log("error: " + err.Error())
		if len(f.specs) != 1 || kind(f.specs[0]) != "split" {
			t.Errorf("a grouping session started: %d sessions", len(f.specs))
		}
		if _, ok := load(t, st); ok {
			t.Error("areas stored")
		}
	})
}

func TestRunNeedsAGather(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	called := false
	_, err = Run(context.Background(), initrecord.Of(st), Session{Agent: func() (adapter.Adapter, error) { called = true; return nil, errors.New("x") }})
	if err == nil || !strings.Contains(err.Error(), "physa init --gather") || called {
		t.Errorf("error %v, agent resolved %v", err, called)
	}
}

// A cancel during the split session starts no grouping session, and nothing
// is stored.
func TestRunCancelStartsNoSessionAfter(t *testing.T) {
	dir := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &fakeAgent{cancel: cancel, reply: func(s adapter.Spec, n int) string { return splitAnswer }}
	st, s := setup(t, dir, f)
	_, err := Run(ctx, st, s)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want cancelled", err)
	}
	if f.late != 0 || len(f.specs) != 1 {
		t.Errorf("%d sessions started, %d after the cancel; want only the split", len(f.specs), f.late)
	}
	if _, ok := load(t, st); ok {
		t.Error("a cancelled run stored areas")
	}
}
