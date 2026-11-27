package initrun

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/consolidateprompt"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
	"github.com/dphaener/physa/internal/walk"
)

// prForge lists PR 3 as merged and holds at most one PR of its own,
// recording each it opens and each body it writes.
type prForge struct {
	mergedFake
	mu      sync.Mutex
	open    bool
	created []string // "head→base: title"
	bodies  []string
	edits   int
}

func (f *prForge) OpenPR(context.Context, string) (int, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return 7, "https://example.com/pr/7", f.open, nil
}

func (f *prForge) CreatePR(_ context.Context, head, base, title, body string) (int, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open = true
	f.created = append(f.created, head+"→"+base+": "+title)
	f.bodies = append(f.bodies, body)
	return 7, "https://example.com/pr/7", nil
}

func (f *prForge) EditPR(context.Context, int, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits++
	return nil
}

// The recovering replies, one per capability. delivery's holds a rule with
// no Because: line, which the check drops after its retries.
const (
	deliveryReply = "````markdown\n# Delivery\n\n## Rules\n\n" +
		"### Changes ship in order\n`ship-in-order` · #3\nChanges reach users in the order merged.\n" +
		"Because: a reviewer asked for it in #3.\n\n" +
		"### Changes ship fast\n`ship-fast` · #3\nChanges reach users within a day.\n````\n\n" +
		"```glossary\n**Delivery**: A change reaching users.\n```\n"
	searchReply = "````markdown\n# Search\n\n## Rules\n\n" +
		"### Kept things are found\n`find-kept` · #3\nPeople find what they kept by its words.\n" +
		"Because: #3 asked for it.\n````\n\n```glossary\n**Search**: Finding what was kept.\n```\n"
)

// discoveryMarker is a line of the discovery skill, which only a discovery
// session's prompt holds.
const discoveryMarker = "You find the sources a product's map is built from"

// The code-first replies of routedAgent on remoteRepo's two source files: the
// split of the files by kind, the areas with the product's name and promise,
// and each area's walk, which names the capabilities delivery and search.
const (
	fullProduct = `"product":{"name":"Keeper","promise":"Teams keep what they learn about their product."},`
	fullAreas   = `"areas":[{"name":"shipping","promise":"Changes reach users.","sources":["web/"]},` +
		`{"name":"finding","promise":"People find things.","sources":["main.go"]}]`
	fullAreasReply = "```json\n{" + fullProduct + fullAreas + "}\n```"
	// searchTwoRules is search's recovering with two rules, so its area has the
	// two entries a session of consolidation starts for.
	searchTwoRules = "````markdown\n# Search\n\n## Rules\n\n" +
		"### Kept things are found\n`find-kept` · #3\nPeople find what they kept by its words.\nBecause: #3 asked for it.\n\n" +
		"### Found things open\n`found-opens` · #3\nWhat is found opens where it was kept.\nBecause: #3 asked for it.\n````\n"
)

// fullWalks answers each area of fullAreas by its name.
var fullWalks = map[string]string{
	"shipping": "```json\n" + `{"capabilities":[{"name":"delivery","promise":"Changes reach users in order.","sources":["web/app.js"],"tests":[],"docs":[]}],"shared":[]}` + "\n```",
	"finding":  "```json\n" + `{"capabilities":[{"name":"search","promise":"People find what they kept.","sources":["main.go"],"tests":[],"docs":[]}],"shared":[]}` + "\n```",
}

// sessionKind names the session s starts: "discover", "split" and "areas"
// for the areas phase's two skills, "walk", "consolidate", or the capability a
// recovering session's message names ("" when it names none).
func sessionKind(s adapter.Spec) string {
	switch {
	case strings.Contains(s.Prompt, discoveryMarker):
		return "discover"
	case strings.HasPrefix(s.System, strings.TrimRight(areas.Sources, "\n")):
		return "split"
	case strings.HasPrefix(s.System, strings.TrimRight(areas.Areas, "\n")):
		return "areas"
	case strings.HasPrefix(s.System, strings.TrimRight(walk.Walk, "\n")):
		return "walk"
	case s.System == consolidateprompt.Consolidate, s.System == consolidateprompt.Merge, s.System == consolidateprompt.Lift:
		return "consolidate"
	}
	if m := recoverName.FindStringSubmatch(s.Prompt); m != nil {
		return m[1]
	}
	return ""
}

// consolidateNothing is a session of consolidation's answer that changes
// nothing, for the skill in system.
func consolidateNothing(system string) string {
	switch system {
	case consolidateprompt.Merge:
		return "```json\n{\"merge\":[]}\n```\n"
	case consolidateprompt.Lift:
		return "```json\n{\"lift\":[]}\n```\n"
	}
	return "```json\n{\"merge\":[],\"move\":[],\"drop\":[]}\n```\n"
}

// routedAgent answers every session of a code-first run: discovery with
// discover (naming nothing when empty), the areas phase with split and
// areas, each area's walk with walks, each
// capability's recovering with its reply and consolidation with nothing to
// change, a resumed session getting its first answer again. It records each
// session it started, by "discover", "areas" (the split too), "walk", the
// capability or "consolidate", and calls first, when set, before its first
// start.
type routedAgent struct {
	mu        sync.Mutex
	replies   map[string]string
	walks     map[string]string // area → the walk's reply
	areas     string            // the areas session's reply
	split     string            // the split's reply
	sessions  map[string]string // session id → what it was recorded as
	texts     map[string]string // session id → its answer
	started   []string          // "discover", "areas", "walk", the capability or "consolidate"
	first     func()
	spend     map[string]int64 // the input tokens a session of "split", "areas", "walk" or "consolidate" uses; zero means 10,000
	discover  string           // discovery's reply
	discovers adapter.Tokens   // the tokens discovery uses; zero means 1,000 in, 10 out
	recovers  int64            // the input tokens each recovering session uses; zero means 10,000
	readOnly  bool             // every discovery session was read-only
	priced    bool             // each result carries its cost, at $4 a million input
	writes1h  bool             // with priced, uncached input is written to a 1-hour cache, at 2× input
}

func newRoutedAgent() *routedAgent {
	return &routedAgent{replies: map[string]string{"delivery": deliveryReply, "search": searchReply}, walks: fullWalks, areas: fullAreasReply, split: areasSplit,
		sessions: map[string]string{}, texts: map[string]string{}, spend: map[string]int64{}}
}

