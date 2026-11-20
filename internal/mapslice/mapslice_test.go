package mapslice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/mapfile"
)

// fixture is a small map: product.md with a rule, a decision and a question,
// and two capabilities, alpha and beta, each with a rule, a decision and a
// question.
func fixture() *mapfile.Map {
	entry := func(k mapfile.Kind, heading, slug, body string) mapfile.Entry {
		e := mapfile.Entry{Kind: k, Heading: heading, Slug: slug, Body: body}
		switch k {
		case mapfile.Rule:
			e.Anchors = []mapfile.Anchor{{Kind: mapfile.Doc, Value: "docs/vision.md"}}
			e.Because = "rules hold the product together."
		case mapfile.Decision:
			e.Role, e.Person, e.How, e.Date = "decided", "dana", "accepting a proposal", "2026-03-02"
			e.Anchors = []mapfile.Anchor{{Kind: mapfile.PR, Value: "12"}, {Kind: mapfile.Commit, Value: "a1b2c3d"}}
			e.Because = "it was the better trade-off."
			e.Rejected = []string{"The other way: worse.", "Doing nothing: costly."}
		case mapfile.Question:
			e.Role, e.Person, e.Date = "raised", "sam", "2026-11-01"
		}
		return e
	}
	return &mapfile.Map{
		Product: &mapfile.File{
			Path: ".physa/product.md", Name: "product", Title: "Mini",
			Promise: "Teams ship with confidence.",
			Entries: []mapfile.Entry{
				entry(mapfile.Rule, "Never log emails", "no-emails", "Never log customer emails."),
				entry(mapfile.Decision, "Go core", "go-core", "The core is Go."),
				entry(mapfile.Question, "How is it priced?", "pricing", "Postponed until the widget exists."),
			},
		},
		Capabilities: []*mapfile.File{
			{
				Path: ".physa/capabilities/alpha.md", Name: "alpha", Title: "Alpha",
				Promise: "Alpha keeps the map readable.",
				Entries: []mapfile.Entry{
					entry(mapfile.Rule, "Keep it short", "alpha-rule", "Keep every file short."),
					entry(mapfile.Decision, "One file each", "alpha-decision", "One file per capability."),
					entry(mapfile.Question, "Split big files?", "alpha-question", "Should a big file be split?"),
				},
			},
			{
				Path: ".physa/capabilities/beta.md", Name: "beta", Title: "Beta",
				Promise: "Beta answers fast.",
				Entries: []mapfile.Entry{
					entry(mapfile.Rule, "Answer fast", "beta-rule", "Answer in under 50 ms."),
					entry(mapfile.Decision, "No cache", "beta-decision", "No cache until the test fails."),
					entry(mapfile.Question, "Which store?", "beta-question", "Which store keeps the records?"),
				},
			},
			{
				Path: ".physa/capabilities/gamma.md", Name: "gamma", Title: "Gamma",
				Promise: "Gamma is never asked for.",
			},
		},
		Glossary: []mapfile.Term{
			{Name: "Map", Definition: "The product's knowledge.", Avoid: []string{"model", "graph"}},
			{Name: "Capability", Definition: "A promise the product makes.", Avoid: []string{"feature"}},
			{Name: "Widget", Definition: "A thing only a question mentions."},
			{Name: "Slice", Definition: "The part of the map for one task.", Avoid: []string{"context pack"}},
			{Name: "Cache", Definition: "A second copy kept for speed."},
		},
	}
}

func build(t *testing.T, m *mapfile.Map, r Request) *Slice {
	t.Helper()
	s, err := Build(m, r)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func md(t *testing.T, s *Slice) string {
	t.Helper()
	var b bytes.Buffer
	if err := WriteMD(&b, s); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func jsonOut(t *testing.T, s *Slice) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := WriteJSON(&b, s); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// costAt is the budget cost of the slice with the first k cuts applied.
func costAt(t *testing.T, m *mapfile.Map, r Request, k int) int {
	t.Helper()
	a, err := newAssembler(m, r)
	if err != nil {
		t.Fatal(err)
	}
	_, cost := a.assemble(k)
	return cost
}

func cutKeys(t *testing.T, m *mapfile.Map, r Request) []string {
	t.Helper()
	a, err := newAssembler(m, r)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, c := range a.cuts {
		if c.part == PartTerm {
			keys = append(keys, a.terms[c.term].Name+"/"+c.part)
			continue
		}
		keys = append(keys, a.secs[c.sec].Entries[c.ent].Slug+"/"+c.part)
	}
	return keys
}

func leftOut(s *Slice) []string {
	var got []string
	for _, l := range s.LeftOut {
		got = append(got, l.Slug+"/"+l.Part)
	}
	return got
}

func inOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(text[at:], p)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", p, text)
		}
		at += i + 1
	}
}

