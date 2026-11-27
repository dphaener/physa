package propose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// fixture is a repo with two changes and a map whose entries cite them.
type fixture struct {
	root   string
	c1, c2 string // short ids, oldest first
	batch  draft.Batch
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// folder writes text, a capability as a single text, as the folder dir
// under root, through mapfolder.Split.
func folder(t *testing.T, root, dir, text string) {
	t.Helper()
	f, err := mapfolder.Split(text)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	if err := f.Write(root, dir); err != nil {
		t.Fatal(err)
	}
}

const deliveryPromise = "Webhooks reach customers at least once."

// newFixture makes the repo. Every scratch copy goes to a TMPDIR of the
// test's own, so a test can tell that none is left behind.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	f := &fixture{root: t.TempDir()}
	gitIn(t, f.root, "init", "-q", "-b", "main")
	gitIn(t, f.root, "commit", "-q", "--allow-empty", "-m", "Add delivery")
	f.c1 = gitIn(t, f.root, "rev-parse", "--short=7", "HEAD")
	gitIn(t, f.root, "commit", "-q", "--allow-empty", "-m", "Fix retries")
	f.c2 = gitIn(t, f.root, "rev-parse", "--short=7", "HEAD")

	write(t, f.root, "docs/vision.md", "vision\n")
	write(t, f.root, ".physa/README.md", "map\n")
	write(t, f.root, ".physa/glossary.md", "# Glossary\n\n**Map**: the product's knowledge.\n")
	folder(t, f.root, ".physa/product", "---\npromise: A product.\n---\n# Product\n\n## Rules\n\n"+
		"### Cite sources\n`cite-sources` · from docs/vision.md\nEvery rule names its source.\nBecause: people check.\n")
	folder(t, f.root, ".physa/capabilities/delivery", "---\npromise: "+deliveryPromise+"\nstewards: [dana]\n---\n# Delivery\n\n"+
		"## Rules\n\n### Sends are queued\n`queued-sends` · "+f.c1+"\nEvery send goes through the queue.\nBecause: sends survive a restart.\n\n"+
		"## Decisions\n\n### One queue\n`one-queue` · decided by dana, in their words · 2026-03-01 · "+f.c1+"\nOne queue for every endpoint.\nBecause: simpler to run.\n")
	folder(t, f.root, ".physa/capabilities/billing", "---\npromise: Customers are billed for what they use.\n---\n# Billing\n\n"+
		"## Questions\n\n### Bill per seat?\n`per-seat` · raised by sam · 2026-03-02\n")

	f.batch = draft.Batch{
		Changes: []learn.ChangeSignals{{
			ChangeRef: learn.ChangeRef{Short: f.c2, Subject: "Fix retries"},
			Signals: []learn.Signal{
				{Kind: learn.KindCorrection, Changes: []string{f.c2}},
				{Kind: learn.KindArguedOptions, Changes: []string{f.c2}},
				{Kind: learn.KindRevert, Changes: []string{f.c2, f.c1}},
				{Kind: "served-not-loaded", Changes: []string{f.c1, f.c2}, Capability: "delivery"},
			},
		}},
		Capabilities: []string{"billing", "delivery"},
	}
	return f
}

func (f *fixture) opts() Options {
	return Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) { return git.Open(context.Background(), d) }}
}

// snapshot reads every file under dir.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		out[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameFiles(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("the map now has %d files, had %d", len(after), len(before))
	}
	for p, b := range before {
		if after[p] != b {
			t.Errorf("%s changed:\n%s", p, after[p])
		}
	}
}

func noScratchLeft(t *testing.T) {
	t.Helper()
	ents, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		t.Errorf("left behind in the temp dir: %s", e.Name())
	}
}

func (f *fixture) rule(slug, anchor string) draft.Proposal {
	return draft.Proposal{
		Kind: "rule", Capability: "delivery", Section: "Rules",
		Signal:  draft.SignalRef{Kind: learn.KindCorrection, Changes: []string{f.c2}},
		Changes: []string{f.c2},
		Entry:   "### Retries go through the queue\n`" + slug + "` · " + anchor + "\nA failed send is retried only through the queue.\nBecause: a deploy dropped the retries.",
	}
}