func (a *routedAgent) Name() string { return "fake" }

func (a *routedAgent) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.started) == 0 && a.first != nil {
		a.first()
	}
	name, text, in := a.sessions[s.SessionID], a.texts[s.SessionID], int64(0)
	if !s.Resume {
		switch name = sessionKind(s); name {
		case "discover":
			text = cmp.Or(a.discover, "```json\n{\"sources\":[]}\n```\n")
			a.readOnly = (len(a.started) == 0 || a.readOnly) && s.ReadOnly && !s.PromptOnly
		case "split":
			name, text, in = "areas", a.split, cmp.Or(a.spend["split"], 10_000)
		case "areas":
			text = a.areas
		case "walk":
			if m := walkAreaOf.FindStringSubmatch(s.Prompt); m != nil {
				text = a.walks[m[1]]
			}
		case "consolidate":
			text = consolidateNothing(s.System)
		default:
			text = a.replies[name]
		}
		a.sessions[s.SessionID], a.texts[s.SessionID] = name, text
		a.started = append(a.started, name)
	}
	t := adapter.Tokens{Input: cmp.Or(in, a.spend[name], 10_000), CachedInput: 5, Output: 20}
	switch {
	case name == "discover":
		t = adapter.Tokens{Input: 1_000, Output: 10}
		if a.discovers != (adapter.Tokens{}) {
			t = a.discovers
		}
	case !slices.Contains([]string{"areas", "walk", "consolidate"}, name) && a.recovers != 0:
		t.Input = a.recovers
	}
	res := adapter.Result{Text: text, Tokens: t, SessionID: s.SessionID}
	if a.priced {
		res.Model = "claude-opus-5-5"
		c := adapter.ModelCost{Model: res.Model, Input: t.Input - t.CachedInput, CacheRead: t.CachedInput, Output: t.Output}
		c.MicroUSD = 4*c.Input + c.CacheRead/5 + 20*c.Output // $4, $0.20 and $20 a million
		if a.writes1h {
			c.CacheWrite1h, c.Input = c.Input, 0
			c.MicroUSD += 4 * c.CacheWrite1h // $8 a million: twice input
		}
		res.Cost = adapter.Cost{c}
	}
	return fakeSession{f: &fakeAgent{reports: []adapter.Tokens{t}, result: res}, usage: s.Usage}, nil
}

// kinds is the sessions a started, each once, in the order they first started.
func (a *routedAgent) kinds() []string {
	var out []string
	for _, k := range a.started {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// remoteRepo is the person's clone of a local bare remote, with AGENTS.md,
// the source files main.go and web/app.js, which PR 3 added, a few changes, an uncommitted edit and an untracked file, made the test's
// working directory. It returns the clone and the bare remote.
func remoteRepo(t *testing.T) (dir, bare string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[user]\n\tname = W\n\temail = w@example.com\n[commit]\n\tgpgsign = false\n[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, bare = t.TempDir(), t.TempDir()
	gitIn(t, bare, "init", "-q", "--bare")
	gitIn(t, dir, "init", "-q")
	write(t, dir, "AGENTS.md", "Our rules.\n")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "web/app.js", "let a\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: Start (#3)")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "feat: More")
	gitIn(t, dir, "remote", "add", "origin", bare)
	gitIn(t, dir, "push", "-q", "origin", "main")
	gitIn(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	write(t, dir, "main.go", "package main // edited\n")
	gitIn(t, dir, "add", "main.go")
	write(t, dir, "scratch.txt", "mine\n")
	t.Chdir(dir)
	return dir, bare
}

// fullEnv is an Env on the repo in the working directory with its own
// store, forge f and agent a, and the git writer and hooks.
func fullEnv(t *testing.T, f forge.Forge, a adapter.Adapter) Env {
	env := testEnv(t, f, a)
	env.Writer = func(_ context.Context, d string) (vcs.Writer, error) { return git.OpenWriter(context.Background(), d) }
	env.Hooks = func(_ context.Context, d string) (vcs.Hooks, error) { return git.OpenHooks(context.Background(), d) }
	env.Stdin = strings.NewReader("")
	env.IsTerminal = func() bool { return false }
	return env
}

// personState is the person's working copy, index and HEAD.
func personState(t *testing.T, dir string) string {
	return gitIn(t, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=all") + "|" +
		gitIn(t, dir, "rev-parse", "HEAD") + "|" + gitIn(t, dir, "diff", "--cached")
}

// changed lists the files ref changes from main in dir.
func changed(t *testing.T, dir, ref string) []string {
	return strings.Fields(gitIn(t, dir, "diff", "--name-only", "main", ref))
}

// checkTree checks out ref of the repo at dir in a fresh clone and runs the
// map check on it.
func checkTree(t *testing.T, dir, ref string) {
	t.Helper()
	clone := t.TempDir()
	gitIn(t, clone, "clone", "-q", "--branch", ref, dir, ".")
	res, err := mapcheck.CheckWith(t.Context(), clone, mapcheck.Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) { return git.Open(context.Background(), d) }})
	if err != nil || len(res.Problems) > 0 {
		t.Errorf("map check on %s: %v %v", ref, err, res.Problems)
	}
}

func TestFullWritePushPR(t *testing.T) {
	dir, bare := remoteRepo(t)
	before := personState(t, dir)
	f := &prForge{mergedFake: mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}}
	a := newRoutedAgent()
	code, out, errOut := runInit(fullEnv(t, f, a), "--write", "--push", "--pr")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	want := []string{".physa/README.md", ".physa/capabilities/delivery/index.md", ".physa/capabilities/delivery/rules.md",
		".physa/capabilities/search/index.md", ".physa/capabilities/search/rules.md", ".physa/glossary.md", ".physa/product/index.md", "AGENTS.md"}
	if got := changed(t, bare, "physa/init"); !slices.Equal(got, want) {
		t.Errorf("the remote's physa/init changes %v, want %v", got, want)
	}
	checkTree(t, bare, "physa/init")
	if after := personState(t, dir); after != before {
		t.Errorf("the person's working copy changed:\nbefore %s\nafter  %s", before, after)
	}
	if len(f.created) != 1 || f.created[0] != "physa/init→main: docs(map): Add the map recovered by physa init" || f.edits != 0 {
		t.Fatalf("PRs created %v, edits %d", f.created, f.edits)
	}
	for _, s := range []string{"`delivery`: Changes reach users in order.", "`search`: People find what they kept.",
		"delivery: rule \"Changes ship fast (ship-fast)\"", "| areas | 20000 | 10 | 40 | 20k | none: no price yet |", "| walk |", "| recover |", "| total |",
		"| Weighted | Cost estimate |",
		"## The Session: hook\n\nNot asked for."} {
		if !strings.Contains(f.bodies[0], s) {
			t.Errorf("PR body lacks %q:\n%s", s, f.bodies[0])
		}
	}
	for _, s := range []string{"Gathered into", "- shipping (1 file): Changes reach users.", "Product: Keeper. Its promise: Teams keep what they learn about their product.",
		"PRs placed: ", "## delivery: recovered", "Consolidated 2 capabilities", "Wrote physa/init at ", "Pushed: true", "PR: opened #7", "## Tokens spent"} {
		if !strings.Contains(out, s) {
			t.Errorf("stdout lacks %q:\n%s", s, out)
		}
	}
	msg := gitIn(t, bare, "log", "-1", "--format=%B", "physa/init")
	if n := strings.Count(msg, "Session: "); n != 6 || strings.Contains(msg, "Capability:") { // the areas' two, the walk's two and the recoveries' two
		t.Errorf("commit message, with %d Session: trailers:\n%s", n, msg)
	}
}

