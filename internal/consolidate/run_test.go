package consolidate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/consolidateprompt"
	"github.com/dphaener/physa/internal/consolidating"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

var (
	walkAt  = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	areaOf  = regexp.MustCompile("## The area `([^`]+)`")
	none    = map[string]string{StepMerge: `{"merge":[]}`, StepArea: `{"merge":[],"move":[],"drop":[]}`, StepLift: `{"lift":[]}`}
	nothing = func(step, area string, turn int) (string, error) { return fence(none[step]), nil }
)

func fence(s string) string { return "Thinking.\n\n```json\n" + s + "\n```\n" }

// stepOf is the step a skill is for.
func stepOf(system string) string {
	switch system {
	case consolidateprompt.Merge:
		return StepMerge
	case consolidateprompt.Consolidate:
		return StepArea
	case consolidateprompt.Lift:
		return StepLift
	}
	return "unknown"
}

// fakeAgent records every Spec and answers through reply, given the step a
// session is for (from its skill), the area of an area session, and how
// many turns that session had before. It never runs a real agent.
type fakeAgent struct {
	mu       sync.Mutex
	specs    []adapter.Spec
	steps    map[string]string // session id to step
	areas    map[string]string // session id to area
	turns    map[string]int
	reply    func(step, area string, turn int) (string, error)
	window   int
	noResume bool
}

type windowed struct{ *fakeAgent }

func (w windowed) Window() int { return w.window }

type fakeSession struct {
	text string
	err  error
}

func (f *fakeAgent) Name() string { return "fake" }

func (f *fakeAgent) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	if s.Resume && f.noResume {
		f.mu.Unlock()
		return nil, adapter.ErrNoResume
	}
	if f.steps == nil {
		f.steps, f.areas, f.turns = map[string]string{}, map[string]string{}, map[string]int{}
	}
	if !s.Resume {
		f.steps[s.SessionID] = stepOf(s.System)
		if m := areaOf.FindStringSubmatch(s.Prompt); m != nil {
			f.areas[s.SessionID] = m[1]
		}
	}
	f.specs = append(f.specs, s)
	step, area := f.steps[s.SessionID], f.areas[s.SessionID]
	key := s.SessionID
	if area != "" {
		key = "area " + area // a new session for the same area counts on
	}
	turn := f.turns[key]
	f.turns[key]++
	f.mu.Unlock()
	text, err := f.reply(step, area, turn)
	return fakeSession{text, err}, nil
}

func (s fakeSession) Wait() (adapter.Result, error) {
	return adapter.Result{Text: s.text, Model: "fake-model", Tokens: adapter.Tokens{Input: 10, CachedInput: 2, Output: 3}}, s.err
}

// started is the steps of the sessions started, in order.
func (f *fakeAgent) started() []string {
	var out []string
	for _, s := range f.specs {
		if !s.Resume {
			out = append(out, f.steps[s.SessionID])
		}
	}
	return out
}

// fixCap is one recovered capability of a fixture.
type fixCap struct {
	name, area, parent, promise string
	rules, decisions, questions []string
	terms                       string
}

// entry is an entry's text: heading, meta line, body and reason.
func entry(heading, slug, anchors, body string) string {
	meta := "`" + slug + "`"
	if anchors != "" {
		meta += " · " + anchors
	}
	return "### " + heading + "\n" + meta + "\n" + body + "\nBecause: it was asked for.\n"
}

func (c fixCap) file() string {
	b := "# " + strings.ToUpper(c.name[:1]) + c.name[1:] + "\n\n"
	for _, s := range []struct {
		word string
		es   []string
	}{{"Rules", c.rules}, {"Decisions", c.decisions}, {"Questions", c.questions}} {
		if len(s.es) > 0 {
			b += "## " + s.word + "\n\n" + strings.Join(s.es, "\n") + "\n"
		}
	}
	return recovering.File(c.name, c.promise, b)
}