func TestKeepsEachKind(t *testing.T) {
	f := newFixture(t)
	ps := []draft.Proposal{
		f.rule("queue-retries", f.c2),
		{
			Kind: "decision", Capability: "delivery", Section: "Decisions",
			Signal:  draft.SignalRef{Kind: learn.KindArguedOptions, Changes: []string{f.c2}},
			Changes: []string{f.c2},
			Entry: "### Six attempts\n`retry-limit` · decided by dana, choosing an option · 2026-03-02 · " + f.c2 +
				"\nSix attempts, then a person.\nBecause: a bounded window.\nRejected:\n- Unlimited attempts: floods endpoints.\n- Three attempts: too short.",
		},
		{
			Kind: "question", Capability: "delivery", Section: "Questions",
			Signal:  draft.SignalRef{Kind: learn.KindRevert, Changes: []string{f.c1, f.c2}}, // any order
			Changes: []string{f.c1},
			Entry:   "### Should ordering be per customer?\n`ordering-scope` · raised by sam · 2026-11-01 · " + f.c1 + "\nThe revert settled nothing.",
		},
		{
			Kind: "promise", Capability: "delivery", Section: "promise",
			Signal:  draft.SignalRef{Kind: "served-not-loaded", Changes: []string{f.c1, f.c2}},
			Changes: []string{f.c1},
			Entry:   "Webhooks reach customers at least once, in order per endpoint.",
		},
		func() draft.Proposal {
			p := f.rule("bill-retries", f.c2) // billing has no Rules section yet
			p.Capability = "billing"
			return p
		}(),
	}
	before := snapshot(t, f.root)
	kept, dropped := Check(t.Context(), f.root, f.batch, ps, f.opts())
	for _, d := range dropped {
		t.Errorf("dropped %s: %s", d.Proposal.Kind, d.Reason)
	}
	want := []string{"queue-retries", "retry-limit", "ordering-scope", "", "bill-retries"}
	if len(kept) != len(want) {
		t.Fatalf("kept %d, want %d", len(kept), len(want))
	}
	for i, k := range kept {
		if k.Slug != want[i] || k.Proposal.Kind != ps[i].Kind {
			t.Errorf("kept[%d] = %s %q, want %s %q", i, k.Proposal.Kind, k.Slug, ps[i].Kind, want[i])
		}
	}
	sameFiles(t, before, snapshot(t, f.root))
	noScratchLeft(t)
}