// noPricesLine is the full run's first line on a machine that knows no
// prices yet.
const noPricesLine = "physa init: no appetite and no circuit breaker: init runs to the end. Spend is shown in tokens; no prices are known yet, so they count raw and no cost is shown.\n"

func TestFullPlanLineComesFirst(t *testing.T) {
	remoteRepo(t)
	var stderr bytes.Buffer
	a := newRoutedAgent()
	var seen string
	a.first = func() { seen = stderr.String() }
	code := runInitTo(&bytes.Buffer{}, &stderr, fullEnv(t, onePR(), a))
	if code != 0 {
		t.Fatalf("code %d, stderr %s", code, stderr.String())
	}
	if !strings.HasPrefix(seen, noPricesLine) {
		t.Errorf("before the first session, stderr was %q", seen)
	}
	if len(a.started) == 0 || a.started[0] != "discover" || !a.readOnly {
		t.Errorf("sessions %v (discovery read-only: %v); want discovery first, read-only", a.started, a.readOnly)
	}
	after := strings.TrimPrefix(stderr.String(), seen)
	if !strings.Contains(after, "physa init: this run spent ") || regexp.MustCompile(`(?i)estimat`).MatchString(stderr.String()) {
		t.Errorf("after gathering, stderr lacks the spend line or names an estimate:\n%s", after)
	}
	if strings.Contains(stderr.String(), "$") || strings.Contains(stderr.String(), "appetite of") {
		t.Errorf("with no prices a cost was shown, or an appetite named:\n%s", stderr.String())
	}
	if out := gitIn(t, ".", "branch", "--list", "physa/init"); out != "" {
		t.Errorf("without --write a branch was made: %q", out)
	}
}

// readFails is a Stdin that fails the test when it is read.
type readFails struct{ t *testing.T }

func (r readFails) Read([]byte) (int, error) {
	r.t.Error("init read the person's input")
	return 0, io.EOF
}

// TestBareRunAsksNothingAboutCost checks that a bare run on a terminal prints
// no estimate and asks no question: it walks and recovers without being
// asked, and the next run's first line names what a token is worth.
func TestBareRunAsksNothingAboutCost(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.priced = true
	env := fullEnv(t, onePR(), a)
	env.IsTerminal = func() bool { return true }
	env.Stdin = readFails{t}
	code, out, errOut := runInit(env, "--write")
	if code != 0 || !strings.Contains(out, "## delivery: recovered") || !slices.Contains(a.started, "walk") {
		t.Fatalf("code %d, sessions %v\nstdout %s\nstderr %s", code, a.started, out, errOut)
	}
	if regexp.MustCompile(`(?i)estimat`).MatchString(errOut) || regexp.MustCompile(`(?m)\?$`).MatchString(errOut) {
		t.Errorf("stderr holds an estimate or a question:\n%s", errOut)
	}
	if !strings.HasPrefix(errOut, noPricesLine) {
		t.Errorf("stderr starts %q", errOut)
	}
	// The first run's prices are kept for the next one, whose first line
	// names what a token is worth.
	a2 := newRoutedAgent()
	env.Agent = func(string) (adapter.Adapter, error) { return a2, nil }
	_, _, errOut = runInit(env, "--write")
	if !strings.Contains(errOut, "physa init: no appetite and no circuit breaker: init runs to the end. Spend is shown in tokens weighted by price, each worth an input token of claude-opus-5-5 ($4.00 a million), with about what they cost beside them.\n") {
		t.Errorf("the second run's first line names no unit:\n%s", errOut)
	}
}