func TestSectionOrder(t *testing.T) {
	r := Request{Capabilities: []string{"beta", "alpha"}}
	r.Budget = costAt(t, fixture(), r, 4) // cuts product's decision and question and the rejected options, keeps the terms
	s := build(t, fixture(), r)
	out := md(t, s)
	if len(s.LeftOut) == 0 {
		t.Fatalf("budget %d should cut something:\n%s", r.Budget, out)
	}
	inOrder(t, out,
		"# Slice: alpha, beta\n",
		"Budget: ",
		"\n## Product: Mini\n",
		"\n## Alpha\n", "\nFile: .physa/capabilities/alpha.md\n", "\nPromise: Alpha keeps the map readable.\n",
		"\n### Rules\n", "\n#### Keep it short\n",
		"\n## Beta\n",
		"\n## Terms\n",
		"\n## Left out\n",
		"\n## How to anchor\n",
	)
	if strings.Contains(out, "Gamma") {
		t.Errorf("a capability that was not asked for is in the slice:\n%s", out)
	}
	if strings.Contains(out, "Current state") {
		t.Errorf("section 4 must not be built:\n%s", out)
	}
}

func TestEntryLayout(t *testing.T) {
	out := md(t, build(t, fixture(), Request{Capabilities: []string{"alpha"}}))
	want := "#### One file each\n" +
		"`alpha-decision` · decided by dana, accepting a proposal · 2026-03-02 · #12 a1b2c3d\n" +
		"One file per capability.\n" +
		"Because: it was the better trade-off.\n" +
		"Rejected:\n- The other way: worse.\n- Doing nothing: costly.\n"
	if !strings.Contains(out, want) {
		t.Errorf("decision not rendered as\n%s\nin:\n%s", want, out)
	}
	if !strings.Contains(out, "`alpha-rule` · from docs/vision.md\n") {
		t.Errorf("rule meta line missing:\n%s", out)
	}
	if !strings.Contains(out, "`alpha-question` · raised by sam · 2026-11-01\n") {
		t.Errorf("question meta line missing:\n%s", out)
	}
	inOrder(t, out, "### Rules", "### Decisions", "### Questions", "## Terms")
	if s := build(t, fixture(), Request{Capabilities: []string{"alpha"}}); len(s.LeftOut) != 0 || strings.Contains(out, "## Left out") {
		t.Errorf("nothing should be cut without a budget:\n%s", out)
	}
}

