package initrun

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/consolidate"
	"github.com/dphaener/physa/internal/consolidateprompt"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

var consolidateAt = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)

// consolidateAgent answers consolidation's sessions by their skill: a merge
// session with merge, a lift session by lifting every group it is given,
// and an area session with nothing to do. It counts every session started.
type consolidateAgent struct {
	mu     sync.Mutex
	starts int
	merge  string
	lift   bool
}

func (a *consolidateAgent) Name() string { return "fake" }

var recoverName = regexp.MustCompile(`\nname: (\S+)\n`)

var liftEntry = regexp.MustCompile("Entry `([^`]+)`")

func (a *consolidateAgent) Start(_ context.Context, s adapter.Spec) (adapter.Session, error) {
	a.mu.Lock()
	a.starts++
	a.mu.Unlock()
	text := `{"merge":[],"move":[],"drop":[]}`
	switch s.System {
	case consolidateprompt.Consolidate: // the default
	case consolidateprompt.Merge:
		text = `{"merge":[` + a.merge + `]}`
	case consolidateprompt.Lift:
		var items []string
		for _, g := range strings.Split(s.Prompt, "### Group ")[1:] {
			ids := liftEntry.FindAllStringSubmatch(g, -1)
			var with []string
			for _, id := range ids[1:] {
				with = append(with, fmt.Sprintf("%q", id[1]))
			}
			if a.lift {
				items = append(items, fmt.Sprintf(`{"keep":%q,"with":[%s],"why":"one rule"}`, ids[0][1], strings.Join(with, ",")))
			}
		}
		text = `{"lift":[` + strings.Join(items, ",") + `]}`
	default: // a capability's recovering, which writes it again with a new rule
		name := "x"
		if m := recoverName.FindStringSubmatch(s.Prompt); m != nil {
			name = m[1]
		}
		text = "````markdown\n# " + name + "\n\n## Rules\n\n" + consEntry("Again "+name, name+"-again", "#5", "It is recovered again.") + "````\n"
		return fakeSession{f: &fakeAgent{result: adapter.Result{Text: text, Model: "fake-model", Tokens: adapter.Tokens{Input: 10, Output: 2}, SessionID: s.SessionID}}, usage: s.Usage}, nil
	}
	return fakeSession{f: &fakeAgent{result: adapter.Result{Text: "```json\n" + text + "\n```\n", Model: "fake-model", Tokens: adapter.Tokens{Input: 10, Output: 2}, SessionID: s.SessionID}}, usage: s.Usage}, nil
}

// consCap is one recovered capability of a fixture: its area and rules.
type consCap struct {
	name, area, promise string
	rules               []string
}

func consEntry(heading, slug, anchor, body string) string {
	return "### " + heading + "\n`" + slug + "` · " + anchor + "\n" + body + "\nBecause: it was asked for.\n"
}