func TestDropReasons(t *testing.T) {
	f := newFixture(t)
	edit := func(fn func(*draft.Proposal)) draft.Proposal {
		p := f.rule("queue-retries", f.c2)
		fn(&p)
		return p
	}
	promise := func(entry string) draft.Proposal {
		return draft.Proposal{Kind: "promise", Capability: "delivery", Section: "promise",
			Signal:  draft.SignalRef{Kind: "served-not-loaded", Changes: []string{f.c1, f.c2}},
			Changes: []string{f.c1}, Entry: entry}
	}
	cases := []struct {
		name string
		p    draft.Proposal
		want string
	}{
		{"kind", edit(func(p *draft.Proposal) { p.Kind = "term" }), `kind "term" is not one of rule, decision, question, promise`},
		{"section", edit(func(p *draft.Proposal) { p.Section = "Decisions" }), `section "Decisions" does not match a rule: want Rules`},
		{"capability", edit(func(p *draft.Proposal) { p.Capability = "nosuch" }), "the map has no capability `nosuch`"},
		{"product is not a capability", edit(func(p *draft.Proposal) { p.Capability = "product" }), "the map has no capability `product`"},
		{"signal kind", edit(func(p *draft.Proposal) { p.Signal.Kind = learn.KindRevert }), "no signal in the batch supports it"},
		{"signal changes", edit(func(p *draft.Proposal) { p.Signal.Changes = []string{f.c1} }), "no signal in the batch supports it"},
		{"no changes", edit(func(p *draft.Proposal) { p.Changes = nil }), "it is anchored to no change"},
		{"change not the signal's", edit(func(p *draft.Proposal) { p.Changes = []string{f.c1} }), "change `" + f.c1 + "` is not one its signal is about"},
		{"empty entry", edit(func(p *draft.Proposal) { p.Entry = " \n " }), "its entry is empty"},
		{"promise on two lines", promise("Webhooks arrive.\nIn order."), "a promise is one line"},
		{"promise unchanged", promise(" " + deliveryPromise + " "), "the promise is the same as today's"},
		{"map check", edit(func(p *draft.Proposal) { p.Entry = strings.Split(p.Entry, "\nBecause:")[0] }),
			"the map check finds: .physa/capabilities/delivery/rules.md:"},
		{"duplicate slug", f.rule("cite-sources", f.c2), `slug "cite-sources" is already used at .physa/product/rules.md:`},
		{"two entries", edit(func(p *draft.Proposal) {
			p.Entry += "\n\n### Another\n`another` · " + f.c2 + "\nMore.\nBecause: more."
		}), "it does not add exactly one rule to `delivery`"},
		{"no heading", edit(func(p *draft.Proposal) { p.Entry = "Retries go through the queue." }),
			"it does not add exactly one rule to `delivery`"},
		{"anchors cite another", f.rule("queue-retries", "from docs/vision.md"), "its anchors cite none of its changes (" + f.c2 + ")"},
	}
	before := snapshot(t, f.root)
	var all []draft.Proposal
	for _, c := range cases {
		all = append(all, c.p)
		t.Run(c.name, func(t *testing.T) {
			kept, dropped := Check(t.Context(), f.root, f.batch, []draft.Proposal{c.p}, f.opts())
			if len(kept) != 0 || len(dropped) != 1 {
				t.Fatalf("kept %d, dropped %d; want one dropped", len(kept), len(dropped))
			}
			if !strings.Contains(dropped[0].Reason, c.want) {
				t.Errorf("reason %q, want it to hold %q", dropped[0].Reason, c.want)
			}
			t.Logf("dropped: %s", dropped[0].Reason)
		})
	}
	// Together, every one is dropped, in order: none is lost.
	kept, dropped := Check(t.Context(), f.root, f.batch, all, f.opts())
	if len(kept) != 0 || len(dropped) != len(all) {
		t.Fatalf("kept %d, dropped %d of %d", len(kept), len(dropped), len(all))
	}
	for i, d := range dropped {
		if !strings.Contains(d.Reason, cases[i].want) {
			t.Errorf("dropped[%d] reason %q, want %q", i, d.Reason, cases[i].want)
		}
	}
	sameFiles(t, before, snapshot(t, f.root))
	noScratchLeft(t)
}

func TestDropAllWhenTheMapCannotBeRead(t *testing.T) {
	f := newFixture(t)
	ps := []draft.Proposal{f.rule("a-rule", f.c2), f.rule("b-rule", f.c2)}
	kept, dropped := Check(t.Context(), t.TempDir(), f.batch, ps, f.opts())
	if len(kept) != 0 || len(dropped) != 2 || !strings.HasPrefix(dropped[1].Reason, "the map cannot be read:") {
		t.Fatalf("kept %v, dropped %v", kept, dropped)
	}
}