func TestMetaAnchorsMixed(t *testing.T) {
	e := mapfile.Entry{Kind: mapfile.Rule, Slug: "x", Anchors: []mapfile.Anchor{
		{Kind: mapfile.PR, Value: "1"}, {Kind: mapfile.Commit, Value: "abcdef0"}, {Kind: mapfile.Doc, Value: "docs/a.md"},
	}}
	got := metaLine(toEntry(e))
	if want := "`x` · #1 abcdef0 · from docs/a.md"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCutOrder(t *testing.T) {
	got := cutKeys(t, fixture(), Request{Capabilities: []string{"alpha", "beta"}})
	want := []string{
		"pricing/entry", "go-core/entry",
		"beta-decision/rejected", "alpha-decision/rejected",
		"Cache/term", "Slice/term", "Widget/term", "Capability/term", "Map/term",
		"beta-question/entry", "alpha-question/entry",
		"beta-decision/because", "alpha-decision/because",
		"beta-decision/entry", "alpha-decision/entry",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cut order:\n got %v\nwant %v", got, want)
	}
}

func TestCutTiers(t *testing.T) {
	m := fixture()
	r := Request{Capabilities: []string{"alpha", "beta"}}
	cases := []struct {
		k    int
		want []string
	}{
		{1, []string{"pricing/entry"}},
		{2, []string{"go-core/entry", "pricing/entry"}},
		{4, []string{"go-core/entry", "pricing/entry", "alpha-decision/rejected", "beta-decision/rejected"}},
		{5, []string{"go-core/entry", "pricing/entry", "alpha-decision/rejected", "beta-decision/rejected", "Cache/term"}},
		{9, []string{"go-core/entry", "pricing/entry", "alpha-decision/rejected", "beta-decision/rejected", "Map/term", "Capability/term", "Cache/term"}},
		{11, []string{"go-core/entry", "pricing/entry", "alpha-decision/rejected", "alpha-question/entry", "beta-decision/rejected", "beta-question/entry", "Map/term", "Capability/term", "Cache/term"}},
		{13, []string{"go-core/entry", "pricing/entry", "alpha-decision/because", "alpha-decision/rejected", "alpha-question/entry", "beta-decision/because", "beta-decision/rejected", "beta-question/entry", "Map/term", "Capability/term", "Cache/term"}},
		// Cutting the decisions takes the only text that names Capability and
		// Cache, so those terms are no longer left out: they are not there.
		{15, []string{"go-core/entry", "pricing/entry", "alpha-decision/entry", "alpha-question/entry", "beta-decision/entry", "beta-question/entry", "Map/term"}},
	}
	for _, c := range cases {
		r.Budget = costAt(t, m, r, c.k)
		if prev := costAt(t, m, r, c.k-1); prev <= r.Budget {
			t.Fatalf("k=%d: cost does not fall (%d then %d); fixture too weak", c.k, prev, r.Budget)
		}
		s := build(t, m, r)
		if got := leftOut(s); !reflect.DeepEqual(got, c.want) {
			t.Errorf("budget %d (k=%d): left out\n got %v\nwant %v", r.Budget, c.k, got, c.want)
		}
		if s.OverBudget || s.Used > s.Budget {
			t.Errorf("k=%d: used %d of %d, over %v", c.k, s.Used, s.Budget, s.OverBudget)
		}
	}
}

func TestCutsAreRemovedFromOutput(t *testing.T) {
	m := fixture()
	r := Request{Capabilities: []string{"alpha", "beta"}}
	r.Budget = costAt(t, m, r, 4)
	out := md(t, build(t, m, r))
	for _, gone := range []string{"Rejected:", "`pricing` ·", "`go-core` ·"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should be cut:\n%s", gone, out)
		}
	}
	for _, kept := range []string{"`no-emails` ·", "alpha-decision` · decided by dana", "One file per capability.\nBecause: it was the better trade-off.\n", "`alpha-question` ·", "**Cache**"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q should be kept:\n%s", kept, out)
		}
	}
}

func TestOverBudgetFloor(t *testing.T) {
	s := build(t, fixture(), Request{Capabilities: []string{"alpha", "beta"}, Budget: 10})
	out := md(t, s)
	if !s.OverBudget {
		t.Fatalf("want over budget:\n%s", out)
	}
	want := fmt.Sprintf("Over budget: what is never cut (product rules, promises and rules) is %d tokens, over the 10-token budget.\n", s.Used)
	if !strings.HasPrefix(out, fmt.Sprintf("# Slice: alpha, beta\nBudget: %d of 10 tokens.\n%s", s.Used, want)) {
		t.Errorf("header wrong, want %q in:\n%s", want, out)
	}
	for _, kept := range []string{
		"Promise: Teams ship with confidence.", "Never log customer emails.\nBecause: rules hold the product together.",
		"Promise: Alpha keeps the map readable.", "Keep every file short.",
		"Promise: Beta answers fast.", "Answer in under 50 ms.",
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("floor lost %q:\n%s", kept, out)
		}
	}
	for _, gone := range []string{"### Decisions", "### Questions", "## Terms"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should be cut over budget:\n%s", gone, out)
		}
	}
	if got := leftOut(s); len(got) != 7 || got[6] != "Map/term" {
		t.Errorf("want every decision, question and term left out, got %v", got)
	}
}