// consEnv is a repo whose store holds a walk of caps, its placements and
// each capability recovered from it, with the agent given.
func consEnv(t *testing.T, a adapter.Adapter, caps []consCap) Env {
	t.Helper()
	dir := newRepo(t)
	env := testEnv(t, dir, nil, a)
	s, err := env.Store(context.Background(), env.Proc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st := initrecord.Of(s)
	if err := s.PutGather(context.Background(), store.Gather{Run: store.GatherRun{At: consolidateAt}}); err != nil {
		t.Fatal(err)
	}
	var wr walk.Record
	var rows []initrecord.Recovered
	for _, c := range caps {
		if c.name == "sign-in-2" {
			wr.Renamed = []walk.Rename{{Area: "web", From: "sign-in", To: "sign-in-2"}}
		}
		wr.Capabilities = append(wr.Capabilities, walk.CapabilityRecord{Capability: walk.Capability{Name: c.name, Promise: c.promise, Sources: []string{"app/" + c.name + ".go"}}, Area: c.area})
		body := "# " + c.name + "\n\n## Rules\n\n" + strings.Join(c.rules, "\n") + "\n"
		rows = append(rows, initrecord.Recovered{Capability: c.name, ProposalAt: consolidateAt, At: consolidateAt.Add(time.Hour), Status: recovery.StatusRecovered,
			File: recovering.File(c.name, c.promise, body), SessionID: "rec-" + c.name, Details: []byte(`{}`)})
	}
	body, _ := json.Marshal(wr)
	for _, err := range []error{st.PutWalk(context.Background(), initrecord.Walk{AreasAt: consolidateAt.Add(-time.Hour), At: consolidateAt, Model: "m", Body: body}),
		st.PutPlace(context.Background(), initrecord.Place{WalkAt: consolidateAt, At: consolidateAt, Body: []byte(`{}`)}), st.PutRecovered(context.Background(), rows)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return env
}

var mergeCaps = []consCap{
	{name: "sign-in", area: "accounts", promise: "People sign in.",
		rules: []string{consEntry("Sessions last a day", "session-length", "#3", "A session ends a day after sign-in.")}},
	{name: "sign-in-2", area: "web", promise: "People sign in from the browser.",
		rules: []string{consEntry("Browser sessions end on close", "session-length", "#7", "Closing the browser signs out.")}},
	{name: "billing", area: "billing", promise: "People pay.",
		rules: []string{consEntry("Invoices go out monthly", "invoice-monthly", "#11", "Once a month.")}},
}

const mergeSignIn = `{"capabilities":["sign-in","sign-in-2"],"name":"sign-in","promise_of":"sign-in-2","why":"one promise"}`

func TestConsolidatePrintsAndStores(t *testing.T) {
	t.Parallel()
	a := &consolidateAgent{merge: mergeSignIn}
	env := consEnv(t, a, mergeCaps)
	code, out, errOut := runInit(env, "--consolidate")
	if code != 0 || !strings.Contains(out, "Merged capabilities sign-in, sign-in-2 into sign-in") || !strings.Contains(out, "Model: fake-model") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	st, err := env.Store(context.Background(), env.Proc.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if cur, err := consolidate.Current(t.Context(), initrecord.Of(st)); !cur || err != nil {
		t.Errorf("consolidation stored and current: %v %v", cur, err)
	}
	if spent, err := initrecord.Of(st).Spending(context.Background()); err != nil || len(spent) == 0 || spent[len(spent)-1].Phase != "consolidate" {
		t.Errorf("spending %v %v", spent, err)
	}
	code, out, errOut = runInit(env, "--consolidate", "--json")
	var res consolidate.Result
	if err := json.Unmarshal([]byte(out), &res); code != 0 || err != nil || len(res.Capabilities) != 1 || res.Capabilities[0].Name != "sign-in" {
		t.Errorf("code %d, err %v\nstdout %s\nstderr %s", code, err, out, errOut)
	}
}

func TestConsolidateWithoutAWalkNamesThePhases(t *testing.T) {
	t.Parallel()
	a := &consolidateAgent{}
	env := testEnv(t, newRepo(t), nil, a)
	code, out, errOut := runInit(env, "--consolidate")
	if code != 2 || out != "" || a.starts != 0 || !strings.Contains(errOut, "run physa init --walk, --place and --recover first") {
		t.Errorf("code %d, %d sessions\nstdout %s\nstderr %s", code, a.starts, out, errOut)
	}
}

func TestConsolidateWriteWritesTheMergedCapability(t *testing.T) {
	t.Parallel()
	env := consEnv(t, &consolidateAgent{merge: mergeSignIn}, mergeCaps)
	code, _, errOut := runInit(env, "--consolidate", "--write")
	if code != 0 {
		t.Fatalf("code %d\n%s", code, errOut)
	}
	files := gitIn(t, env.Proc.Dir, "ls-tree", "-r", "--name-only", "physa/init")
	if !strings.Contains(files, ".physa/capabilities/sign-in/") || strings.Contains(files, "sign-in-2") || !strings.Contains(files, ".physa/capabilities/billing/") {
		t.Errorf("physa/init holds:\n%s", files)
	}
	rules := gitIn(t, env.Proc.Dir, "show", "physa/init:.physa/capabilities/sign-in/rules.md")
	for _, want := range []string{"Sessions last a day", "Browser sessions end on close", "session-length-2"} {
		if !strings.Contains(rules, want) {
			t.Errorf("sign-in rules lack %q:\n%s", want, rules)
		}
	}
	if log := gitIn(t, env.Proc.Dir, "log", "-1", "--format=%B", "physa/init"); !strings.Contains(log, "Session: rec-sign-in-2") {
		t.Errorf("the merged-away capability's session is not kept:\n%s", log)
	}
}

// liftCaps are two areas whose rules repeat across them, n each.
func liftCaps(n int) []consCap {
	var caps []consCap
	for ai, area := range []string{"web", "mail"} {
		c := consCap{name: area + "-app", area: area, promise: "People use " + area + "."}
		for k := range n {
			w := fmt.Sprintf("alpha%02d beta%02d", k, k)
			c.rules = append(c.rules, consEntry("Rule "+w, fmt.Sprintf("%c-rule-%02d", area[0], k), fmt.Sprintf("#%d", 100*ai+k+1), "Body "+w+"."))
		}
		caps = append(caps, c)
	}
	return caps
}

// TestConsolidateLiftToTheRulesLimitPassesTheMapCheck: 24 lifted rules make
// the product's rules exactly 150 lines, the most the map check accepts, and
// the real write accepts them.
func TestConsolidateLiftToTheRulesLimitPassesTheMapCheck(t *testing.T) {
	t.Parallel()
	env := consEnv(t, &consolidateAgent{lift: true}, liftCaps(24))
	code, out, errOut := runInit(env, "--consolidate", "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	rules := gitIn(t, env.Proc.Dir, "show", "physa/init:.physa/product/rules.md")
	if n := capabilitytext.Lines(rules); n != mapcheck.MaxRulesLines {
		t.Errorf("product rules are %d lines, want %d", n, mapcheck.MaxRulesLines)
	}
	if n := strings.Count(rules, "\n### "); n != 24 {
		t.Errorf("%d product rules, want 24", n)
	}
}

func TestConsolidateIsBadUsageWithOtherPhases(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--walk"}, {"--recover"}, {"--only", "x"}, {"--since", "2026-01-01"}} {
		if code, _, _ := runInit(testEnv(t, t.TempDir(), nil, nil), append([]string{"--consolidate"}, args...)...); code != command.BadUsage {
			t.Errorf("--consolidate %v: code %d, want bad usage", args, code)
		}
	}
}

// TestRecoverWriteIgnoresAStaleConsolidation: after a new recover changes the
// recovered text, the stored consolidation no longer matches, so a write says
// so and writes the recovered capabilities as they are.
func TestRecoverWriteIgnoresAStaleConsolidation(t *testing.T) {
	t.Parallel()
	env := consEnv(t, &consolidateAgent{merge: mergeSignIn}, mergeCaps)
	if code, _, errOut := runInit(env, "--consolidate"); code != 0 {
		t.Fatalf("consolidate: %d %s", code, errOut)
	}
	// --only recovers every capability again, since a plain --recover keeps what is already recovered
	code, _, errOut := runInit(env, "--recover", "--only", "sign-in,sign-in-2,billing", "--write")
	if code != 0 || !strings.Contains(errOut, "run physa init --consolidate again") {
		t.Fatalf("code %d\n%s", code, errOut)
	}
	files := gitIn(t, env.Proc.Dir, "ls-tree", "-r", "--name-only", "physa/init")
	if !strings.Contains(files, ".physa/capabilities/sign-in-2/") || !strings.Contains(gitIn(t, env.Proc.Dir, "show", "physa/init:.physa/capabilities/billing/rules.md"), "Again billing") {
		t.Errorf("the stale consolidation was used:\n%s", files)
	}
}

// TestCappedConsolidateStoresNothing checks that a --consolidate the cap
// stops before its first session stores no consolidation, says the cap was
// reached and how to continue, and exits 1, and that one under a cap that
// holds consolidates and stores as an uncapped one does.
func TestCappedConsolidateStoresNothing(t *testing.T) {
	t.Parallel()
	a := &consolidateAgent{merge: mergeSignIn}
	env := consEnv(t, a, mergeCaps)
	code, out, errOut := runInit(env, "--consolidate", "--cap", "1k")
	if code != 1 || a.starts != 0 || out != "" {
		t.Fatalf("code %d, %d sessions\nstdout %s\nstderr %s", code, a.starts, out, errOut)
	}
	for _, want := range []string{
		"physa init: the cap of 1k tokens was reached: nothing more was started",
		"To continue, run physa init --consolidate with a larger --cap, or none.\n",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "nothing is stored") {
		t.Errorf("the cap's stop is said as a failure:\n%s", errOut)
	}
	consolidated := func() bool {
		st, err := env.Store(context.Background(), env.Proc.Dir)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		cur, err := consolidate.Current(t.Context(), initrecord.Of(st))
		if err != nil {
			t.Fatal(err)
		}
		return cur
	}
	if consolidated() {
		t.Fatal("a consolidation was stored past the cap")
	}
	if code, out, errOut := runInit(env, "--consolidate", "--cap", "10m"); code != 0 || !strings.Contains(out, "Merged capabilities sign-in, sign-in-2 into sign-in") || !consolidated() {
		t.Errorf("under a cap that holds: code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
}

// TestConsolidateSpendAddsUp checks that a second --consolidate adds what it
// spent to the first's in init's record, so a cap sees both.
func TestConsolidateSpendAddsUp(t *testing.T) {
	t.Parallel()
	env := consEnv(t, &consolidateAgent{merge: mergeSignIn}, mergeCaps)
	spent := func() int64 {
		st, err := env.Store(context.Background(), env.Proc.Dir)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		rows, err := initrecord.Of(st).Spending(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Phase == "consolidate" {
				return r.Raw.Input + r.Raw.Output
			}
		}
		return 0
	}
	var after []int64
	for i := range 2 {
		if code, _, errOut := runInit(env, "--consolidate"); code != 0 {
			t.Fatalf("run %d: code %d\n%s", i+1, code, errOut)
		}
		after = append(after, spent())
	}
	if after[0] == 0 || after[1] != 2*after[0] {
		t.Errorf("consolidate spent %d after one run and %d after two", after[0], after[1])
	}
}