func TestPreexistingProblemKeepsAGoodProposal(t *testing.T) {
	f := newFixture(t)
	// A decision with no date, after the Rules section the proposal goes in:
	// its problem's line moves, and it is still the same problem.
	p := ".physa/capabilities/delivery/decisions.md"
	b, err := os.ReadFile(filepath.Join(f.root, p))
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(b), " · 2026-03-01 · ", " · ", 1)
	write(t, f.root, p, broken)
	// And, in billing, a doc anchor that names no file and a question with
	// that decision's slug: the decision's problem says where the question
	// is, a line a rule added to billing moves.
	folder(t, f.root, ".physa/capabilities/billing", "---\npromise: Billed.\n---\n# Billing\n\n## Rules\n\n"+
		"### Bills are exact\n`exact-bills` · from docs/gone.md\nBills are exact.\nBecause: trust.\n\n"+
		"## Questions\n\n### One queue for bills?\n`one-queue` · raised by sam · 2026-03-02\n")
	// And a capability whose rules are already over their limit, which any
	// rule lengthens.
	folder(t, f.root, ".physa/capabilities/zones", "---\npromise: Zones.\n---\n# Zones\n\n## Rules\n\n"+
		"### Zones are kept\n`zones-kept` · from docs/vision.md\n"+strings.Repeat("Zones are kept.\n", 160)+"Because: zones.\n")
	for _, capability := range []string{"delivery", "billing", "zones"} {
		p := f.rule("queue-retries", f.c2)
		p.Capability = capability
		kept, dropped := Check(t.Context(), f.root, f.batch, []draft.Proposal{p}, f.opts())
		if len(kept) != 1 || len(dropped) != 0 {
			t.Fatalf("%s: kept %v, dropped %v", capability, kept, dropped)
		}
	}
}

func TestDropAddedProblemQuotesOnlyTheNewOne(t *testing.T) {
	f := newFixture(t)
	folder(t, f.root, ".physa/capabilities/billing", "---\npromise: Billed.\n---\n# Billing\n\n## Rules\n\n"+
		"### Bills are exact\n`exact-bills` · from docs/gone.md\nBills are exact.\nBecause: trust.\n")
	_, dropped := Check(t.Context(), f.root, f.batch, []draft.Proposal{f.rule("queue-retries", "from docs/missing.md")}, f.opts())
	if len(dropped) != 1 {
		t.Fatalf("dropped %v", dropped)
	}
	r := dropped[0].Reason
	if !strings.Contains(r, `anchor "docs/missing.md" does not name a file`) || strings.Contains(r, "gone.md") {
		t.Errorf("reason %q", r)
	}
}

func TestResolversAreAskedOnce(t *testing.T) {
	f := newFixture(t)
	folder(t, f.root, ".physa/capabilities/billing", "---\npromise: Billed.\n---\n# Billing\n\n## Rules\n\n"+
		"### Bills are exact\n`exact-bills` · #12\nBills are exact.\nBecause: trust.\n")
	var prs, opens int
	o := Options{
		OpenRepo: func(_ context.Context, root string) (vcs.Repo, error) {
			opens++
			return git.Open(context.Background(), root)
		},
		PRExists: func(_ context.Context, n int) (bool, error) { prs++; return n == 12, nil },
	}
	ps := []draft.Proposal{f.rule("a-rule", f.c2), f.rule("b-rule", f.c2), f.rule("c-rule", f.c2)}
	kept, dropped := Check(t.Context(), f.root, f.batch, ps, o)
	if len(kept) != 3 || len(dropped) != 0 {
		t.Fatalf("kept %v, dropped %v", kept, dropped)
	}
	if prs != 1 || opens != 1 {
		t.Errorf("PRExists asked %d times, OpenRepo %d; want once each", prs, opens)
	}
}

func TestResolverErrorIsTheSameEveryTime(t *testing.T) {
	f := newFixture(t)
	folder(t, f.root, ".physa/capabilities/billing", "---\npromise: Billed.\n---\n# Billing\n\n## Rules\n\n"+
		"### Bills are exact\n`exact-bills` · #12\nBills are exact.\nBecause: trust.\n")
	calls := 0
	o := Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) { return git.Open(context.Background(), d) }, PRExists: func(context.Context, int) (bool, error) { calls++; return false, errors.New("gh is not installed") }}
	kept, _ := Check(t.Context(), f.root, f.batch, []draft.Proposal{f.rule("a-rule", f.c2)}, o)
	if len(kept) != 1 || calls != 1 {
		t.Errorf("kept %d, resolver called %d times", len(kept), calls)
	}
}