func TestWithDecisionsKeepsThem(t *testing.T) {
	s := build(t, fixture(), Request{Capabilities: []string{"alpha"}, WithDecisions: true, Budget: 10})
	out := md(t, s)
	for _, kept := range []string{"`go-core`", "The core is Go.\nBecause: it was the better trade-off.\nRejected:", "`alpha-decision`", "One file per capability.\nBecause:"} {
		if !strings.Contains(out, kept) {
			t.Errorf("--with decisions lost %q:\n%s", kept, out)
		}
	}
	if got, want := leftOut(s), []string{"pricing/entry", "alpha-question/entry", "Map/term", "Capability/term"}; !reflect.DeepEqual(got, want) {
		t.Errorf("left out %v, want %v", got, want)
	}
	for _, l := range s.LeftOut {
		if l.Command != "physa map slice alpha" {
			t.Errorf("command %q", l.Command)
		}
	}
	if !strings.Contains(out, "Over budget: what is never cut (product rules, promises, rules and decisions) is ") {
		t.Errorf("over-budget line should name the decisions:\n%s", out)
	}
}

func TestWithQuestionsKeepsThem(t *testing.T) {
	s := build(t, fixture(), Request{Capabilities: []string{"alpha"}, WithQuestions: true, Budget: 10})
	out := md(t, s)
	for _, kept := range []string{"`pricing`", "Postponed until the widget exists.", "`alpha-question`"} {
		if !strings.Contains(out, kept) {
			t.Errorf("--with questions lost %q:\n%s", kept, out)
		}
	}
	if got, want := leftOut(s), []string{"go-core/entry", "alpha-decision/entry", "Map/term"}; !reflect.DeepEqual(got, want) {
		t.Errorf("left out %v, want %v", got, want)
	}
}

func TestWithBothCutsOnlyTerms(t *testing.T) {
	s := build(t, fixture(), Request{Capabilities: []string{"alpha"}, WithDecisions: true, WithQuestions: true, Budget: 10})
	if got, want := leftOut(s), []string{"Map/term", "Capability/term"}; !reflect.DeepEqual(got, want) || !s.OverBudget {
		t.Errorf("left out %v, want %v; over %v", got, want, s.OverBudget)
	}
}

func termNames(s *Slice) []string {
	var got []string
	for _, t := range s.Terms {
		got = append(got, t.Name)
	}
	return got
}

func TestTermsMatch(t *testing.T) {
	s := build(t, fixture(), Request{Capabilities: []string{"alpha", "beta"}})
	// Map: "map" in Alpha's promise; Capability: in a body; Cache: "No cache"
	// heading. Widget is only in product.md, which never brings in a term, and
	// Slice is not in the text.
	if got, want := termNames(s), []string{"Map", "Capability", "Cache"}; !reflect.DeepEqual(got, want) {
		t.Errorf("terms %v, want %v", got, want)
	}
	out := md(t, s)
	inOrder(t, out, "## Terms\n\n**Map**: The product's knowledge.\n_Avoid_: model, graph\n\n**Capability**: A promise the product makes.\n_Avoid_: feature\n\n**Cache**")
}

func TestTermsByAvoidAndWholeWord(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"Load the CONTEXT PACK first.", []string{"Slice"}},
		{"The context\npack is gone.", []string{"Slice"}},
		{"A Feature flag.", []string{"Capability"}},
		{"The roadmap and mapping and maps.", nil},
		{"Cached features, contextpack.", nil},
		{"(map) and map's and map-reader.", []string{"Map"}},
		{"graph", []string{"Map"}},
	}
	for _, c := range cases {
		m := &mapfile.Map{
			Capabilities: []*mapfile.File{{Path: ".physa/capabilities/x.md", Name: "x", Title: "X", Promise: "P.",
				Entries: []mapfile.Entry{{Kind: mapfile.Rule, Heading: "R", Slug: "r", Body: c.text}}}},
			Glossary: fixture().Glossary,
		}
		if got := termNames(build(t, m, Request{Capabilities: []string{"x"}})); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: terms %v, want %v", c.text, got, c.want)
		}
	}
}

func TestTermsRematchedAfterCuts(t *testing.T) {
	m := fixture()
	r := Request{Capabilities: []string{"alpha"}}
	if got := termNames(build(t, m, r)); !reflect.DeepEqual(got, []string{"Map", "Capability"}) {
		t.Fatalf("terms before cuts: %v", got)
	}
	r.Budget = 10
	// Capability is only in the decision, which is cut too, so it is not
	// left out: it is no longer in the text. Map, in the promise, is.
	if got, want := leftOut(build(t, m, r)), []string{"go-core/entry", "pricing/entry", "alpha-decision/entry", "alpha-question/entry", "Map/term"}; !reflect.DeepEqual(got, want) {
		t.Errorf("left out %v, want %v", got, want)
	}
}