// seed is a store holding a walk of caps, with renamed its renames, its
// placements, and each capability recovered from it.
func seed(t *testing.T, caps []fixCap, renamed []walk.Rename) *initrecord.Store {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	st := initrecord.Of(s)
	wr := walk.Record{Renamed: renamed}
	var rows []initrecord.Recovered
	for _, c := range caps {
		wr.Capabilities = append(wr.Capabilities, walk.CapabilityRecord{Capability: walk.Capability{Name: c.name, Promise: c.promise,
			Sources: []string{"app/" + c.name + ".go"}}, Area: c.area, Parent: c.parent})
		rows = append(rows, initrecord.Recovered{Capability: c.name, ProposalAt: walkAt, At: walkAt.Add(time.Hour), Status: recovery.StatusRecovered,
			File: c.file(), Terms: c.terms, SessionID: "rec-" + c.name, Details: []byte(`{"dropped":[{"kind":"rule","heading":"Lost one","reasons":["no anchor"]}]}`)})
	}
	body, _ := json.Marshal(wr)
	if err := st.PutWalk(context.Background(), initrecord.Walk{AreasAt: walkAt.Add(-time.Hour), At: walkAt, Model: "m", Body: body}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutPlace(context.Background(), initrecord.Place{WalkAt: walkAt, At: walkAt, Body: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutRecovered(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	return st
}

// consolidate runs Run over st with f, failing t on an error.
func consolidate(t *testing.T, st *initrecord.Store, f *fakeAgent) Result {
	t.Helper()
	res, err := runWith(st, f)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := Text(&b, res); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", b.String())
	return res
}

func runWith(st *initrecord.Store, f *fakeAgent) (Result, error) {
	var a adapter.Adapter = f
	if f.window > 0 {
		a = windowed{f}
	}
	return Run(context.Background(), st, Session{Agent: func() (adapter.Adapter, error) { return a, nil }, Product: "Shop",
		Now: func() time.Time { return walkAt.Add(2 * time.Hour) }})
}

// capOf is the consolidated capability named name, parsed, or the stored
// one when it was left as recovered.
func capOf(t *testing.T, st *initrecord.Store, res Result, name string) capabilitytext.Capability {
	t.Helper()
	for _, c := range res.Capabilities {
		if c.Name == name {
			p, err := capabilitytext.Parse(c.Name, c.Area, c.File, c.Terms)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	rows, _ := st.Recovered(context.Background())
	for _, r := range rows {
		if r.Capability == name && slices.Contains(res.Left, name) {
			p, _ := capabilitytext.Parse(name, capabilitytext.Area{}, r.File, r.Terms)
			return p
		}
	}
	t.Fatalf("no capability %q in %+v", name, res.Capabilities)
	return capabilitytext.Capability{}
}

// refusedAt fails t unless res refuses an item at step whose reason holds
// want.
func refusedAt(t *testing.T, res Result, step, want string) consolidating.Refusal {
	t.Helper()
	for _, r := range res.Record.Refused {
		if r.Step == step && strings.Contains(r.Reason, want) {
			t.Logf("refused (%s) %s: %s", r.Step, r.Item, r.Reason)
			return r
		}
	}
	t.Fatalf("no refusal at %s holding %q in %+v", step, want, res.Record.Refused)
	return consolidating.Refusal{}
}

var signIn = []fixCap{
	{name: "sign-in", area: "accounts", promise: "People sign in.",
		rules:     []string{entry("Sessions last a day", "session-length", "#3", "A session ends a day after sign-in.")},
		decisions: []string{"### Passwords are hashed with bcrypt\n`password-hash` · decided by dphaener, in their words · 2026-01-02 · #4 abc1234\nBcrypt.\nBecause: it is slow.\nRejected: sha1."}},
	{name: "sign-in-2", area: "web", promise: "People sign in from the browser and stay signed in.",
		rules: []string{entry("Browser sessions end on close", "session-length", "#7 def5678", "Closing the browser signs out."),
			entry("The sign-in form remembers the email", "remember-email", "#9", "The email field is filled in.")}},
	{name: "billing", area: "billing", promise: "People pay.",
		rules: []string{entry("Invoices go out monthly", "invoice-monthly", "#11", "Once a month.")}},
}

var signInRename = []walk.Rename{{Area: "web", From: "sign-in", To: "sign-in-2"}}

func TestMergeTwoCapabilitiesIntoOne(t *testing.T) {
	st := seed(t, signIn, signInRename)
	f := &fakeAgent{reply: func(step, area string, turn int) (string, error) {
		if step == StepMerge {
			return fence(`{"merge":[{"capabilities":["sign-in","sign-in-2"],"name":"sign-in","promise_of":"sign-in-2","why":"one promise"}]}`), nil
		}
		return fence(none[step]), nil
	}}
	res := consolidate(t, st, f)
	if len(res.Record.Merged) != 1 || res.Record.Merged[0].Name != "sign-in" {
		t.Fatalf("merged = %+v", res.Record.Merged)
	}
	c := capOf(t, st, res, "sign-in")
	if c.Promise() != "People sign in from the browser and stay signed in." {
		t.Errorf("promise = %q; want sign-in-2's", c.Promise())
	}
	var slugs, heads []string
	anchors := map[string]bool{}
	for _, e := range c.Entries() {
		slugs, heads = append(slugs, e.Slug), append(heads, e.Heading)
		for _, a := range e.Anchors {
			anchors[capabilitytext.AnchorText(a)] = true
		}
	}
	for _, h := range []string{"Sessions last a day", "Passwords are hashed with bcrypt", "Browser sessions end on close", "The sign-in form remembers the email"} {
		if !slices.Contains(heads, h) {
			t.Errorf("merged capability lost %q: %q", h, heads)
		}
	}
	for _, a := range []string{"#3", "#4", "abc1234", "#7", "def5678", "#9"} {
		if !anchors[a] {
			t.Errorf("merged capability lost anchor %s", a)
		}
	}
	if s := slices.Clone(slugs); len(slices.Compact(func() []string { slices.Sort(s); return s }())) != len(slugs) {
		t.Errorf("slugs not unique: %q", slugs)
	}
	if !slices.Contains(slugs, "session-length-2") || len(res.Record.SlugRenames) != 1 {
		t.Errorf("slugs %q, renames %+v; want the collision renamed session-length-2", slugs, res.Record.SlugRenames)
	}
	if len(res.MergedAway) != 1 || res.MergedAway[0].Name != "sign-in-2" || res.MergedAway[0].SessionID != "rec-sign-in-2" || len(res.MergedAway[0].Dropped) != 1 {
		t.Errorf("merged away = %+v", res.MergedAway)
	}
	if got := res.Capabilities[0].MergedFrom; !reflect.DeepEqual(got, []string{"sign-in-2"}) {
		t.Errorf("merged from = %q", got)
	}
	if !reflect.DeepEqual(res.Left, []string{"billing"}) || res.After.Capabilities != 2 || res.Before.Capabilities != 3 {
		t.Errorf("left %q, before %+v, after %+v", res.Left, res.Before, res.After)
	}
	if res.ExitCode() != 0 || res.Model != "fake-model" || res.Tokens.Input == 0 {
		t.Errorf("exit %d, model %q, tokens %+v", res.ExitCode(), res.Model, res.Tokens)
	}
	stored, ok, err := Load(t.Context(), st)
	if err != nil || !ok || !reflect.DeepEqual(stored, res) {
		t.Fatalf("Load = %v, %v; want the result stored", ok, err)
	}
	if cur, err := Current(t.Context(), st); !cur || err != nil {
		t.Errorf("Current = %v, %v; want true", cur, err)
	}
}

func TestSessionsArePromptOnly(t *testing.T) {
	st := seed(t, signIn, signInRename)
	f := &fakeAgent{reply: nothing}
	consolidate(t, st, f)
	if len(f.specs) == 0 {
		t.Fatal("no session ran")
	}
	ids := map[string]bool{}
	for _, s := range f.specs {
		if !s.PromptOnly || len(s.Tools()) != 0 || s.Tools() == nil {
			t.Errorf("session %s is not prompt-only: %+v", s.SessionID, s.Tools())
		}
		if ids[s.SessionID] {
			t.Errorf("session id %s used twice", s.SessionID)
		}
		ids[s.SessionID] = true
	}
	if got := f.started(); !reflect.DeepEqual(got, []string{StepMerge, StepArea, StepArea}) {
		t.Errorf("sessions started = %q; want the merge, then accounts and web (billing has one entry)", got)
	}
}

var billing = []fixCap{
	{name: "invoices", area: "billing", promise: "People get invoices.",
		rules:     []string{entry("Invoices are sent monthly", "invoice-monthly", "#4", "Every month an invoice is sent.")},
		questions: []string{"### Should invoices be weekly?\n`invoice-weekly` · raised by dphaener · 2026-01-02\nNobody has decided."}},
	{name: "payments", area: "billing", promise: "People pay their invoices.",
		rules: []string{entry("An invoice goes out each month", "invoice-cadence", "#5 def5678", "Each month brings an invoice.")}},
	{name: "accounts", area: "accounts", promise: "People sign in.",
		rules: []string{entry("Sessions last a day", "session-length", "#3", "A day."), entry("Passwords are long", "password-length", "#2", "Twelve letters.")}},
}

func TestAreaMergeKeepsEveryAnchor(t *testing.T) {
	st := seed(t, billing, nil)
	f := &fakeAgent{reply: func(step, area string, turn int) (string, error) {
		if step == StepArea && area == "billing" {
			return fence(`{"merge":[{"keep":"invoices/invoice-monthly","drop":["payments/invoice-cadence"],"why":"same"}],"move":[],"drop":[]}`), nil
		}
		return fence(none[step]), nil
	}}
	res := consolidate(t, st, f)
	if len(res.Record.EntryMerges) != 1 {
		t.Fatalf("entry merges = %+v", res.Record.EntryMerges)
	}
	invoices := capOf(t, st, res, "invoices")
	kept := invoices.Entry("invoice-monthly")
	for _, a := range []string{"#4", "#5", "def5678"} {
		if !strings.Contains(kept.Meta(), a) {
			t.Errorf("kept meta %q lacks %s", kept.Meta(), a)
		}
	}
	if kept.Heading != "Invoices are sent monthly" || kept.Body() != "Every month an invoice is sent." {
		t.Errorf("kept entry's words changed: %q", kept.Text())
	}
	if payments := capOf(t, st, res, "payments"); payments.Entry("invoice-cadence") != nil {
		t.Error("the dropped entry is still in payments")
	}
}

func TestUnknownIDsRefusedAndReported(t *testing.T) {
	st := seed(t, billing, nil)
	f := &fakeAgent{reply: func(step, area string, turn int) (string, error) {
		if step == StepArea && area == "billing" {
			return fence(`{"merge":[{"keep":"invoices/invented","drop":["payments/invoice-cadence"],"why":"x"}],
				"move":[{"id":"accounts/session-length","to":"invoices","why":"x"},{"id":"invoices/invoice-monthly","to":"accounts","why":"x"}],
				"drop":[{"id":"invoices/invoice-weekly","answered_by":"elsewhere/rule","why":"x"}]}`), nil
		}
		return fence(none[step]), nil
	}}
	res := consolidate(t, st, f)
	refusedAt(t, res, StepArea, "invoices/invented is not among the entries its session was given")
	refusedAt(t, res, StepArea, "accounts/session-length is not among the entries its session was given")
	refusedAt(t, res, StepArea, `"accounts" is not among the capabilities its session was given`)
	refusedAt(t, res, StepArea, "elsewhere/rule is not among the entries")
	if n := len(res.Record.EntryMerges) + len(res.Record.Moves) + len(res.Record.Dropped); n != 0 {
		t.Errorf("%d choices applied; want none", n)
	}
	if res.After.Entries != res.Before.Entries || len(res.Capabilities) != 0 {
		t.Errorf("entries %d to %d, consolidated %d; want nothing changed", res.Before.Entries, res.After.Entries, len(res.Capabilities))
	}
}

func TestUnsourcedTextRefused(t *testing.T) {
	st := seed(t, billing, nil)
	f := &fakeAgent{reply: func(step, area string, turn int) (string, error) {
		if step == StepArea && area == "billing" {
			return fence(`{"merge":[{"keep":"invoices/invoice-monthly","drop":["payments/invoice-cadence"],"why":"x","text":"### Invoices go out on the first\n` + "`invoice-first`" + ` · #4"}],
				"entries":[{"capability":"invoices","text":"### A rule a session wrote"}],"move":[],"drop":[]}`), nil
		}
		return fence(none[step]), nil
	}}
	res := consolidate(t, st, f)
	refusedAt(t, res, StepArea, `unknown field "text"`)
	refusedAt(t, res, StepArea, "not a key the skill asks for")
	body, _ := json.Marshal(res)
	for _, bad := range []string{"Invoices go out on the first", "A rule a session wrote"} {
		if strings.Contains(string(body), bad) {
			t.Errorf("the session's words %q reached the record", bad)
		}
	}
	payments := capOf(t, st, res, "payments")
	if len(res.Record.EntryMerges) != 0 || payments.Entry("invoice-cadence") == nil {
		t.Error("the refused merge was applied")
	}
}

// liftFixture is two areas of three capabilities each, every rule of one
// repeated word for word by a rule of the other: n groups to lift.
func liftFixture(n int) []fixCap {
	var caps []fixCap
	for ai, area := range []string{"web", "mail"} {
		for ci := range 3 {
			c := fixCap{name: fmt.Sprintf("%s-%d", area, ci), area: area, promise: fmt.Sprintf("People use part %d of %s.", ci, area)}
			for k := ci; k < n; k += 3 {
				w := fmt.Sprintf("alpha%02d beta%02d", k, k)
				c.rules = append(c.rules, entry("Rule "+w, fmt.Sprintf("%c-rule-%02d", area[0], k), fmt.Sprintf("#%d", 100*ai+k+1), "Body "+w+"."))
			}
			caps = append(caps, c)
		}
	}
	return caps
}

// liftAll lifts every group a lift session is given, keeping its first
// rule, and judges nothing else.
func liftAll(prompt *string) func(step, area string, turn int) (string, error) {
	return func(step, area string, turn int) (string, error) {
		if step != StepLift {
			return fence(none[step]), nil
		}
		var items []string
		for _, g := range strings.Split(*prompt, "### Group ")[1:] {
			ids := regexp.MustCompile("Entry `([^`]+)`").FindAllStringSubmatch(g, -1)
			var with []string
			for _, id := range ids[1:] {
				with = append(with, `"`+id[1]+`"`)
			}
			items = append(items, fmt.Sprintf(`{"keep":%q,"with":[%s],"why":"one rule"}`, ids[0][1], strings.Join(with, ",")))
		}
		return fence(`{"lift":[` + strings.Join(items, ",") + `]}`), nil
	}
}

func TestLiftPastBudgetRefused(t *testing.T) {
	st := seed(t, liftFixture(30), nil)
	var prompt string
	f := &fakeAgent{}
	f.reply = func(step, area string, turn int) (string, error) {
		f.mu.Lock()
		prompt = f.specs[len(f.specs)-1].Prompt
		f.mu.Unlock()
		return liftAll(&prompt)(step, area, turn)
	}
	res := consolidate(t, st, f)
	r := refusedAt(t, res, StepLift, fmt.Sprintf("over the limit of %d", mapcheck.MaxRulesLines))
	if len(res.Record.Lifts) == 0 || len(res.Record.Lifts) == 30 {
		t.Fatalf("lifted %d of 30; want some, then the budget refusal", len(res.Record.Lifts))
	}
	if n := capabilitytext.Lines(consolidating.ProductRules("Shop", parseRules(t, res.ProductRules))); n > mapcheck.MaxRulesLines {
		t.Errorf("product rules are %d lines", n)
	}
	keep := strings.Fields(strings.TrimPrefix(r.Item, "lift "))[0]
	name, slug, _ := strings.Cut(keep, "/")
	if held := capOf(t, st, res, name); held.Entry(slug) == nil {
		t.Errorf("the refused lift's rule %s left its capability", keep)
	}
	if len(res.ProductRules) != len(res.Record.Lifts) || res.After.ProductRules != len(res.Record.Lifts) {
		t.Errorf("product rules %d, lifts %d", len(res.ProductRules), len(res.Record.Lifts))
	}
}

// parseRules reads product rule texts as entries.
func parseRules(t *testing.T, texts []string) []capabilitytext.Entry {
	t.Helper()
	c, err := capabilitytext.Parse("p", capabilitytext.Area{}, recovering.File("p", "P.", "# P\n\n## Rules\n\n"+strings.Join(texts, "\n\n")+"\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	return c.Entries()
}

func TestLiftOneAreaRefused(t *testing.T) {
	caps := []fixCap{
		{name: "news", area: "web", promise: "People read news.",
			rules: []string{entry("Every email carries an unsubscribe link", "news-unsub", "#1", "Each email ends with an unsubscribe link.")}},
		{name: "digest", area: "web", promise: "People get digests.",
			rules: []string{entry("Every digest email carries an unsubscribe link at its foot", "digest-unsub", "#2", "Each email ends with an unsubscribe link at its foot.")}},
		{name: "alerts", area: "mail", promise: "People get alerts.",
			rules: []string{entry("Every email carries an unsubscribe link", "alert-unsub", "#3", "Each email ends with an unsubscribe link.")}},
	}
	st := seed(t, caps, nil)
	f := &fakeAgent{reply: func(step, area string, turn int) (string, error) {
		if step == StepLift {
			return fence(`{"lift":[{"keep":"news/news-unsub","with":["digest/digest-unsub"],"why":"same"}]}`), nil
		}
		return fence(none[step]), nil
	}}
	res := consolidate(t, st, f)
	refusedAt(t, res, StepLift, "all in one area")
	if len(res.Record.Lifts) != 0 || len(res.ProductRules) != 0 {
		t.Errorf("lifted %+v", res.Record.Lifts)
	}
}

func TestUnjudgedMergeBlocksLift(t *testing.T) {
	st := seed(t, liftFixture(3), nil)
	f := &fakeAgent{reply: func(step, area string, turn int) (string, error) {
		if step == StepMerge {
			return "I cannot answer in json.", nil
		}
		if step == StepLift {
			return "", errors.New("no lift session should run")
		}
		return fence(none[step]), nil
	}}
	res := consolidate(t, st, f)
	refusedAt(t, res, StepMerge, "its step is left as it was")
	refusedAt(t, res, StepLift, notJudged)
	if slices.Contains(f.started(), StepLift) || len(res.Record.Lifts) != 0 {
		t.Errorf("a lift ran over unjudged capabilities: %q", f.started())
	}
	if res.ExitCode() != 1 {
		t.Errorf("exit code = %d; want 1", res.ExitCode())
	}
	if _, ok, _ := Load(t.Context(), st); !ok {
		t.Error("the record was not stored")
	}
	var merge SessionRun
	for _, s := range res.Sessions {
		if s.Step == StepMerge {
			merge = s
		}
	}
	if f.turns[merge.IDs[0]] != 2 || merge.Answered {
		t.Errorf("merge session = %+v, turns %d; want one answer-now turn, then left", merge, f.turns[merge.IDs[0]])
	}
}

func TestAgentErrorLeavesStepAndContinues(t *testing.T) {
	st := seed(t, billing, nil)
	f := &fakeAgent{noResume: true, reply: func(step, area string, turn int) (string, error) {
		if area == "billing" {
			return "", fmt.Errorf("rate limited: %w", adapter.ErrFailed)
		}
		if area == "accounts" && turn == 0 {
			return "no json", nil
		}
		return fence(none[step]), nil
	}}
	res := consolidate(t, st, f)
	refusedAt(t, res, StepArea, "rate limited")
	if res.ExitCode() != 1 {
		t.Errorf("exit code = %d; want 1", res.ExitCode())
	}
	var accounts SessionRun
	for _, s := range res.Sessions {
		if strings.Contains(s.Item, "accounts") {
			accounts = s
		}
	}
	if !accounts.Answered || len(accounts.IDs) != 2 {
		t.Errorf("accounts session = %+v; want answered in a new session, as the agent cannot resume", accounts)
	}
}

func TestSecondRunGivesTheSameResult(t *testing.T) {
	st := seed(t, signIn, signInRename)
	f := &fakeAgent{reply: func(step, area string, turn int) (string, error) {
		switch step {
		case StepMerge:
			return fence(`{"merge":[{"capabilities":["sign-in","sign-in-2"],"name":"sign-in","promise_of":"sign-in","why":"x"}]}`), nil
		case StepArea:
			return fence(`{"merge":[],"move":[],"drop":[]}`), nil
		}
		return fence(none[step]), nil
	}}
	first := consolidate(t, st, f)
	second := consolidate(t, st, f)
	if len(second.Record.Merged) != 1 || len(second.Record.SlugRenames) != 1 {
		t.Fatalf("second run did not start from the recovered rows: %+v", second.Record)
	}
	first.Sessions, second.Sessions, first.Tokens, second.Tokens = nil, nil, adapter.Tokens{}, adapter.Tokens{}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second run differs:\n%+v\n%+v", first, second)
	}
}

func TestCurrentGoesStaleOnNewRecovery(t *testing.T) {
	st := seed(t, billing, nil)
	consolidate(t, st, &fakeAgent{reply: nothing})
	if cur, _ := Current(t.Context(), st); !cur {
		t.Fatal("not current right after the run")
	}
	rows, _ := st.Recovered(context.Background())
	rows[0].Terms = "**Seat**: A paid place."
	if err := st.PutRecovered(context.Background(), rows[:1]); err != nil {
		t.Fatal(err)
	}
	if cur, err := Current(t.Context(), st); cur || err != nil {
		t.Errorf("Current after a new recovery = %v, %v; want stale", cur, err)
	}
}

func TestNoWalkIsClearError(t *testing.T) {
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st := initrecord.Of(s)
	f := &fakeAgent{reply: nothing}
	if _, err := runWith(st, f); err == nil || !strings.Contains(err.Error(), "--walk, --place and --recover") {
		t.Fatalf("err = %v; want the phases to run first", err)
	} else {
		t.Logf("no walk: %v", err)
	}
	if cur, err := Current(t.Context(), st); cur || err != nil {
		t.Errorf("Current = %v, %v; want false, nil", cur, err)
	}
	body, _ := json.Marshal(walk.Record{})
	st.PutWalk(context.Background(), initrecord.Walk{At: walkAt, Body: body})
	st.PutPlace(context.Background(), initrecord.Place{WalkAt: walkAt, At: walkAt, Body: []byte(`{}`)})
	if _, err := runWith(st, f); err == nil || !strings.Contains(err.Error(), "--recover") {
		t.Fatalf("err = %v; want --recover first", err)
	} else {
		t.Logf("no recovered row: %v", err)
	}
	if len(f.specs) != 0 {
		t.Errorf("%d sessions ran", len(f.specs))
	}
}

func TestCancelledStoresNothing(t *testing.T) {
	st := seed(t, signIn, signInRename)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeAgent{reply: nothing}
	if _, err := Run(ctx, st, Session{Agent: func() (adapter.Adapter, error) { return f, nil }}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want cancelled", err)
	}
	if _, ok, _ := Load(t.Context(), st); ok {
		t.Error("a cancelled run stored a record")
	}
}

// A cancel during a step starts no further session: the agent's fake takes
// no ctx check of its own here, so only the run's guard can hold it back.
func TestCancelDuringAStepStartsNoFurtherSession(t *testing.T) {
	st := seed(t, signIn, signInRename)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var late atomic.Int32
	f := &fakeAgent{reply: func(step, area string, turn int) (string, error) { cancel(); return nothing(step, area, turn) }}
	a := cancelWatch{f, &late}
	_, err := Run(ctx, st, Session{Agent: func() (adapter.Adapter, error) { return a, nil }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want cancelled", err)
	}
	if late.Load() != 0 {
		t.Errorf("%d sessions started after the cancel", late.Load())
	}
	if _, ok, _ := Load(t.Context(), st); ok {
		t.Error("a cancelled run stored a record")
	}
}

// cancelWatch counts the sessions started with a done ctx, and lets them
// through so the fake's own ctx check does not hide the run's.
type cancelWatch struct {
	*fakeAgent
	late *atomic.Int32
}

func (w cancelWatch) Start(ctx context.Context, s adapter.Spec) (adapter.Session, error) {
	if ctx.Err() != nil {
		w.late.Add(1)
	}
	return w.fakeAgent.Start(context.WithoutCancel(ctx), s)
}

func TestAreaSplitToFitTheWindow(t *testing.T) {
	long := strings.Repeat("A long sentence about invoices and their many lines. ", 40)
	caps := []fixCap{
		{name: "invoices", area: "billing", promise: "People get invoices.",
			rules: []string{entry("Invoices are sent monthly", "invoice-monthly", "#4", long), entry("Invoices name the seat", "invoice-seat", "#6", long)}},
		{name: "payments", area: "billing", promise: "People pay.",
			rules: []string{entry("Payments settle in a day", "pay-settle", "#5", long), entry("Payments are refunded whole", "pay-refund", "#7", long)}},
	}
	st := seed(t, caps, nil)
	one := reading.Tokens(consolidateprompt.Consolidate) + reading.Tokens(caps[0].file())
	f := &fakeAgent{reply: nothing, window: one + one/2 + 16_000}
	res := consolidate(t, st, f)
	var items []string
	for _, s := range res.Sessions {
		items = append(items, s.Item)
	}
	if !reflect.DeepEqual(items, []string{"billing (part 1 of 2)", "billing (part 2 of 2)"}) {
		t.Errorf("sessions = %q; want the area split by capability", items)
	}
}