func report(session string) Report {
	r := Report{Version: ReportVersion, Range: "abc1234..def5678", Changes: 1, Signals: 2, Capabilities: 1,
		Session: session, Appetite: 60000}
	switch session {
	case SessionRan:
		r.Tokens = adapter.Tokens{Input: 1200, CachedInput: 800, Output: 300}
		r.Kept = []Kept{{Proposal: draft.Proposal{Kind: "rule", Capability: "delivery", Section: "Rules",
			Signal: draft.SignalRef{Kind: "correction", Changes: []string{"def5678"}}, Changes: []string{"def5678"},
			Entry: "### Retries go through the queue\n`queue-retries` · def5678\nBody.\nBecause: b."}, Slug: "queue-retries"}}
		r.Dropped = append([]Dropped{{Proposal: draft.Proposal{Kind: "decision", Capability: "delivery"}, Reason: "no signal in the batch supports it"}},
			FromMalformed([]draft.Malformed{{Raw: "7", Reason: "a proposal must be a JSON object"}})...)
	case SessionStopped:
		r.Tokens = adapter.Tokens{Input: 59000, Output: 1500}
	case SessionNone:
		r.Changes, r.Signals, r.Capabilities = 0, 0, 0
	}
	return r
}

func TestWriteMarkdown(t *testing.T) {
	cases := map[string]string{
		SessionRan: "# 1 proposal kept from 2 signals on 1 change\n\n" +
			"Range `abc1234..def5678`; the batch bears on 1 capability.\n\n" +
			"## delivery · Rules\n\nFrom a correction signal on def5678.\n\n" +
			"### Retries go through the queue\n`queue-retries` · def5678\nBody.\nBecause: b.\n\n" +
			"## Dropped\n\n- decision for `delivery`: no signal in the batch supports it\n" +
			"- an item that is not a proposal: a proposal must be a JSON object\n\n" +
			"Tokens: 1200 in (800 cached), 300 out of an appetite of 60000.\n",
		SessionStopped: "# 0 proposals kept from 2 signals on 1 change\n\n" +
			"Range `abc1234..def5678`; the batch bears on 1 capability.\n\n" +
			"Stopped at the appetite (60000 tokens) after 59000 in (0 cached), 1500 out; nothing kept.\n",
		SessionNone: "# 0 proposals kept from 0 signals on 0 changes\n\n" +
			"Range `abc1234..def5678`; the batch bears on 0 capabilities.\n\n" +
			"No signals, so no session started.\n",
	}
	for session, want := range cases {
		var b bytes.Buffer
		if err := WriteMarkdown(&b, report(session)); err != nil {
			t.Fatal(err)
		}
		if b.String() != want {
			t.Errorf("%s:\n%s\nwant\n%s", session, b.String(), want)
		}
	}
}

func TestSignalLineArticle(t *testing.T) {
	cases := map[string]string{
		learn.KindCorrection:   "From a correction signal on def5678.",
		learn.KindIntervention: "From an intervention signal on def5678.",
		learn.KindAnsweredAsk:  "From an answered-ask signal on def5678.",
	}
	for kind, want := range cases {
		r := Report{Version: ReportVersion, Session: SessionNone, Kept: []Kept{{
			Proposal: draft.Proposal{Kind: "rule", Capability: "delivery", Section: "Rules",
				Signal: draft.SignalRef{Kind: kind, Changes: []string{"def5678"}}, Changes: []string{"def5678"},
				Entry: "### Retries go through the queue\n`queue-retries` · def5678\nBody.\nBecause: b."},
			Slug: "queue-retries",
		}}}
		var b bytes.Buffer
		if err := WriteMarkdown(&b, r); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), want) {
			t.Errorf("%s: output lacks %q:\n%s", kind, want, b.String())
		}
	}
}