func TestLeftOutLines(t *testing.T) {
	m := fixture()
	r := Request{Capabilities: []string{"beta", "alpha"}}
	r.Budget = costAt(t, m, r, 5)
	s := build(t, m, r)
	out := md(t, s)
	want := "## Left out\n\n" +
		"- `go-core` (product): the entry. Show it with `physa map slice alpha beta`.\n" +
		"- `pricing` (product): the entry. Show it with `physa map slice alpha beta`.\n" +
		"- `alpha-decision` (alpha): its rejected options. Show it with `physa map slice alpha beta`.\n" +
		"- `beta-decision` (beta): its rejected options. Show it with `physa map slice alpha beta`.\n" +
		"- `Cache` (glossary): the term. Show it with `physa map slice alpha beta`.\n\n"
	if !strings.Contains(out, want) {
		t.Errorf("want\n%s\nin:\n%s", want, out)
	}
	if s.LeftOut[0].Capability != "product" || s.LeftOut[2].Capability != "alpha" || s.LeftOut[4].Capability != "glossary" {
		t.Errorf("capabilities: %+v", s.LeftOut)
	}
}

func TestTrailers(t *testing.T) {
	s := build(t, fixture(), Request{Capabilities: []string{"beta", "alpha", "beta"}})
	if want := []string{"Capability: alpha", "Capability: beta"}; !reflect.DeepEqual(s.Trailers, want) {
		t.Errorf("trailers %v, want %v", s.Trailers, want)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(s.Capabilities, want) {
		t.Errorf("capabilities %v, want %v", s.Capabilities, want)
	}
	out := md(t, s)
	want := "## How to anchor\n\nAnchor each commit to the one capability it serves, with a trailer:\n\n- `Capability: alpha`\n- `Capability: beta`\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("want suffix\n%s\ngot:\n%s", want, out)
	}
}

func TestUnknownCapability(t *testing.T) {
	_, err := Build(fixture(), Request{Capabilities: []string{"alpha", "x"}})
	if err == nil || err.Error() != `unknown capability "x"; run physa map index to see them` {
		t.Errorf("got %v", err)
	}
	if _, err := Build(fixture(), Request{Capabilities: []string{"product"}}); err == nil {
		t.Errorf("product.md is not a capability")
	}
}

func TestUnknownNoCapabilities(t *testing.T) {
	if _, err := Build(fixture(), Request{}); err == nil {
		t.Errorf("want an error for no capabilities")
	}
	if _, err := Build(fixture(), Request{Capabilities: []string{"alpha"}, Budget: -1}); err == nil {
		t.Errorf("want an error for a negative budget")
	}
	if _, err := Build(nil, Request{Capabilities: []string{"alpha"}}); err == nil {
		t.Errorf("want an error for no map")
	}
}

func TestNoBudgetCutsNothing(t *testing.T) {
	m := fixture()
	r := Request{Capabilities: []string{"alpha", "beta"}}
	s := build(t, m, r)
	out := md(t, s)
	if s.Budget != 0 || s.OverBudget || len(s.LeftOut) != 0 || strings.Contains(out, "## Left out") {
		t.Errorf("budget %d, over %v, left out %v:\n%s", s.Budget, s.OverBudget, leftOut(s), out)
	}
	if want := fmt.Sprintf("# Slice: alpha, beta\nTokens: %d, no budget.\n\n", s.Used); !strings.HasPrefix(out, want) {
		t.Errorf("want header %q in:\n%s", want, out)
	}
	if s.Used != costAt(t, m, r, 0) {
		t.Errorf("used %d, want the uncut cost %d", s.Used, costAt(t, m, r, 0))
	}
	for _, kept := range []string{"`go-core` ·", "`pricing` ·", "Rejected:", "`alpha-question` ·", "`beta-decision` ·"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q should be in an uncut slice:\n%s", kept, out)
		}
	}
}