// TestFullTableAgreesWithTheRunAt1HourWrites checks that the PR body's
// table shows what the meter counted from the adapter's dollars: with one-turn
// sessions whose input is all 1-hour cache writes, at twice input, its
// weighted total is about twice the raw input and matches the end-of-run line.
func TestFullTableAgreesWithTheRunAt1HourWrites(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.priced, a.writes1h = true, true
	env := fullEnv(t, onePR(), a)
	code, out, errOut := runInit(env, "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	m := regexp.MustCompile(`physa init: this run spent ([0-9.]+[kM]?) tokens \((about \$[0-9.]+)\)\.`).FindStringSubmatch(errOut)
	if m == nil {
		t.Fatalf("no end-of-run line:\n%s", errOut)
	}
	if total := regexp.MustCompile(`\| total \| \d+ \| \d+ \| \d+ \| ([^ ]+) \| ([^|]+) \|`).FindStringSubmatch(out); total == nil || total[1] != m[1] || strings.TrimSpace(total[2]) != m[2] {
		t.Errorf("the table's total %q does not match the run's %q:\n%s", total, m[1:], out)
	}
	for _, phase := range []string{"| discover |", "| areas |", "| walk |", "| recover |"} {
		if !strings.Contains(out, phase) {
			t.Errorf("the table has no %s row:\n%s", phase, out)
		}
	}
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := initrecord.Of(st).Spending(context.Background())
	var w, in int64
	for _, p := range rows {
		w, in = w+p.Spend.Tokens, in+p.Raw.Input
	}
	if err != nil || in == 0 || float64(w)/float64(in) < 1.95 || float64(w)/float64(in) > 2.1 {
		t.Errorf("weighted %d for %d raw input (%v); want about twice", w, in, err)
	}
}

// TestFullWithPricesShowsCost checks the PR body's weighted tokens and cost
// estimate once the agent reports what its sessions cost.
func TestFullWithPricesShowsCost(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.priced = true
	code, out, errOut := runInit(fullEnv(t, onePR(), a), "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	// The areas' two sessions: each 9,995 uncached in, 5 cached, 20 out at $4/M input, $0.04.
	for _, want := range []string{"| areas | 20000 | 10 | 40 | 20k | about $0.08 |", "| total |", "physa init: this run spent "} {
		if !strings.Contains(out+errOut, want) {
			t.Errorf("output lacks %q:\n%s\n%s", want, out, errOut)
		}
	}
	if !regexp.MustCompile(`physa init: this run spent [0-9.]+k tokens \(about \$0\.[0-9]{2}\)\.`).MatchString(errOut) {
		t.Errorf("stderr:\n%s", errOut)
	}
}

func TestFullJSONIsOneDocument(t *testing.T) {
	remoteRepo(t)
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write", "--json")
	var doc struct {
		Gather struct {
			Version   int
			Discovery struct {
				Started bool
				Tokens  adapter.Tokens
			}
		}
		Areas struct {
			Product struct{ Name, Promise string }
		}
		Walk, Place, Recovery, Consolidation json.RawMessage
		Write                                struct {
			Branch string
			Commit struct{ Subject string }
			Pushed bool
			PR     struct{ State string }
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if d := doc.Gather.Discovery; doc.Gather.Version != 3 || !d.Started || d.Tokens.Input != 1_000 {
		t.Errorf("gather %+v", doc.Gather)
	}
	if p := doc.Areas.Product; p.Name != "Keeper" || p.Promise != "Teams keep what they learn about their product." {
		t.Errorf("areas.product %+v", p)
	}
	if len(doc.Walk) == 0 || len(doc.Place) == 0 || len(doc.Recovery) == 0 || len(doc.Consolidation) == 0 || doc.Write.Branch != "physa/init" ||
		doc.Write.Commit.Subject == "" || doc.Write.Pushed || doc.Write.PR.State != "not asked" {
		t.Errorf("document %s", out)
	}
}

// newCodebaseRepo is a repo of two changes and a README only, with a local
// bare remote, made the working directory.
func newCodebaseRepo(t *testing.T) string {
	t.Helper()
	dir, _ := remoteRepo(t)
	gitIn(t, dir, "rm", "-q", "--cached", "AGENTS.md")
	if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "README.md", "# Lathe\n")
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "docs: Readme")
	gitIn(t, dir, "push", "-q", "origin", "main")
	return dir
}

// onlyDiscovered fails the test unless a started discovery's session alone.
func onlyDiscovered(t *testing.T, a *routedAgent) {
	t.Helper()
	if !slices.Equal(a.started, []string{"discover"}) {
		t.Errorf("sessions %v, want discovery's alone", a.started)
	}
}

// noAgent fails the test when an agent is resolved.
func noAgent(t *testing.T) func(string) (adapter.Adapter, error) {
	return func(string) (adapter.Adapter, error) {
		t.Error("an agent was resolved")
		return nil, os.ErrInvalid
	}
}

func TestNewCodebaseAnswersWrite(t *testing.T) {
	dir := newCodebaseRepo(t)
	answers := filepath.Join(t.TempDir(), "answers.txt")
	write(t, filepath.Dir(answers), "answers.txt", "# mine\n1: Lathe\n2: Lathe turns sketches into cut lists.\n3: Cut list: The parts to cut, with their sizes\n")
	a := newRoutedAgent()
	env := fullEnv(t, nil, a)
	code, out, errOut := runInit(env, "--write", "--answers", answers)
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	want := []string{".physa/README.md", ".physa/glossary.md", ".physa/product/index.md", "AGENTS.md"}
	if got := changed(t, dir, "physa/init"); !slices.Equal(got, want) {
		t.Errorf("physa/init changes %v, want %v", got, want)
	}
	if p := gitIn(t, dir, "show", "physa/init:.physa/product/index.md"); strings.TrimSpace(p) != "---\npromise: Lathe turns sketches into cut lists.\n---\n\n# Lathe\n\nLathe turns sketches into cut lists." {
		t.Errorf("product/index.md:\n%s", p)
	}
	if g := gitIn(t, dir, "show", "physa/init:.physa/glossary.md"); !strings.Contains(g, "**Cut list**: The parts to cut, with their sizes") {
		t.Errorf("glossary.md:\n%s", g)
	}
	checkTree(t, dir, "physa/init")
	if !strings.Contains(out, "A new codebase") || !strings.Contains(out, "- .physa/product/index.md: Lathe") || !strings.Contains(out, "Wrote physa/init at ") {
		t.Errorf("stdout %s", out)
	}
	onlyDiscovered(t, a)
}

func TestNewCodebaseAsksOnATerminal(t *testing.T) {
	newCodebaseRepo(t)
	a := newRoutedAgent()
	env := fullEnv(t, nil, a)
	env.IsTerminal = func() bool { return true }
	env.Stdin = strings.NewReader("\nLathe turns sketches into cut lists.\n\n")
	code, out, errOut := runInit(env)
	if code != 0 || !strings.Contains(errOut, "1. What is the product called?") ||
		!strings.Contains(out, "Lathe turns sketches into cut lists.") || !strings.Contains(out, "Nothing is written without --write.") {
		t.Errorf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	onlyDiscovered(t, a)
}

func TestNewCodebaseWithoutAnswers(t *testing.T) {
	newCodebaseRepo(t)
	a := newRoutedAgent()
	env := fullEnv(t, nil, a)
	code, _, errOut := runInit(env, "--write")
	if code != 2 || !strings.Contains(errOut, "physa init: this looks like a new codebase (") ||
		!strings.Contains(errOut, "answer its questions on a terminal or with --answers FILE") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
	onlyDiscovered(t, a)
}

func TestFullHooksKeepsOtherHooks(t *testing.T) {
	dir, _ := remoteRepo(t)
	pre := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(pre, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write", "--hooks")
	if code != 0 || !strings.Contains(out, "Hook: installed: ") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if info, err := os.Stat(filepath.Join(dir, ".git", "hooks", "prepare-commit-msg")); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("prepare-commit-msg: %v %v", info, err)
	}
	if info, err := os.Stat(pre); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("pre-commit: %v %v", info, err)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "--get", "core.hooksPath").Output(); err == nil {
		t.Errorf("core.hooksPath was set: %q", out)
	}
	if !strings.Contains(out, "## The Session: hook\n\nInstalled at ") {
		t.Errorf("the body does not say the hook was installed:\n%s", out)
	}
}

func TestFullHooksRefusedKeepsBranch(t *testing.T) {
	dir, _ := remoteRepo(t)
	gitIn(t, dir, "config", "core.hooksPath", "elsewhere")
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write", "--hooks")
	if code != 2 || !strings.Contains(errOut, "physa init: the branch was written, but the hook was not installed: core.hooksPath is already") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if gitIn(t, dir, "branch", "--list", "physa/init") == "" {
		t.Error("the branch was not written")
	}
	if !strings.Contains(out, "Not installed: core.hooksPath is already") {
		t.Errorf("the body does not say the hook was refused:\n%s", out)
	}
}

func TestFullBadCombinationsAreBadUsage(t *testing.T) {
	newRepo(t)
	for _, args := range [][]string{{"--push"}, {"--write", "--gather"}, {"--propose"}, {"--answers", "a.txt", "--recover"}, {"--hooks"}, {"--yes", "--gather"}, {"--appetite", "300000"}} {
		if code, _, _ := runInit(testEnv(t, nil, nil), args...); code != command.BadUsage {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

// mapLayouts are a product of each layout, by its path: the single file of
// old and the folder's index.md.
var mapLayouts = map[string]string{
	".physa/product.md":       "---\npromise: P.\n---\n\n# P\n",
	".physa/product/index.md": "---\npromise: P.\n---\n\n# P\n\nP.\n",
}

// mappedRepo is remoteRepo with a map's product at p committed to its
// default branch.
func mappedRepo(t *testing.T, p string) {
	t.Helper()
	dir, _ := remoteRepo(t)
	write(t, dir, p, mapLayouts[p])
	gitIn(t, dir, "add", p)
	gitIn(t, dir, "commit", "-q", "-m", "docs(map): Add a map")
	gitIn(t, dir, "push", "-q", "origin", "main")
}

func TestMappedRepoIsRefusedBeforeAnythingIsSpent(t *testing.T) {
	for p := range mapLayouts {
		for _, args := range [][]string{nil, {"--walk"}, {"--recover"}} {
			mappedRepo(t, p)
			a := newRoutedAgent()
			env := fullEnv(t, onePR(), a)
			code, out, errOut := runInit(env, args...)
			t.Logf("%s %v: %s", p, args, strings.TrimSpace(errOut))
			if code != 2 || out != "" || !strings.Contains(errOut, "the repo already has a map ("+p+" on origin/main)") || !strings.Contains(errOut, "physa map learn") {
				t.Errorf("%s %v: code %d, stdout %q, stderr %q", p, args, code, out, errOut)
			}
			if len(a.started) != 0 {
				t.Errorf("%s %v: %d agent sessions ran", p, args, len(a.started))
			}
			st, err := env.Store(context.Background(), ".")
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := st.Gathered(context.Background()); ok {
				t.Errorf("%s %v: a gather was stored", p, args)
			}
			st.Close()
		}
	}
}

func TestMappedRepoStillGathers(t *testing.T) {
	mappedRepo(t, ".physa/product/index.md")
	env := fullEnv(t, onePR(), nil)
	env.Agent = noAgent(t)
	if code, out, errOut := runInit(env, "--gather"); code != 0 || !strings.Contains(out, "Gathered into") ||
		!strings.Contains(out, "Discovery: not started: the repo has a map") ||
		errOut != "physa init: gather starts no agent (discovery not started: the repo has a map)\n" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	code, out, _ := runInit(env, "--gather", "--json", "--agent", "x")
	var sum gather.Summary
	if code != 0 || json.Unmarshal([]byte(out), &sum) != nil {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if d := sum.Discovery; d.Started || d.Why != "discovery not started: the repo has a map" || d.Tokens != (adapter.Tokens{}) || sum.Docs.Count != 1 {
		t.Errorf("summary %+v", sum)
	}
	storedGather(t, env)
}

// trunkRepo is newRepo on a branch named trunk with no remote, so no default
// branch can be found.
func trunkRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	gitIn(t, dir, "branch", "-m", "trunk")
	return dir
}

func TestNoDefaultBranchSkipsTheEarlyCheck(t *testing.T) {
	trunkRepo(t)
	code, _, errOut := runInit(testEnv(t, nil, nil), "--areas")
	if code != 2 || strings.Contains(errOut, "already has a map") || !strings.Contains(errOut, "nothing gathered yet") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestNoDefaultBranchRefusesAMapInTheWorkingTree(t *testing.T) {
	for p, text := range mapLayouts {
		dir := trunkRepo(t)
		write(t, dir, p, text)
		a := newRoutedAgent()
		for _, args := range [][]string{nil, {"--walk"}, {"--recover"}} {
			code, out, errOut := runInit(testEnv(t, nil, a), args...)
			t.Logf("%s %v: %s", p, args, strings.TrimSpace(errOut))
			if code != 2 || out != "" || !strings.Contains(errOut, "("+p+" in the working tree)") || !strings.Contains(errOut, "physa map learn") {
				t.Errorf("%s %v: code %d, stdout %q, stderr %q", p, args, code, out, errOut)
			}
		}
		if len(a.started) != 0 {
			t.Errorf("%s: %d agent sessions ran", p, len(a.started))
		}
	}
}

func TestGatherChoosesTheDiscoveryAgent(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "docs/notes/why.md", "# Why\n")
	a := newRoutedAgent()
	a.discover = "```json\n" + `{"sources":[{"path":"docs/notes/why.md","kind":"overview","reason":"why the product exists"}]}` + "\n```\n"
	a.discovers = adapter.Tokens{Input: 500, Output: 10}
	env := testEnv(t, onePR(), a)
	var names []string
	env.Agent = func(name string) (adapter.Adapter, error) { names = append(names, name); return a, nil }
	code, out, errOut := runInit(env, "--gather", "--agent", "x", "--json")
	var sum gather.Summary
	if code != 0 || json.Unmarshal([]byte(out), &sum) != nil {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if !strings.HasPrefix(errOut, "physa init: gather, with the discovery agent; init has no appetite and no circuit breaker\n") || !slices.Equal(names, []string{"x"}) ||
		!slices.Equal(a.started, []string{"discover"}) || !a.readOnly {
		t.Errorf("stderr %q, agents %v, sessions %v", errOut, names, a.started)
	}
	if d := sum.Discovery; !d.Started || d.Added != 1 || d.Tokens.Input != 500 {
		t.Errorf("discovery %+v", d)
	}
	if g := storedGather(t, env); !slices.ContainsFunc(g.Docs, func(d store.Doc) bool { return d.Path == "docs/notes/why.md" && d.Reason == "why the product exists" }) {
		t.Errorf("stored docs %+v", g.Docs)
	}
}

// skillLine is a line Env.Skill gives for an agent with no current Physa skill.
const skillLine = "Claude Code has no Physa skill on this account: run `physa skill setup` to install it, so its sessions learn Physa's commands and terms."

// skillSpy is an Env.Skill that gives line and counts the agents it was asked about.
type skillSpy struct {
	line   string
	agents []string
}

func (s *skillSpy) skill(agent string) string {
	s.agents = append(s.agents, agent)
	return s.line
}

// lastLine is the last line of out, without its newline.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestFullSkillLineIsSaidOnceLastAndInThePRBody(t *testing.T) {
	remoteRepo(t)
	f := &prForge{mergedFake: mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}}
	env := fullEnv(t, f, newRoutedAgent())
	spy := &skillSpy{line: skillLine}
	env.Skill = spy.skill
	code, out, errOut := runInit(env, "--write", "--push", "--pr", "--agent", "fake")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if !slices.Equal(spy.agents, []string{"fake"}) {
		t.Errorf("Skill asked about %v, want [fake] once", spy.agents)
	}
	if n := strings.Count(errOut, "physa skill setup"); n != 1 {
		t.Errorf("stderr names physa skill setup %d times, want 1:\n%s", n, errOut)
	}
	if got := lastLine(errOut); got != "physa init: "+skillLine {
		t.Errorf("stderr's last line %q, want the skill's line", got)
	}
	section := "\n## Physa's skill\n\n" + skillLine + "\n"
	if len(f.bodies) != 1 || !strings.HasSuffix(f.bodies[0], section) {
		t.Errorf("the PR body does not end with the skill's section:\n%v", f.bodies)
	}
	if !strings.Contains(out, section) {
		t.Errorf("the body on stdout lacks the skill's section:\n%s", out)
	}
}

func TestFullSkillLineIsInTheJSONBody(t *testing.T) {
	remoteRepo(t)
	env := fullEnv(t, onePR(), newRoutedAgent())
	env.Skill = (&skillSpy{line: skillLine}).skill
	code, out, errOut := runInit(env, "--write", "--json")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	var doc struct {
		Write struct{ Body string } `json:"write"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, out)
	}
	if !strings.HasSuffix(doc.Write.Body, "\n## Physa's skill\n\n"+skillLine+"\n") {
		t.Errorf("--json's body lacks the skill's section:\n%s", doc.Write.Body)
	}
	if got := lastLine(errOut); got != "physa init: "+skillLine {
		t.Errorf("stderr's last line %q, want the skill's line", got)
	}
}

func TestFullSkillCurrentOrNilSaysNothing(t *testing.T) {
	for name, skill := range map[string]func(string) string{"empty": (&skillSpy{}).skill, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			remoteRepo(t)
			f := &prForge{mergedFake: mergedFake{prs: []forge.PR{{Number: 3, Title: "A change", Body: "why", MergedAt: time.Now()}}}}
			env := fullEnv(t, f, newRoutedAgent())
			env.Skill = skill
			code, out, errOut := runInit(env, "--write", "--push", "--pr")
			if code != 0 || len(f.bodies) != 1 {
				t.Fatalf("code %d, bodies %d\nstdout %s\nstderr %s", code, len(f.bodies), out, errOut)
			}
			for where, s := range map[string]string{"stdout": out, "stderr": errOut, "PR body": f.bodies[0]} {
				if strings.Contains(s, "Physa's skill") || strings.Contains(s, "physa skill") {
					t.Errorf("%s names the skill:\n%s", where, s)
				}
			}
		})
	}
}

func TestNewCodebaseSkillLineIsInTheBody(t *testing.T) {
	newCodebaseRepo(t)
	answers := filepath.Join(t.TempDir(), "answers.txt")
	write(t, filepath.Dir(answers), "answers.txt", "1: Lathe\n2: Lathe turns sketches into cut lists.\n3: Cut list: The parts to cut\n")
	env := fullEnv(t, nil, newRoutedAgent())
	spy := &skillSpy{line: skillLine}
	env.Skill = spy.skill
	code, out, errOut := runInit(env, "--write", "--answers", answers)
	if code != 0 || len(spy.agents) != 1 {
		t.Fatalf("code %d, asked %v\nstdout %s\nstderr %s", code, spy.agents, out, errOut)
	}
	if !strings.HasSuffix(out, "\n## Physa's skill\n\n"+skillLine+"\n") {
		t.Errorf("the body on stdout does not end with the skill's section:\n%s", out)
	}
	if strings.Count(errOut, "physa skill setup") != 1 || lastLine(errOut) != "physa init: "+skillLine {
		t.Errorf("stderr:\n%s", errOut)
	}
}

func TestFullSkillLineKeepsTheExitCode(t *testing.T) {
	cases := map[string]struct {
		repo  func(t *testing.T)
		forge forge.Forge
		agent func() adapter.Adapter
		args  []string
	}{
		"stopped by the cap":        {func(t *testing.T) { remoteRepo(t) }, onePR(), func() adapter.Adapter { return newRoutedAgent() }, []string{"--write", "--cap", "1k"}},
		"a new codebase unanswered": {func(t *testing.T) { newCodebaseRepo(t) }, nil, func() adapter.Adapter { return newRoutedAgent() }, []string{"--write"}},
		"a hook refused": {func(t *testing.T) {
			dir, _ := remoteRepo(t)
			gitIn(t, dir, "config", "core.hooksPath", "elsewhere")
		}, onePR(), func() adapter.Adapter { return newRoutedAgent() }, []string{"--write", "--hooks"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			codes := map[string]int{}
			for _, line := range []string{"", skillLine} {
				c.repo(t)
				env := fullEnv(t, c.forge, c.agent())
				env.Skill = (&skillSpy{line: line}).skill
				code, _, errOut := runInit(env, c.args...)
				codes[line] = code
				if said := strings.Contains(errOut, "physa skill setup"); said != (line != "") {
					t.Errorf("line %q: stderr names the skill %v:\n%s", line, said, errOut)
				}
			}
			if codes[""] != codes[skillLine] || codes[""] == 0 {
				t.Errorf("exit codes %v, want the same non-zero code with and without the line", codes)
			}
		})
	}
}

func TestSkillIsNotAskedOutsideAFullRun(t *testing.T) {
	never := func(string) string { t.Error("Skill was asked"); return skillLine }
	newRepo(t)
	env := testEnv(t, onePR(), nil)
	env.Skill = never
	if code, _, errOut := runInit(env, "--gather"); code != 0 || strings.Contains(errOut, "physa skill") {
		t.Errorf("--gather: code %d, stderr %s", code, errOut)
	}
	if code, _, errOut := runInit(env, "--push"); code != command.BadUsage || strings.Contains(errOut, "physa skill") {
		t.Errorf("a bad line: code %d, stderr %s", code, errOut)
	}
	for _, args := range [][]string{nil, {"--walk"}, {"--recover"}} {
		mappedRepo(t, ".physa/product/index.md")
		env := fullEnv(t, onePR(), newRoutedAgent())
		env.Skill = never
		if code, _, errOut := runInit(env, args...); code != 2 || strings.Contains(errOut, "physa skill") {
			t.Errorf("a mapped repo %v: code %d, stderr %s", args, code, errOut)
		}
	}
}

// tickingNow gives env a clock that moves a second each time it is read, so
// what a run stores is in the order it stored it.
func tickingNow(env *Env) {
	var mu sync.Mutex
	now := time.Now().UTC().Truncate(time.Second)
	env.Now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Second)
		return now
	}
}

// inOrder fails the test unless out holds each of marks, each after the one
// before it.
func inOrder(t *testing.T, out string, marks ...string) {
	t.Helper()
	at := 0
	for _, m := range marks {
		i := strings.Index(out[at:], m)
		if i < 0 {
			t.Fatalf("the output lacks %q after %q:\n%s", m, out[:at], out)
		}
		at += i + len(m)
	}
}

// TestFullRunsThePhasesInOrder checks that a bare run starts its sessions in
// the order discovery, areas, walk, recover, consolidate, prints each phase as
// the phase alone does in that order with the placements between the walk and
// the recovery, and stores them in that order, each spending under its phase.
func TestFullRunsThePhasesInOrder(t *testing.T) {
	remoteRepo(t)
	a := newRoutedAgent()
	a.replies["search"] = searchTwoRules // two entries in an area, so consolidation starts a session
	env := fullEnv(t, onePR(), a)
	tickingNow(&env)
	code, out, errOut := runInit(env)
	t.Logf("sessions %v", a.started)
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	kinds := a.kinds()
	if len(kinds) != 6 || !slices.Equal(kinds[:3], []string{"discover", "areas", "walk"}) || kinds[5] != "consolidate" ||
		!slices.Equal(slices.Sorted(slices.Values(kinds[3:5])), []string{"delivery", "search"}) {
		t.Errorf("sessions %v; want discovery, areas, walk, the two recoveries, then consolidation", a.started)
	}
	inOrder(t, out, "Gathered into", "- shipping (1 file): Changes reach users.", "delivery", "PRs placed: ", "## delivery: recovered", "Consolidated 2 capabilities", reportHead)
	st, err := env.Store(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rec := initrecord.Of(st)
	ar, okA, errA := rec.Areas(t.Context())
	wk, okW, errW := rec.Walk(t.Context())
	pl, okP, errP := rec.Place(t.Context())
	rows, errR := rec.Recovered(t.Context())
	co, okC, errC := rec.Consolidated(t.Context())
	if err := errors.Join(errA, errW, errP, errR, errC); err != nil || !okA || !okW || !okP || !okC || len(rows) != 2 {
		t.Fatalf("stored: areas %v, walk %v, place %v, %d recovered, consolidation %v: %v", okA, okW, okP, len(rows), okC, err)
	}
	// the placements are of this walk, and recovery reads them, so they were stored between the two
	if !ar.At.Before(wk.At) || !pl.WalkAt.Equal(wk.At) {
		t.Errorf("areas at %v, walk at %v, placements of the walk at %v; want that order", ar.At, wk.At, pl.WalkAt)
	}
	for _, r := range rows {
		if !r.ProposalAt.Equal(wk.At) || !wk.At.Before(r.At) || !r.At.Before(co.At) {
			t.Errorf("%s recovered at %v from the walk at %v; want after the walk (%v) and before the consolidation (%v)", r.Capability, r.At, r.ProposalAt, wk.At, co.At)
		}
	}
	if !strings.Contains(errOut, "from the walk and placements") {
		t.Errorf("recovery did not read the placements:\n%s", errOut)
	}
	spent, err := rec.Spending(t.Context())
	var phases []string
	for _, p := range spent {
		phases = append(phases, p.Phase)
	}
	if want := []string{"discover", "areas", "walk", "recover", "consolidate"}; err != nil || !slices.Equal(phases, want) {
		t.Errorf("spend stored under %v (%v), want %v", phases, err, want)
	}
}

// productIndex is the map's product as physa/init holds it in dir.
func productIndex(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, dir, "show", "physa/init:.physa/product/index.md"))
}

// TestFullWriteWritesTheStatedProduct checks that a full run with --write
// writes the name and promise the areas session stated to the map's product,
// and says nothing of a promise not stated.
func TestFullWriteWritesTheStatedProduct(t *testing.T) {
	dir, _ := remoteRepo(t)
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	want := "---\npromise: Teams keep what they learn about their product.\n---\n\n# Keeper\n\nTeams keep what they learn about their product."
	if p := productIndex(t, dir); p != want {
		t.Errorf("product/index.md:\n%s\nwant:\n%s", p, want)
	}
	if strings.Contains(errOut, "Not yet stated.") || strings.Contains(out, "Not yet stated.") {
		t.Errorf("a stated promise is said missing:\n%s\n%s", out, errOut)
	}
}

// TestFullWriteWithoutAStatedPromiseSaysNotYetStated checks that when the
// areas session states no product, the full run still writes, the map's
// product is named after the folder and says "Not yet stated.", and init says
// so without advising --areas alone.
func TestFullWriteWithoutAStatedPromiseSaysNotYetStated(t *testing.T) {
	dir, _ := remoteRepo(t)
	a := newRoutedAgent()
	a.areas = "```json\n{" + fullAreas + "}\n```"
	code, out, errOut := runInit(fullEnv(t, onePR(), a), "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	p := productIndex(t, dir)
	if !strings.HasPrefix(p, "---\npromise: Not yet stated.\n---\n\n# ") || !strings.HasSuffix(p, "\n\nNot yet stated.") || strings.Contains(p, "Keeper") {
		t.Errorf("product/index.md:\n%s", p)
	}
	line := "physa init: the stored areas state no product promise, so the map's product says \"Not yet stated.\"; a new physa init states one\n"
	if strings.Count(errOut, line) != 1 || strings.Contains(errOut, "--areas") {
		t.Errorf("stderr does not say the promise is not stated once, or advises --areas:\n%s", errOut)
	}
	if !strings.Contains(out, `no product promise was stated with the areas, so the map's product will say "Not yet stated."`) {
		t.Errorf("the areas' output does not say so:\n%s", out)
	}
}

// TestFullWithNoSourceFileStopsAtAreas checks that a full run on a repo with
// history and docs but no source file to divide stops at areas with areas'
// own message and exit 2, having started no walk and written nothing.
func TestFullWithNoSourceFileStopsAtAreas(t *testing.T) {
	dir, _ := remoteRepo(t)
	gitIn(t, dir, "rm", "-q", "-f", "main.go", "web/app.js")
	write(t, dir, "notes.txt", "notes\n")
	gitIn(t, dir, "add", "notes.txt")
	gitIn(t, dir, "commit", "-q", "-m", "chore: Drop the code")
	a := newRoutedAgent()
	a.split = "```json\n" + `{"tests":[],"docs":["AGENTS.md"],"other":["*.txt"]}` + "\n```"
	code, out, errOut := runInit(fullEnv(t, onePR(), a), "--write")
	t.Logf("stderr:\n%s", errOut)
	if code != 2 || !strings.Contains(errOut, "physa init: the split leaves no source file to divide into areas") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	if got := a.kinds(); !slices.Equal(got, []string{"discover", "areas"}) {
		t.Errorf("sessions %v; want discovery and the areas' split alone", a.started)
	}
	if strings.Contains(errOut, "To continue") || gitIn(t, dir, "branch", "--list", "physa/init") != "" {
		t.Errorf("a failed areas phase said how to continue or wrote a branch:\n%s", errOut)
	}
}

// TestFullRecoveryWithAFailedCapabilityStillConsolidatesAndWrites checks that
// a full run whose recovery left one capability failed, and was not stopped,
// goes on to consolidate and write, and exits 2 for the failure.
func TestFullRecoveryWithAFailedCapabilityStillConsolidatesAndWrites(t *testing.T) {
	dir, _ := remoteRepo(t)
	a := newRoutedAgent()
	a.replies["search"] = "no block here"
	env := fullEnv(t, onePR(), a)
	code, out, errOut := runInit(env, "--write")
	if code != 2 {
		t.Fatalf("code %d, want 2\nstdout %s\nstderr %s", code, out, errOut)
	}
	inOrder(t, out, "## search: failed", "Consolidated 1 capabilities", "Wrote physa/init at ")
	files := gitIn(t, dir, "ls-tree", "-r", "--name-only", "physa/init")
	if !strings.Contains(files, ".physa/capabilities/delivery/rules.md") || !strings.Contains(files, ".physa/capabilities/search/index.md") {
		t.Errorf("physa/init holds:\n%s", files)
	}
	if !strings.Contains(errOut, "physa init: this run spent ") || strings.Contains(errOut, "To continue") {
		t.Errorf("stderr:\n%s", errOut)
	}
}

// TestFullWritesTheRecoveredMapWhenConsolidationCannotRun checks that when
// consolidation cannot run, here because no capability was recovered, init
// says so, still writes the map as recovered with --write, and exits 2.
func TestFullWritesTheRecoveredMapWhenConsolidationCannotRun(t *testing.T) {
	dir, _ := remoteRepo(t)
	a := newRoutedAgent()
	a.replies["delivery"], a.replies["search"] = "no block here", "no block here"
	code, out, errOut := runInit(fullEnv(t, onePR(), a), "--write")
	if code != 2 || strings.Contains(out, "Consolidated ") || !strings.Contains(out, "Wrote physa/init at ") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	inOrder(t, errOut, "physa init: no capability recovered from the walk yet", "physa init: consolidation could not run, so the map stays as recovered\n", "physa init: this run spent ")
	if files := gitIn(t, dir, "ls-tree", "-r", "--name-only", "physa/init"); !strings.Contains(files, ".physa/capabilities/delivery/index.md") {
		t.Errorf("physa/init holds:\n%s", files)
	}
}

// TestProposeIsAnUnknownFlag checks that --propose, removed with init's
// propose, is refused as any unknown flag is: bad usage, with nothing run.
func TestProposeIsAnUnknownFlag(t *testing.T) {
	newRepo(t)
	for _, args := range [][]string{{"--propose"}, {"--propose", "--json"}, {"--gather", "--propose"}, {"--write", "--propose"}} {
		f := oneAgent()
		env := testEnv(t, onePR(), f)
		code, out, errOut := runInit(env, args...)
		bogus, _, bogusErr := runInit(env, "--bogus")
		if code != command.BadUsage || out != "" || f.starts != 0 || code != bogus || errOut != bogusErr || !strings.HasPrefix(errOut, "usage: physa init") || strings.Contains(errOut, "--propose") {
			t.Errorf("%v: code %d, %d sessions, stdout %q, stderr %q", args, code, f.starts, out, errOut)
		}
	}
}