func TestWriteJSON(t *testing.T) {
	for _, session := range []string{SessionRan, SessionStopped, SessionNone} {
		var b bytes.Buffer
		if err := WriteJSON(&b, report(session)); err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b.Bytes(), &raw); err != nil {
			t.Fatalf("%s: %v\n%s", session, err, b.String())
		}
		for _, key := range []string{"version", "range", "changes", "signals", "capabilities", "session", "draft_session", "appetite", "tokens", "kept", "dropped"} {
			if _, ok := raw[key]; !ok {
				t.Errorf("%s: no %q in\n%s", session, key, b.String())
			}
		}
		if strings.Contains(b.String(), "null") {
			t.Errorf("%s: null in\n%s", session, b.String())
		}
		var got Report
		if err := json.Unmarshal(b.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Version != ReportVersion || got.Session != session || got.Tokens != report(session).Tokens {
			t.Errorf("%s: decoded %+v", session, got)
		}
		if session == SessionRan && (len(got.Kept) != 1 || got.Kept[0].Slug != "queue-retries" || len(got.Dropped) != 2 || got.Dropped[1].Raw != "7") {
			t.Errorf("ran: decoded %+v", got)
		}
	}
	if !strings.Contains(mustJSON(t, report(SessionRan)), `"cached_input": 800`) {
		t.Error("tokens are not in snake_case")
	}
}

func mustJSON(t *testing.T, r Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := WriteJSON(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func (f *fixture) second(slug string) draft.Proposal {
	p := f.rule(slug, f.c2)
	p.Entry = strings.Replace(p.Entry, "Retries go through the queue", "Dead sends are parked", 1)
	return p
}

func TestApplyAllLandsEachInOrder(t *testing.T) {
	f := newFixture(t)
	ps := []draft.Proposal{f.rule("queue-retries", f.c2), f.second("park-dead-sends"), {
		Kind: "promise", Capability: "delivery", Section: "promise",
		Signal:  draft.SignalRef{Kind: "served-not-loaded", Changes: []string{f.c1, f.c2}},
		Changes: []string{f.c1},
		Entry:   "Webhooks reach customers at least once, in order per endpoint.",
	}}
	kept, dropped := Check(t.Context(), f.root, f.batch, ps, f.opts())
	if len(dropped) != 0 || len(kept) != 3 {
		t.Fatalf("Check kept %d, dropped %v", len(kept), dropped)
	}
	applied, dropped := ApplyAll(t.Context(), f.root, f.batch, kept, f.opts())
	if len(dropped) != 0 || len(applied) != 3 {
		t.Fatalf("applied %d, dropped %v", len(applied), dropped)
	}
	text := read(t, f.root, ".physa/capabilities/delivery/rules.md")
	a, b := strings.Index(text, "`queue-retries`"), strings.Index(text, "`park-dead-sends`")
	if a < 0 || b < a {
		t.Errorf("entries are missing or out of order:\n%s", text)
	}
	text = read(t, f.root, ".physa/capabilities/delivery/index.md")
	if !strings.Contains(text, "promise: Webhooks reach customers at least once, in order per endpoint.\n") ||
		strings.Contains(text, "promise: "+deliveryPromise+"\n") {
		t.Errorf("the promise was not replaced in place:\n%s", text)
	}
	res, err := mapcheck.CheckWith(t.Context(), f.root, mapcheck.Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) { return git.Open(context.Background(), d) }})
	if err != nil || len(res.Problems) != 0 {
		t.Errorf("map check: %v %v", err, res.Problems)
	}
	noScratchLeft(t)
}

func TestApplyAllDropsADuplicateSlug(t *testing.T) {
	f := newFixture(t)
	kept := []Kept{{Proposal: f.rule("queue-retries", f.c2)}, {Proposal: f.second("queue-retries")}}
	file := filepath.Join(f.root, ".physa/capabilities/delivery/rules.md")
	applied, dropped := ApplyAll(t.Context(), f.root, f.batch, kept, f.opts())
	if len(applied) != 1 || applied[0].Slug != "queue-retries" || len(dropped) != 1 {
		t.Fatalf("applied %v, dropped %v", applied, dropped)
	}
	t.Logf("dropped reason: %s", dropped[0].Reason)
	if !strings.Contains(dropped[0].Reason, "queue-retries") {
		t.Errorf("the reason does not name the slug: %s", dropped[0].Reason)
	}
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), "Dead sends are parked") || !strings.Contains(string(raw), "Retries go through the queue") {
		t.Errorf("the file is not as after the first proposal:\n%s", raw)
	}
	t.Logf("file after:\n%s", raw)
}