func TestUsedCountsBodyOnly(t *testing.T) {
	m := fixture()
	r := Request{Capabilities: []string{"alpha", "beta"}, Budget: 200}
	s := build(t, m, r)
	out := md(t, s)
	start := strings.Index(out, "\n## Product")
	end := strings.Index(out, "\n## Left out")
	if start < 0 || end < 0 {
		t.Fatalf("sections missing:\n%s", out)
	}
	body := out[start+1 : end]
	if want := (len(body) + 3) / 4; s.Used != want {
		t.Errorf("used %d, want ceil(%d/4) = %d", s.Used, len(body), want)
	}
}

func TestProductMissing(t *testing.T) {
	m := fixture()
	m.Product = nil
	out := md(t, build(t, m, Request{Capabilities: []string{"alpha"}, Budget: 60}))
	if strings.Contains(out, "## Product") {
		t.Errorf("no product section expected:\n%s", out)
	}
}

func TestDeterminism(t *testing.T) {
	r := Request{Capabilities: []string{"beta", "alpha"}, Budget: 200}
	a, b := build(t, fixture(), r), build(t, fixture(), r)
	if md(t, a) != md(t, b) || md(t, a) != md(t, a) {
		t.Errorf("markdown differs between runs")
	}
	if !bytes.Equal(jsonOut(t, a), jsonOut(t, b)) || !bytes.Equal(jsonOut(t, a), jsonOut(t, a)) {
		t.Errorf("JSON differs between runs")
	}
}

var slugLine = regexp.MustCompile("(?m)^`([a-z0-9-]+)` · ")

func TestJSONRoundTrip(t *testing.T) {
	s := build(t, fixture(), Request{Capabilities: []string{"alpha", "beta"}, Budget: 220})
	raw := jsonOut(t, s)
	var back Slice
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	if again := jsonOut(t, &back); !bytes.Equal(raw, again) {
		t.Errorf("JSON does not round-trip:\n%s\n%s", raw, again)
	}
	if md(t, &back) != md(t, s) {
		t.Errorf("markdown from decoded JSON differs")
	}
	var fromJSON []string
	for _, e := range back.Product.Entries {
		fromJSON = append(fromJSON, e.Slug)
	}
	for _, c := range back.Sections {
		for _, e := range c.Entries {
			fromJSON = append(fromJSON, e.Slug)
		}
	}
	var fromMD []string
	for _, m := range slugLine.FindAllStringSubmatch(md(t, s), -1) {
		fromMD = append(fromMD, m[1])
	}
	if !reflect.DeepEqual(fromJSON, fromMD) {
		t.Errorf("entries differ: json %v, md %v", fromJSON, fromMD)
	}
	for _, key := range []string{`"capabilities":`, `"budget":`, `"used":`, `"over_budget":`, `"product":`, `"sections":`, `"terms":`, `"left_out":`, `"trailers":`} {
		if !bytes.Contains(raw, []byte(key)) {
			t.Errorf("JSON lacks %s", key)
		}
	}
	inOrder(t, string(raw), `"capabilities":`, `"budget":`, `"used":`, `"over_budget":`, `"product":`, `"sections":`, `"terms":`, `"left_out":`, `"trailers":`)
}

func TestJSONOmitsCutParts(t *testing.T) {
	m := fixture()
	r := Request{Capabilities: []string{"alpha"}}
	r.Budget = costAt(t, m, r, 3) // product's decision and question go, and alpha-decision loses its rejected options
	raw := string(jsonOut(t, build(t, m, r)))
	if strings.Contains(raw, `"rejected":`) {
		t.Errorf("cut rejected options are in the JSON:\n%s", raw)
	}
	if !strings.Contains(raw, `"because":"it was the better trade-off."`) {
		t.Errorf("because lost:\n%s", raw)
	}
}

