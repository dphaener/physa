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
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
)

// fixture is a small map: the product with a rule, a decision and a question,
// and capabilities alpha and beta, each with a rule, a decision and a
// question, and gamma, which no test asks for.
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
			Path: ".physa/product", Name: "product", Title: "Mini",
			Promise:  "Teams ship with confidence.",
			Overview: "Mini is a feature-rich widget.",
			Entries: []mapfile.Entry{
				entry(mapfile.Rule, "Never log emails", "no-emails", "Never log customer emails."),
				entry(mapfile.Decision, "Go core", "go-core", "The core is Go."),
				entry(mapfile.Question, "How is it priced?", "pricing", "Postponed until the widget exists."),
			},
		},
		Capabilities: []*mapfile.File{
			{
				Path: ".physa/capabilities/alpha", Name: "alpha", Title: "Alpha",
				Promise:  "Alpha keeps the map readable.",
				Overview: "Alpha covers how files are laid out.\nIt reads every folder.",
				Entries: []mapfile.Entry{
					entry(mapfile.Rule, "Keep it short", "alpha-rule", "Keep every file short."),
					entry(mapfile.Decision, "One file each", "alpha-decision", "One file per capability."),
					entry(mapfile.Question, "Split big files?", "alpha-question", "Should a big file be split?"),
				},
			},
			{
				Path: ".physa/capabilities/beta", Name: "beta", Title: "Beta",
				Promise:  "Beta answers fast.",
				Overview: "Beta builds the context pack.",
				Entries: []mapfile.Entry{
					entry(mapfile.Rule, "Answer fast", "beta-rule", "Answer in under 50 ms."),
					entry(mapfile.Decision, "No cache", "beta-decision", "No cache until the test fails."),
					entry(mapfile.Question, "Which store?", "beta-question", "Which store keeps the records?"),
				},
			},
			{
				Path: ".physa/capabilities/gamma", Name: "gamma", Title: "Gamma",
				Promise: "Gamma is never asked for.",
			},
		},
		Glossary: []mapfile.Term{
			{Name: "Map", Definition: "The product's knowledge.", Avoid: []string{"model", "graph"}},
			{Name: "Capability", Definition: "A promise the product makes.", Avoid: []string{"feature"}},
			{Name: "Widget", Definition: "A thing only the product mentions."},
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

func take(t *testing.T, m *mapfile.Map, slugs ...string) *Taken {
	t.Helper()
	tk, err := Take(m, slugs)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func takenMD(t *testing.T, tk *Taken) string {
	t.Helper()
	var b bytes.Buffer
	if err := WriteTakenMD(&b, tk); err != nil {
		t.Fatal(err)
	}
	return b.String()
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

const (
	decisionsLine = "Decisions (take with physa map slice --entry <slug>...)\n"
	questionsLine = "Questions (take with physa map slice --entry <slug>...)\n"
)

func TestSectionOrder(t *testing.T) {
	out := md(t, build(t, fixture(), Request{Capabilities: []string{"beta", "alpha"}}))
	inOrder(t, out,
		"# Slice: alpha, beta\n\n## Product: Mini\n",
		"\nFile: .physa/product/\nPromise: Teams ship with confidence.\n",
		"\n### Rules\n\n#### Never log emails\n",
		"\n"+decisionsLine+"- go-core: Go core\n",
		"\n"+questionsLine+"- pricing: How is it priced?\n",
		"\n## Alpha\nFile: .physa/capabilities/alpha/\nPromise: Alpha keeps the map readable.\n\nAlpha covers how files are laid out.\nIt reads every folder.\n",
		"\n### Rules\n\n#### Keep it short\n",
		"\n"+decisionsLine, "\n"+questionsLine,
		"\n## Beta\n",
		"\n## Terms\n",
		"\n## Reading further\n\n",
		"\n## How to anchor\n",
	)
	for _, gone := range []string{"Gamma", "Current state", "Tokens:", "Mini is a feature-rich widget."} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should not be in the slice:\n%s", gone, out)
		}
	}
}

func TestHeadingLines(t *testing.T) {
	out := md(t, build(t, fixture(), Request{Capabilities: []string{"alpha"}}))
	want := "\n" + decisionsLine + "- alpha-decision: One file each\n\n" + questionsLine + "- alpha-question: Split big files?\n\n## Terms\n"
	if !strings.Contains(out, want) {
		t.Errorf("want\n%s\nin:\n%s", want, out)
	}
	body := out[:strings.Index(out, "\n## Reading further\n")] // the skill names Rejected: lists
	for _, part := range []string{"The core is Go.", "Postponed until", "One file per capability.", "Should a big file be split?", "Rejected:", "`alpha-decision` ·", "`pricing` ·", "### Decisions", "### Questions"} {
		if strings.Contains(body, part) {
			t.Errorf("a bare slice holds %q:\n%s", part, out)
		}
	}
	// Heading lines keep the file's order; they are never sorted.
	m := fixture()
	a := m.Capabilities[0]
	a.Entries = append(a.Entries, mapfile.Entry{Kind: mapfile.Decision, Heading: "Aardvark first", Slug: "aardvark"})
	if out := md(t, build(t, m, Request{Capabilities: []string{"alpha"}})); !strings.Contains(out, "- alpha-decision: One file each\n- aardvark: Aardvark first\n") {
		t.Errorf("heading lines out of file order:\n%s", out)
	}
}

func TestWithKinds(t *testing.T) {
	m := fixture()
	out := md(t, build(t, m, Request{Capabilities: []string{"alpha"}, WithDecisions: true}))
	inOrder(t, out, "## Product: Mini", "### Decisions\n\n#### Go core\n`go-core` · decided by dana", "The core is Go.", questionsLine+"- pricing: How is it priced?\n",
		"## Alpha", "### Decisions\n\n#### One file each\n", questionsLine+"- alpha-question: Split big files?\n")
	if strings.Contains(out, decisionsLine) {
		t.Errorf("decisions in full and as headings:\n%s", out)
	}
	out = md(t, build(t, m, Request{Capabilities: []string{"alpha"}, WithQuestions: true}))
	inOrder(t, out, "### Questions\n\n#### How is it priced?\n", "Postponed until the widget exists.", "### Questions\n\n#### Split big files?\n")
	if !strings.Contains(out, decisionsLine+"- alpha-decision: One file each\n") || strings.Contains(out, questionsLine) {
		t.Errorf("--with questions keeps decisions as headings only:\n%s", out)
	}
	s := build(t, m, Request{Capabilities: []string{"alpha"}, WithDecisions: true, WithQuestions: true})
	out = md(t, s)
	if strings.Contains(out, "(take with") {
		t.Errorf("--with all leaves heading lines:\n%s", out)
	}
	if len(s.Sections[0].Contents) != 0 || len(s.Sections[0].Entries) != 3 || len(s.Product.Entries) != 3 {
		t.Errorf("--with all: %+v", s.Sections[0])
	}
	if !strings.Contains(out, "\n## Reading further\n") {
		t.Errorf("--with all lacks the skill:\n%s", out)
	}
}

func TestEntryLayout(t *testing.T) {
	out := md(t, build(t, fixture(), Request{Capabilities: []string{"alpha"}, WithDecisions: true, WithQuestions: true}))
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

func termNames(s *Slice) []string {
	var got []string
	for _, t := range s.Terms {
		got = append(got, t.Name)
	}
	return got
}

func TestTermsFromHeadings(t *testing.T) {
	m := fixture()
	s := build(t, m, Request{Capabilities: []string{"alpha", "beta"}})
	// Map: Alpha's promise; Slice: "context pack" in Beta's overview; Cache:
	// the "No cache" heading. Capability is only in a decision's body, which
	// a bare slice does not show, and Widget and Capability ("feature") only
	// in the product, which never brings in a term.
	if got, want := termNames(s), []string{"Map", "Slice", "Cache"}; !reflect.DeepEqual(got, want) {
		t.Errorf("terms %v, want %v", got, want)
	}
	inOrder(t, md(t, s), "## Terms\n\n**Map**: The product's knowledge.\n_Avoid_: model, graph\n\n**Slice**: The part of the map for one task.\n_Avoid_: context pack\n\n**Cache**")
	full := build(t, m, Request{Capabilities: []string{"alpha", "beta"}, WithDecisions: true, WithQuestions: true})
	if got, want := termNames(full), []string{"Map", "Capability", "Slice", "Cache"}; !reflect.DeepEqual(got, want) {
		t.Errorf("--with all: terms %v, want %v", got, want)
	}
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
			Capabilities: []*mapfile.File{{Path: ".physa/capabilities/x", Name: "x", Title: "X", Promise: "P.",
				Entries: []mapfile.Entry{{Kind: mapfile.Rule, Heading: "R", Slug: "r", Body: c.text}}}},
			Glossary: fixture().Glossary,
		}
		if got := termNames(build(t, m, Request{Capabilities: []string{"x"}})); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: terms %v, want %v", c.text, got, c.want)
		}
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

func TestSkillInEverySlice(t *testing.T) {
	if n := len(skill); n == 0 || n > 850 {
		t.Fatalf("the skill is %d bytes; want about 800 at most", n)
	}
	for _, want := range []string{"physa map slice --entry <slug>", "Rejected:", "stop and ask", "Capability:"} {
		if !strings.Contains(skill, want) {
			t.Errorf("the skill lacks %q", want)
		}
	}
	section := "\n## Reading further\n\n" + strings.TrimRight(skill, "\n") + "\n\n## How to anchor\n"
	for _, r := range []Request{
		{Capabilities: []string{"alpha"}},
		{Capabilities: []string{"alpha", "beta"}, WithDecisions: true},
		{Capabilities: []string{"beta"}, WithDecisions: true, WithQuestions: true},
	} {
		s := build(t, fixture(), r)
		if out := md(t, s); !strings.Contains(out, section) {
			t.Errorf("%+v: no Reading further section before how to anchor:\n%s", r, out)
		}
		var back struct {
			ReadingFurther string `json:"reading_further"`
		}
		if err := json.Unmarshal(jsonOut(t, s), &back); err != nil || back.ReadingFurther != skill {
			t.Errorf("%+v: reading_further %q, %v", r, back.ReadingFurther, err)
		}
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
		t.Errorf("the product is not a capability")
	}
}

func TestBadRequests(t *testing.T) {
	if _, err := Build(fixture(), Request{}); err == nil {
		t.Errorf("want an error for no capabilities")
	}
	if _, err := Build(nil, Request{Capabilities: []string{"alpha"}}); err == nil {
		t.Errorf("want an error for no map")
	}
	if _, err := Build(fixture(), Request{Capabilities: []string{"alpha"}, Entries: []string{"go-core"}}); err == nil {
		t.Errorf("want an error for capabilities and entries both")
	}
	if _, err := Take(fixture(), nil); err == nil {
		t.Errorf("want an error for no entries")
	}
	if _, err := Take(nil, []string{"go-core"}); err == nil {
		t.Errorf("want an error for no map")
	}
}

func TestProductMissing(t *testing.T) {
	m := fixture()
	m.Product = nil
	s := build(t, m, Request{Capabilities: []string{"alpha"}})
	if out := md(t, s); strings.Contains(out, "## Product") || !strings.HasPrefix(out, "# Slice: alpha\n\n## Alpha\n") {
		t.Errorf("no product section expected:\n%s", out)
	}
	if bytes.Contains(jsonOut(t, s), []byte(`"product":`)) {
		t.Errorf("no product in the JSON expected")
	}
}

func TestDeterminism(t *testing.T) {
	r := Request{Capabilities: []string{"beta", "alpha"}, WithQuestions: true}
	a, b := build(t, fixture(), r), build(t, fixture(), r)
	if md(t, a) != md(t, b) || md(t, a) != md(t, a) {
		t.Errorf("markdown differs between runs")
	}
	if !bytes.Equal(jsonOut(t, a), jsonOut(t, b)) {
		t.Errorf("JSON differs between runs")
	}
	slugs := []string{"pricing", "beta-rule", "alpha-decision"}
	if takenMD(t, take(t, fixture(), slugs...)) != takenMD(t, take(t, fixture(), slugs...)) {
		t.Errorf("taken entries differ between runs")
	}
}

var slugLine = regexp.MustCompile("(?m)^`([a-z0-9-]+)` · ")

func TestJSONFields(t *testing.T) {
	s := build(t, fixture(), Request{Capabilities: []string{"alpha", "beta"}, WithDecisions: true})
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
	for _, sec := range append([]Section{*back.Product}, back.Sections...) {
		for _, e := range sec.Entries {
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
	inOrder(t, string(raw), `{"capabilities":["alpha","beta"],"product":{`, `"sections":[`, `"terms":[`, `"reading_further":"`, `"trailers":["Capability: alpha","Capability: beta"]}`)
	inOrder(t, string(raw), `"sections":[{"name":"alpha","title":"Alpha","file":".physa/capabilities/alpha/","promise":"Alpha keeps the map readable.","overview":"Alpha covers how files are laid out.\nIt reads every folder.","entries":[{"slug":"alpha-rule"`,
		`"contents":[{"kind":"question","slug":"alpha-question","heading":"Split big files?"}]}`)
	if !bytes.Contains(raw, []byte(`"product":{"name":"product","title":"Mini","file":".physa/product/","promise":"Teams ship with confidence.","overview":"","entries":[`)) {
		t.Errorf("product section:\n%s", raw)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	var got []string
	for k := range keys {
		got = append(got, k)
	}
	slices.Sort(got)
	if want := []string{"capabilities", "product", "reading_further", "sections", "terms", "trailers"}; !reflect.DeepEqual(got, want) {
		t.Errorf("JSON fields %v, want %v", got, want)
	}
}

func TestTakeOrderAndDedupe(t *testing.T) {
	tk := take(t, fixture(), "beta-decision", "no-emails", "alpha-question", "beta-decision")
	var got []string
	for _, e := range tk.Entries {
		got = append(got, e.Capability+"/"+e.Slug)
	}
	if want := []string{"beta/beta-decision", "product/no-emails", "alpha/alpha-question"}; !reflect.DeepEqual(got, want) {
		t.Errorf("taken %v, want %v", got, want)
	}
	want := "### No cache\n" +
		"`beta-decision` · decided by dana, accepting a proposal · 2026-03-02 · #12 a1b2c3d\n" +
		"No cache until the test fails.\n" +
		"Because: it was the better trade-off.\n" +
		"Rejected:\n- The other way: worse.\n- Doing nothing: costly.\n" +
		"\n### Never log emails\n" +
		"`no-emails` · from docs/vision.md\n" +
		"Never log customer emails.\n" +
		"Because: rules hold the product together.\n" +
		"\n### Split big files?\n" +
		"`alpha-question` · raised by sam · 2026-11-01\n" +
		"Should a big file be split?\n"
	if out := takenMD(t, tk); out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestTakeProductEntries(t *testing.T) {
	tk := take(t, fixture(), "pricing", "go-core")
	if len(tk.Entries) != 2 || tk.Entries[0].Capability != "product" || tk.Entries[1].Body != "The core is Go." {
		t.Errorf("got %+v", tk.Entries)
	}
}

func TestTakeUnknownSlugs(t *testing.T) {
	tk, err := Take(fixture(), []string{"alpha-rule", "nope", "go-core", "also-nope", "nope"})
	if tk != nil {
		t.Errorf("want nothing taken, got %+v", tk)
	}
	want := `unknown entries "nope", "also-nope"; a slice lists the slugs of its capabilities' decisions and questions`
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %s", err, want)
	}
	t.Logf("error: %v", err)
	if _, err := Take(fixture(), []string{"nope"}); err == nil || !strings.HasPrefix(err.Error(), `unknown entry "nope";`) {
		t.Errorf("one unknown slug: %v", err)
	}
	var out bytes.Buffer
	if err := Write(&out, fixture(), Flags{Req: Request{Entries: []string{"go-core", "nope"}}, JSON: true}); err == nil || out.Len() != 0 {
		t.Errorf("want an error and no output, got %v and %q", err, out.String())
	}
}

func TestTakeJSON(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, fixture(), Flags{Req: Request{Entries: []string{"beta-rule", "pricing"}}, JSON: true}); err != nil {
		t.Fatal(err)
	}
	want := `{"entries":[{"capability":"beta","slug":"beta-rule","kind":"rule","heading":"Answer fast","anchors":["from docs/vision.md"],"body":"Answer in under 50 ms.","because":"rules hold the product together."},` +
		`{"capability":"product","slug":"pricing","kind":"question","heading":"How is it priced?","role":"raised","person":"sam","date":"2026-11-01","body":"Postponed until the widget exists."}]}` + "\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
	var back Taken
	if err := json.Unmarshal(b.Bytes(), &back); err != nil || !reflect.DeepEqual(&back, take(t, fixture(), "beta-rule", "pricing")) {
		t.Errorf("JSON does not round-trip: %v %+v", err, back)
	}
}

func TestWriteBareSlice(t *testing.T) {
	var b bytes.Buffer
	f := Flags{Req: Request{Capabilities: []string{"alpha"}}}
	if err := Write(&b, fixture(), f); err != nil {
		t.Fatal(err)
	}
	if b.String() != md(t, build(t, fixture(), f.Req)) {
		t.Errorf("Write differs from WriteMD")
	}
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

// putFolder writes one capability or the product, given as a single text, as
// the folder the map reads.
func putFolder(t *testing.T, root, dir, text string) {
	t.Helper()
	f, err := mapfolder.Split(text)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range f.Files {
		put(t, root, dir+"/"+name, body)
	}
}

// smallMap writes a map the check accepts, with one capability, x, whose
// rules are given.
func smallMap(t *testing.T, rules string) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, ".physa/README.md", "# Map\n\nRead the product first.\n")
	put(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: The product's knowledge.\n")
	putFolder(t, root, ".physa/product", "---\npromise: Teams ship.\n---\n\n# Mini\n\n## Rules\n\n### Never log emails\n`no-emails` · #1\nNever log customer emails.\nBecause: customers trust us.\n")
	putFolder(t, root, ".physa/capabilities/x", "---\npromise: X works.\nstewards: [dana]\n---\n\n# X\n\n## Rules\n\n"+rules)
	return root
}

func TestReadAcceptsAGoodMap(t *testing.T) {
	m, err := Read(smallMap(t, "### Keep it short\n`x-short` · #2 1a2b3c4\nKeep it short.\nBecause: it reads fast.\n"))
	if err != nil {
		t.Fatalf("a map the check accepts, with a commit anchor history cannot resolve: %v", err)
	}
	if len(m.Capabilities) != 1 || m.Capabilities[0].Name != "x" {
		t.Errorf("got %+v", m.Capabilities)
	}
}

func TestReadFailsOnMalformedMap(t *testing.T) {
	root := smallMap(t, "### Keep it short\n`x-short` · #2\nKeep it short.\n\n### Bad slug\n`Bad_Slug` · #3\nBecause: no.\n")
	m, err := Read(root)
	if m != nil || err == nil {
		t.Fatalf("want a failure and no map, got %v, %v", m, err)
	}
	t.Logf("error:\n%v", err)
	msg := err.Error()
	lines := regexp.MustCompile(`(?m)^\.physa/capabilities/x/rules\.md:\d+: .+$`).FindAllString(msg, -1)
	if len(lines) < 2 || !strings.HasPrefix(msg, "the map has ") {
		t.Errorf("want every problem as path:line: message, got:\n%s", msg)
	}
}

func TestOwnMap(t *testing.T) {
	m, err := Read(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	s := build(t, m, Request{Capabilities: []string{"init"}})
	out := md(t, s)
	inOrder(t, out, "# Slice: init\n", "## Product: Physa", "`no-adr`", decisionsLine, "## Init\n", "\nPromise: ",
		s.Sections[0].Overview, "### Rules", "## Reading further\n", "## How to anchor", "`Capability: init`")
	var init *mapfile.File
	for _, c := range m.Capabilities {
		if c.Name == "init" {
			init = c
		}
	}
	for _, f := range []*mapfile.File{m.Product, init} {
		for _, e := range f.Entries {
			if e.Kind == mapfile.Rule {
				continue
			}
			if strings.Contains(out, "`"+e.Slug+"` ·") || (len(e.Body) > 20 && strings.Contains(out, e.Body)) {
				t.Errorf("the bare slice holds the body of %s", e.Slug)
			}
			if !strings.Contains(out, "- "+e.Slug+": "+e.Heading+"\n") {
				t.Errorf("the bare slice lacks the heading line of %s", e.Slug)
			}
		}
	}
	var slugs []string
	for _, f := range []*mapfile.File{m.Product, init} {
		for _, e := range f.Entries {
			if e.Kind == mapfile.Decision {
				slugs = append(slugs, e.Slug)
				break
			}
		}
	}
	if tk := take(t, m, slugs...); len(tk.Entries) != len(slugs) || tk.Entries[0].Capability != "product" || tk.Entries[len(slugs)-1].Capability != "init" {
		t.Errorf("taking %v: %+v", slugs, tk.Entries)
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

// TestLatency times the whole path physa map slice runs, reading the map
// with the check, building and writing, on a generated map of 100
// capabilities. Every case is warmed once, then seven rounds run each case
// once in turn, so a burst of load on a busy machine lands on one run of
// every case rather than on all of one case's runs. Each run starts from a
// collected heap, as a fresh physa process does. The median of a case's
// seven runs must be under 50 ms.
func TestLatency(t *testing.T) {
	if raceEnabled {
		t.Skip("latency budgets don't hold under -race")
	}
	root := t.TempDir()
	put(t, root, "docs/vision.md", "# Vision\n")
	put(t, root, ".physa/README.md", "# Map\n\nRead the product first.\n")
	put(t, root, ".physa/glossary.md", glossary())
	putFolder(t, root, ".physa/product", "---\npromise: Teams ship with confidence.\n---\n\n# Mini\n\n## Rules\n\n### Never log emails\n`no-emails` · from docs/vision.md\nNever log customer emails.\nBecause: customers trust us.\n")
	var all []string
	for n := 1; n <= 100; n++ {
		putFolder(t, root, fmt.Sprintf(".physa/capabilities/cap-%03d", n), capability(n))
		all = append(all, fmt.Sprintf("cap-%03d", n))
	}
	var ten []string
	for n := 1; n <= 10; n++ {
		ten = append(ten, fmt.Sprintf("cap-%03d-decision-%d", n*10-3, n%3+1))
	}
	ten[9] = "no-emails"
	cases := []struct {
		name string
		req  Request
		want int // bytes written at least
	}{
		{"three", Request{Capabilities: []string{"cap-007", "cap-042", "cap-099"}}, 1000},
		{"all", Request{Capabilities: all}, 50000},
		{"all --with all", Request{Capabilities: all, WithDecisions: true, WithQuestions: true}, 100000},
		{"--entry 10", Request{Entries: ten}, 1000},
	}
	run := func(req Request) (time.Duration, int) {
		runtime.GC()
		start := time.Now()
		m, err := Read(root)
		if err != nil {
			t.Fatal(err)
		}
		var w countWriter
		if err := Write(&w, m, Flags{Req: req}); err != nil {
			t.Fatal(err)
		}
		return time.Since(start), int(w)
	}
	runs := make([][]time.Duration, len(cases))
	written := make([]int, len(cases))
	for round := 0; round <= 7; round++ {
		for i, c := range cases {
			d, n := run(c.req)
			written[i] = n
			if round > 0 {
				runs[i] = append(runs[i], d)
			}
		}
	}
	for i, c := range cases {
		slices.Sort(runs[i])
		median := runs[i][len(runs[i])/2]
		if written[i] < c.want {
			t.Fatalf("%s: wrote %d bytes, want at least %d", c.name, written[i], c.want)
		}
		if median >= 50*time.Millisecond {
			t.Errorf("%s: read with the check, build and write took a median %v, want under 50ms (runs %v)", c.name, median, runs[i])
		}
		t.Logf("%s: median %v of %v, %d bytes", c.name, median, runs[i], written[i])
	}
}

// countWriter counts the bytes written to it and keeps none.
type countWriter int

func (w *countWriter) Write(p []byte) (int, error) {
	*w += countWriter(len(p))
	return len(p), nil
}

var _ io.Writer = (*countWriter)(nil)