func TestWhatNamesUnreadableOutputPlainly(t *testing.T) {
	cases := []struct {
		d    Dropped
		want string
	}{
		{Dropped{Reason: "it has no fenced json block"}, "the session's output, which could not be read"},
		{Dropped{Raw: "7"}, "an item that is not a proposal"},
		{Dropped{Proposal: draft.Proposal{Kind: "decision"}}, "decision for no capability"},
	}
	for _, c := range cases {
		if got := what(c.d); got != c.want {
			t.Errorf("what(%+v) = %q, want %q", c.d, got, c.want)
		}
	}
	if got := plural(1, "token") + " " + plural(2, "token"); got != "1 token 2 tokens" {
		t.Errorf("plural = %q", got)
	}
}

// read reads the file rel under root.
func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// checked fails t unless the map under root passes the map check.
func checked(t *testing.T, root string) {
	t.Helper()
	res, err := mapcheck.CheckWith(t.Context(), root, mapcheck.Options{OpenRepo: func(_ context.Context, d string) (vcs.Repo, error) { return git.Open(context.Background(), d) }})
	if err != nil || len(res.Problems) != 0 {
		t.Errorf("map check: %v %v", err, res.Problems)
	}
}

func (f *fixture) decision(meta string) draft.Proposal {
	return draft.Proposal{
		Kind: "decision", Capability: "delivery", Section: "Decisions",
		Signal:  draft.SignalRef{Kind: learn.KindArguedOptions, Changes: []string{f.c2}},
		Changes: []string{f.c2},
		Entry: "### Six attempts\n`retry-limit` · " + meta + " · " + f.c2 +
			"\nSix attempts, then a person.\nBecause: a bounded window.\nRejected:\n- Unlimited attempts: floods endpoints.",
	}
}

func (f *fixture) question(meta string) draft.Proposal {
	return draft.Proposal{
		Kind: "question", Capability: "delivery", Section: "Questions",
		Signal:  draft.SignalRef{Kind: learn.KindRevert, Changes: []string{f.c2, f.c1}},
		Changes: []string{f.c1},
		Entry:   "### Should ordering be per customer?\n`ordering-scope` · " + meta + " · " + f.c1 + "\nThe revert settled nothing.",
	}
}

func TestDecisionLandsInItsKindFileWithItsContentsLine(t *testing.T) {
	f := newFixture(t)
	dir := ".physa/capabilities/delivery/"
	index := read(t, f.root, dir+"index.md")
	applied, dropped := ApplyAll(t.Context(), f.root, f.batch, []Kept{{Proposal: f.decision("decided by dana, choosing an option · 2026-03-02")}}, f.opts())
	if len(applied) != 1 || applied[0].Slug != "retry-limit" || len(dropped) != 0 {
		t.Fatalf("applied %v, dropped %v", applied, dropped)
	}
	text := read(t, f.root, dir+"decisions.md")
	t.Logf("decisions.md:\n%s", text)
	if !strings.Contains(text, "## Contents\n\n- [One queue](#one-queue)\n- [Six attempts](#six-attempts)\n\n## Entries\n") {
		t.Errorf("decisions.md lacks the new contents line")
	}
	if !strings.HasSuffix(text, "Because: simpler to run.\n\n"+f.decision("decided by dana, choosing an option · 2026-03-02").Entry+"\n") {
		t.Errorf("the decision is not last under ## Entries")
	}
	if strings.Contains(read(t, f.root, dir+"rules.md"), "Six attempts") || read(t, f.root, dir+"index.md") != index {
		t.Errorf("a file other than decisions.md changed")
	}
	checked(t, f.root)
}