func TestOwnMap(t *testing.T) {
	m, _, err := mapfile.Read(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	s := build(t, m, Request{Capabilities: []string{"slices", "map"}})
	out := md(t, s)
	inOrder(t, out, "# Slice: map, slices\n", "## Product: Physa", "`no-adr`", "## Map\n", "## Slices\n",
		"Everything in a slice except its current-state section depends only on the map and the request.",
		"## Terms\n", "**Slice**", "## How to anchor", "`Capability: map`", "`Capability: slices`")
}

func put(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func capability(n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\npromise: Capability %03d keeps its word to the people who rely on it.\nstewards: [dana]\n---\n\n# Cap %03d\n\n## Rules\n\n", n, n)
	for i := 1; i <= 4; i++ {
		fmt.Fprintf(&b, "### Rule %d\n`cap-%03d-rule-%d` · #%d\nHold this.\nBecause: it matters.\n\n", i, n, i, n)
	}
	b.WriteString("## Decisions\n\n")
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&b, "### Decision %d\n`cap-%03d-decision-%d` · decided by dana, accepting a proposal · 2026-03-02 · #%d\nWe chose this.\nBecause: it was needed.\nRejected:\n- Other way: worse.\n\n", i, n, i, n)
	}
	fmt.Fprintf(&b, "## Questions\n\n### Open question?\n`cap-%03d-question` · raised by sam · 2026-11-01\n", n)
	return b.String()
}

func glossary() string {
	var b strings.Builder
	b.WriteString("# Glossary\n\n**Map**: The product's knowledge.\n_Avoid_: model\n\n")
	for i := 1; i < 40; i++ {
		fmt.Fprintf(&b, "**Word%02d**: A term the map may use.\n_Avoid_: synonym%02d, other phrase %02d\n\n", i, i, i)
	}
	return b.String()
}

func TestLatency(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".physa/README.md", "# Map\n\nRead product.md first.\n")
	put(t, root, ".physa/glossary.md", glossary())
	put(t, root, ".physa/product.md", "---\npromise: Teams ship with confidence.\n---\n\n# Mini\n\n## Rules\n\n### Never log emails\n`no-emails` · from docs/vision.md\nNever log customer emails.\nBecause: customers trust us.\n")
	var all []string
	for n := 1; n <= 100; n++ {
		put(t, root, fmt.Sprintf(".physa/capabilities/cap-%03d.md", n), capability(n))
		all = append(all, fmt.Sprintf("cap-%03d", n))
	}
	for _, c := range []struct {
		name   string
		caps   []string
		budget int
	}{
		{"three", []string{"cap-007", "cap-042", "cap-099"}, 0},
		{"all", all, 0},
		{"all at 3000", all, 3000},
	} {
		// The best of three runs, so one descheduling on a busy machine
		// does not fail the test; each run must still be whole.
		var (
			elapsed  time.Duration
			s        *Slice
			m        *mapfile.Map
			problems []mapfile.Problem
		)
		for try := range 3 {
			start := time.Now()
			var err error
			m, problems, err = mapfile.Read(root)
			if err != nil {
				t.Fatal(err)
			}
			s, err = Build(m, Request{Capabilities: c.caps, Budget: c.budget})
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteMD(io.Discard, s); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(start); try == 0 || d < elapsed {
				elapsed = d
			}
		}
		if len(problems) != 0 {
			t.Fatalf("generated map has problems: %v", problems)
		}
		if len(m.Glossary) != 40 || len(s.Sections) != len(c.caps) {
			t.Fatalf("%s: %d terms, %d capabilities", c.name, len(m.Glossary), len(s.Sections))
		}
		if elapsed >= 50*time.Millisecond {
			t.Errorf("%s: read, build and write took %v, want under 50ms", c.name, elapsed)
		}
		t.Logf("%s: %v, used %d of %d, %d left out", c.name, elapsed, s.Used, s.Budget, len(s.LeftOut))
	}
}

func TestTermsPhraseStartingWithPunctuation(t *testing.T) {
	terms := []mapfile.Term{{Name: "Tag", Avoid: []string{"#label"}}, {Name: "Flag", Avoid: []string{"A/B test"}}}
	cases := map[string][]string{
		"Use a #label here.":    {"Tag"},
		"Use a x#label here.":   nil,
		"Use a #labels here.":   nil,
		"Run an a/b  test now.": {"Flag"},
	}
	for text, want := range cases {
		m := &mapfile.Map{
			Capabilities: []*mapfile.File{{Name: "x", Title: "X", Entries: []mapfile.Entry{{Kind: mapfile.Rule, Slug: "r", Body: text}}}},
			Glossary:     terms,
		}
		if got := termNames(build(t, m, Request{Capabilities: []string{"x"}})); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: terms %v, want %v", text, got, want)
		}
	}
}