func TestQuestionMakesItsKindFileAndIndexLinksIt(t *testing.T) {
	f := newFixture(t)
	dir := ".physa/capabilities/delivery/"
	applied, dropped := ApplyAll(t.Context(), f.root, f.batch, []Kept{{Proposal: f.question("raised by sam · 2026-11-01")}}, f.opts())
	if len(applied) != 1 || applied[0].Slug != "ordering-scope" || len(dropped) != 0 {
		t.Fatalf("applied %v, dropped %v", applied, dropped)
	}
	text := read(t, f.root, dir+"questions.md")
	t.Logf("questions.md:\n%s", text)
	want := "# Delivery · Questions\n\n## Contents\n\n- [Should ordering be per customer?](#should-ordering-be-per-customer)\n\n## Entries\n\n" +
		f.question("raised by sam · 2026-11-01").Entry + "\n"
	if text != want {
		t.Errorf("questions.md =\n%s\nwant\n%s", text, want)
	}
	index := read(t, f.root, dir+"index.md")
	t.Logf("index.md:\n%s", index)
	if !strings.HasSuffix(index, "\n\n- [Rules](rules.md)\n- [Decisions](decisions.md)\n- [Questions](questions.md)\n") {
		t.Errorf("index.md does not link questions.md last")
	}
	checked(t, f.root)
}

func TestPromiseEditsOnlyIndexFrontMatter(t *testing.T) {
	f := newFixture(t)
	dir := ".physa/capabilities/delivery/"
	before := snapshot(t, filepath.Join(f.root, dir))
	p := draft.Proposal{Kind: "promise", Capability: "delivery", Section: "promise",
		Signal:  draft.SignalRef{Kind: "served-not-loaded", Changes: []string{f.c1, f.c2}},
		Changes: []string{f.c1}, Entry: "Webhooks reach customers at least once, in order per endpoint."}
	applied, dropped := ApplyAll(t.Context(), f.root, f.batch, []Kept{{Proposal: p}}, f.opts())
	if len(applied) != 1 || len(dropped) != 0 {
		t.Fatalf("applied %v, dropped %v", applied, dropped)
	}
	index := filepath.Join(f.root, dir, "index.md")
	after := snapshot(t, filepath.Join(f.root, dir))
	want := strings.Replace(before[index], "promise: "+deliveryPromise+"\n", "promise: "+p.Entry+"\n", 1)
	if after[index] != want || want == before[index] {
		t.Errorf("index.md =\n%s\nwant\n%s", after[index], want)
	}
	delete(before, index)
	delete(after, index)
	sameFiles(t, before, after)
	checked(t, f.root)
}

func TestDropRestoresTheChangedKindFile(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, ".physa/capabilities/delivery")
	before := snapshot(t, dir)
	// A decision with no date: its meta line is bad.
	applied, dropped := ApplyAll(t.Context(), f.root, f.batch, []Kept{{Proposal: f.decision("decided by dana, choosing an option")}}, f.opts())
	if len(applied) != 0 || len(dropped) != 1 {
		t.Fatalf("applied %v, dropped %v", applied, dropped)
	}
	r := dropped[0].Reason
	t.Logf("dropped: %s", r)
	if !strings.Contains(r, ".physa/capabilities/delivery/decisions.md:16: ") {
		t.Errorf("the reason does not name decisions.md and the entry's meta line: %s", r)
	}
	sameFiles(t, before, snapshot(t, dir))
}

func TestDropRemovesTheKindFileItMade(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, ".physa/capabilities/delivery")
	before := snapshot(t, dir)
	// A question with no date, for a capability with no questions yet.
	q := f.question("raised by sam")
	for _, run := range []func() []Dropped{
		func() []Dropped { _, d := Check(t.Context(), f.root, f.batch, []draft.Proposal{q}, f.opts()); return d },
		func() []Dropped {
			_, d := ApplyAll(t.Context(), f.root, f.batch, []Kept{{Proposal: q}}, f.opts())
			return d
		},
	} {
		dropped := run()
		if len(dropped) != 1 {
			t.Fatalf("dropped %v", dropped)
		}
		r := dropped[0].Reason
		t.Logf("dropped: %s", r)
		if !strings.Contains(r, ".physa/capabilities/delivery/questions.md:10: ") {
			t.Errorf("the reason does not name questions.md and the entry's meta line: %s", r)
		}
		if _, err := os.Stat(filepath.Join(dir, "questions.md")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("questions.md is left behind: %v", err)
		}
		sameFiles(t, before, snapshot(t, dir))
	}
	noScratchLeft(t)
}
