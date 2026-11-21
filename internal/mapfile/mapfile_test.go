package mapfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/mapfolder"
)

func mustRead(t *testing.T, root string) (*Map, []Problem) {
	t.Helper()
	m, ps, err := Read(root)
	if err != nil {
		t.Fatalf("Read(%s): %v", root, err)
	}
	return m, ps
}

func has(ps []Problem, path string, line int, part string) bool {
	for _, p := range ps {
		if p.Path == path && p.Line == line && strings.Contains(p.Message, part) {
			return true
		}
	}
	return false
}

func expect(t *testing.T, ps []Problem, path string, line int, part string) {
	t.Helper()
	if !has(ps, path, line, part) {
		t.Errorf("missing problem %s:%d containing %q in:\n%v", path, line, part, ps)
	}
}

func TestGoodMap(t *testing.T) {
	m, ps := mustRead(t, "testdata/good")
	if len(ps) != 0 {
		t.Fatalf("want no problems, got %v", ps)
	}
	if !m.HasReadme {
		t.Error("HasReadme should be true")
	}
	if m.Product == nil || m.Product.Name != "product" || m.Product.Promise != "Teams ship with confidence." || m.Product.Title != "Mini" {
		t.Fatalf("product: %+v", m.Product)
	}
	if m.Product.Path != ".physa/product" || m.Product.Lines != 11 {
		t.Errorf("product path %q, lines %d", m.Product.Path, m.Product.Lines)
	}
	if m.Product.Overview != "Mini is the small product the reader's tests read.\nIts second line stays in the overview." || m.Product.OverviewLine != 7 {
		t.Errorf("product overview %q at line %d", m.Product.Overview, m.Product.OverviewLine)
	}
	rule := m.Product.Entries[0]
	if rule.Kind != Rule || rule.Slug != "no-emails" || rule.Heading != "Never log emails" || rule.Line != 9 ||
		rule.Path != ".physa/product/rules.md" ||
		rule.Because != "customers trust us with them." ||
		!reflect.DeepEqual(rule.Anchors, []Anchor{{Doc, "docs/vision.md"}}) ||
		rule.MetaHas != (Meta{Slug: true, Anchors: true}) {
		t.Errorf("rule: %+v", rule)
	}
	q := m.Product.Entries[1]
	if q.Kind != Question || q.Path != ".physa/product/questions.md" || q.Line != 9 || q.Role != "raised" || q.Person != "sam" || q.Date != "2026-11-01" ||
		q.MetaHas != (Meta{Slug: true, Person: true, Date: true}) {
		t.Errorf("question: %+v", q)
	}
	if len(m.Capabilities) != 1 {
		t.Fatalf("capabilities: %d", len(m.Capabilities))
	}
	c := m.Capabilities[0]
	if c.Name != "delivery" || c.Path != ".physa/capabilities/delivery" ||
		c.Overview != "Delivery sends webhooks and retries them until they land." || !reflect.DeepEqual(c.Stewards, []string{"dana", "sam"}) {
		t.Errorf("capability: %+v", c)
	}
	if got := c.Entries[0].Anchors; !reflect.DeepEqual(got, []Anchor{{PR, "1290"}, {Commit, "a1b2c3d"}}) {
		t.Errorf("anchors: %v", got)
	}
	d := c.Entries[1]
	if d.Kind != Decision || d.Path != ".physa/capabilities/delivery/decisions.md" || d.Line != 9 || d.Slug != "retry-limit" || d.Role != "decided" || d.Person != "dana" ||
		d.How != "accepting a proposal" || d.Date != "2026-03-02" || d.Because != "support needs a bounded window." {
		t.Errorf("decision: %+v", d)
	}
	wantAnchors := []Anchor{{PR, "1102"}, {PR, "1140"}, {Doc, "docs/vision.md"}}
	if !reflect.DeepEqual(d.Anchors, wantAnchors) {
		t.Errorf("decision anchors: %v", d.Anchors)
	}
	if !reflect.DeepEqual(d.Rejected, []string{"Unlimited retries: floods endpoints.", "Three attempts: too short."}) {
		t.Errorf("rejected: %v", d.Rejected)
	}
	want := []Term{
		{Name: "Map", Definition: "The product's knowledge.", Avoid: []string{"model", "graph"}, Line: 7},
		{Name: "Rule", Definition: "Something that must hold, with its reason.", Avoid: []string{"constraint"}, Line: 10},
	}
	if !reflect.DeepEqual(m.Glossary, want) {
		t.Errorf("glossary: %+v", m.Glossary)
	}
}

func TestMalformedFrontMatter(t *testing.T) {
	_, ps := mustRead(t, "testdata/broken")
	expect(t, ps, ".physa/product/index.md", 3, `unknown front matter key "color"`)
	expect(t, ps, ".physa/capabilities/nofront/index.md", 1, "missing front matter")
	expect(t, ps, ".physa/capabilities/unclosed/index.md", 1, "not closed")
	expect(t, ps, ".physa/capabilities/shapes/rules.md", 1, "front matter belongs only in index.md")
}

func TestMalformedMetaLine(t *testing.T) {
	_, ps := mustRead(t, "testdata/broken")
	// A meta line without its backticked slug, and reading goes on after it.
	expect(t, ps, ".physa/product/rules.md", 11, "meta line must start with a `slug`")
	sh := ".physa/capabilities/shapes/rules.md"
	expect(t, ps, sh, 18, "not a real date")
	expect(t, ps, sh, 18, "out of order")
	expect(t, ps, sh, 22, "unknown meta segment")
	expect(t, ps, sh, 26, "missing meta line")
	expect(t, ps, sh, 30, "one path without spaces")
}

func TestMalformedStructure(t *testing.T) {
	_, ps := mustRead(t, "testdata/broken")
	sh := ".physa/capabilities/shapes/rules.md"
	expect(t, ps, sh, 7, "text outside the contents and the entries")
	expect(t, ps, sh, 15, "text before the first entry")
	expect(t, ps, sh, 19, "Because: has no text")
	expect(t, ps, sh, 23, "Rejected: has no items")
	expect(t, ps, ".physa/capabilities/Bad_Name", 0, "not a capability folder")
	expect(t, ps, ".physa/capabilities/shapes/notes.txt", 0, "not a file of a folder")
	expect(t, ps, ".physa/capabilities/noindex/index.md", 0, "missing file")
}

func TestParts(t *testing.T) {
	m, _ := mustRead(t, "testdata/broken")
	var shapes, noindex *File
	for _, c := range m.Capabilities {
		switch c.Name {
		case "shapes":
			shapes = c
		case "noindex":
			noindex = c
		}
	}
	if shapes == nil || noindex == nil {
		t.Fatalf("capabilities: %v", m.Capabilities)
	}
	if len(shapes.Parts) != 3 {
		t.Fatalf("parts: %+v", shapes.Parts)
	}
	ix, rules, dec := shapes.Parts[0], shapes.Parts[1], shapes.Parts[2]
	if ix.Path != ".physa/capabilities/shapes/index.md" || ix.Kind != 0 || ix.Lines != 10 || ix.Title != "Shapes" || ix.TitleLine != 5 ||
		!reflect.DeepEqual(ix.Contents, []Line{{9, "- [Rules](rules.md)"}, {10, "- [Decisions](decisions.md)"}}) {
		t.Errorf("index part: %+v", ix)
	}
	if rules.Kind != Rule || rules.Title != "Shapes · Rules" || rules.TitleLine != 5 ||
		!reflect.DeepEqual(rules.Contents, []Line{{11, "- [Out of order](#out-of-order)"}}) || len(rules.OutsideEntries) != 0 {
		t.Errorf("rules part: %+v", rules)
	}
	wantHeadings := []Line{{5, "# Shapes · Rules"}, {9, "## Contents"}, {13, "## Entries"}, {17, "### Out of order"},
		{21, "### Unknown segment"}, {25, "### No meta"}, {29, "### Bad from"}}
	if !reflect.DeepEqual(rules.Headings, wantHeadings) {
		t.Errorf("rules headings: %+v", rules.Headings)
	}
	if dec.Kind != Decision || !reflect.DeepEqual(dec.OutsideEntries, []int{5}) || len(dec.Contents) != 0 {
		t.Errorf("decisions part: %+v", dec)
	}
	last := shapes.Entries[len(shapes.Entries)-1]
	if last.Kind != Decision || last.Slug != "early" || last.Path != ".physa/capabilities/shapes/decisions.md" || last.Line != 5 {
		t.Errorf("last entry: %+v", last)
	}
	if len(noindex.Parts) != 1 || noindex.Parts[0].Kind != Rule || len(noindex.Entries) != 1 || noindex.Lines != 0 {
		t.Errorf("noindex: %+v", noindex)
	}
}

func TestOldLayout(t *testing.T) {
	m, ps := mustRead(t, "testdata/old")
	want := []Problem{
		{".physa/capabilities/delivery.md", 0, OldLayout},
		{".physa/product.md", 0, OldLayout},
	}
	if !reflect.DeepEqual(ps, want) {
		t.Errorf("problems = %v, want %v", ps, want)
	}
	if m.Product != nil || len(m.Capabilities) != 0 {
		t.Errorf("map: %+v", m)
	}
	if got := ps[1].String(); got != ".physa/product.md: the map is in the old single-file format; convert it to folders (docs/architecture/09-capability-folders.md)" {
		t.Errorf("message = %q", got)
	}
}

func TestGoodMapIsFormatted(t *testing.T) {
	changed, problems, err := mapfolder.FormatMap("testdata/good", false)
	if err != nil || len(changed) != 0 || len(problems) != 0 {
		t.Errorf("FormatMap = %v, %v, %v; want the good fixture already formatted", changed, problems, err)
	}
}

func TestMalformedGlossary(t *testing.T) {
	_, ps := mustRead(t, "testdata/broken")
	g := ".physa/glossary.md"
	expect(t, ps, g, 3, "directly follow a term")
	expect(t, ps, g, 5, "no definition")
	expect(t, ps, g, 8, "lists no words")
}

func TestProblemsSorted(t *testing.T) {
	_, ps := mustRead(t, "testdata/broken")
	for i := 1; i < len(ps); i++ {
		a, b := ps[i-1], ps[i]
		if a.Path > b.Path || (a.Path == b.Path && a.Line > b.Line) {
			t.Fatalf("not sorted: %v before %v", a, b)
		}
	}
}

func TestMissingFiles(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Read(root); err == nil {
		t.Fatal("missing .physa should be an error")
	}
	if err := os.MkdirAll(filepath.Join(root, ".physa"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, ps := mustRead(t, root)
	if m.Product != nil || m.HasReadme {
		t.Errorf("map: %+v", m)
	}
	for _, p := range []string{"product/index.md", "glossary.md", "README.md"} {
		expect(t, ps, ".physa/"+p, 0, "missing")
	}
	if has(ps, ".physa/capabilities", 0, "missing") {
		t.Errorf("a missing capabilities directory is not a problem: %v", ps)
	}
}

func TestNoCapabilitiesDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".physa")
	if err := os.MkdirAll(filepath.Join(dir, "product"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"product/index.md": "---\npromise: p\n---\n# T\n\nT is a product.\n",
		"glossary.md":      "# Glossary\n\n**Map**: a thing.\n",
		"README.md":        "hi\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, ps := mustRead(t, root)
	if len(ps) != 0 {
		t.Fatalf("want no problems, got %v", ps)
	}
	if len(m.Capabilities) != 0 {
		t.Errorf("capabilities = %d, want 0", len(m.Capabilities))
	}
}

func TestCapabilitiesNotADirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".physa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "capabilities"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, ps := mustRead(t, root)
	expect(t, ps, ".physa/capabilities", 0, "not a directory")
	if len(m.Capabilities) != 0 {
		t.Errorf("capabilities = %d, want 0", len(m.Capabilities))
	}
}

func TestOwnMap(t *testing.T) {
	m, ps := mustRead(t, "../..")
	if len(ps) != 0 {
		t.Fatalf("own map has problems: %v", ps)
	}
	entries := len(m.Product.Entries)
	for _, c := range m.Capabilities {
		entries += len(c.Entries)
	}
	dirs, _ := filepath.Glob("../../.physa/capabilities/*/index.md")
	files, _ := filepath.Glob("../../.physa/capabilities/*/[rdq]*.md")
	more, _ := filepath.Glob("../../.physa/product/[rdq]*.md")
	headings := 0
	for _, f := range append(files, more...) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "### ") {
				headings++
			}
		}
	}
	if len(m.Capabilities) != len(dirs) || entries != headings {
		t.Errorf("got %d capabilities, %d entries; want %d, %d", len(m.Capabilities), entries, len(dirs), headings)
	}
	if len(m.Glossary) < 40 {
		t.Errorf("got %d terms", len(m.Glossary))
	}
}

// TestOwnMapCounts holds the converted map to the entry counts per kind that
// physa map index printed before the conversion.
func TestOwnMapCounts(t *testing.T) {
	m, _ := mustRead(t, "../..")
	want := map[string][3]int{
		"confidence": {4, 1, 0}, "earned-release": {2, 5, 0}, "engine": {5, 8, 0},
		"harnesses": {2, 7, 0}, "ideas": {4, 2, 0}, "init": {6, 4, 1}, "learning": {4, 5, 0},
		"map": {5, 3, 0}, "rollback": {3, 1, 0}, "signals": {4, 4, 0}, "slices": {4, 3, 0},
		"strength": {3, 3, 1}, "views": {3, 11, 1},
	}
	if len(m.Capabilities) != len(want) {
		t.Errorf("got %d capabilities, want %d", len(m.Capabilities), len(want))
	}
	for _, c := range m.Capabilities {
		var got [3]int
		for _, e := range c.Entries {
			got[e.Kind-1]++
		}
		if got != want[c.Name] {
			t.Errorf("%s: counts %v, want %v", c.Name, got, want[c.Name])
		}
		if c.Promise == "" || c.Overview == "" || c.OverviewLine == 0 || c.Path != ".physa/capabilities/"+c.Name {
			t.Errorf("%s: promise %q, overview %q at %d, path %q", c.Name, c.Promise, c.Overview, c.OverviewLine, c.Path)
		}
		for _, p := range c.Parts {
			if len(p.OutsideEntries) != 0 {
				t.Errorf("%s: ### outside entries at %v", p.Path, p.OutsideEntries)
			}
		}
	}
	if m.Product == nil || m.Product.Overview == "" || len(m.Product.Entries) == 0 {
		t.Errorf("product: %+v", m.Product)
	}
}

func TestBodyAndDefinition(t *testing.T) {
	m, _ := mustRead(t, "testdata/good")
	rule, q := m.Product.Entries[0], m.Product.Entries[1]
	if rule.Body != "Never log customer emails." {
		t.Errorf("rule body = %q", rule.Body)
	}
	if q.Body != "" {
		t.Errorf("question without body has Body %q", q.Body)
	}
	c := m.Capabilities[0]
	if c.Entries[0].Body != "Retry only by enqueueing a job." || c.Entries[1].Body != "Six attempts, then dead-letter." {
		t.Errorf("bodies = %q, %q", c.Entries[0].Body, c.Entries[1].Body)
	}
	if m.Glossary[1].Definition != "Something that must hold, with its reason." {
		t.Errorf("definition = %q", m.Glossary[1].Definition)
	}
}

func TestBodyJoinsLinesAndDropsBlanks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".physa", "product")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "# T · Rules\n\n## Contents\n\n- [H](#h)\n\n## Entries\n\n### H\n`h`\n\nfirst\n\nsecond\n```\nfenced\n### not an entry\n```\nBecause: x\n"
	if err := os.WriteFile(filepath.Join(dir, "rules.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := mustRead(t, root)
	if len(m.Product.Entries) != 1 {
		t.Fatalf("entries = %+v", m.Product.Entries)
	}
	if got := m.Product.Entries[0].Body; got != "first\nsecond" {
		t.Errorf("body = %q", got)
	}
}

func TestBrokenBodyNoPanic(t *testing.T) {
	m, _ := mustRead(t, "testdata/broken")
	if len(m.Capabilities) == 0 {
		t.Fatal("no capabilities read")
	}
}

func TestOwnMapBody(t *testing.T) {
	m, _ := mustRead(t, "../..")
	for _, c := range m.Capabilities {
		if c.Name != "slices" {
			continue
		}
		for _, e := range c.Entries {
			if e.Slug == "deterministic-slice" {
				want := "Everything in a slice except its current-state section depends only on the map and the request."
				if e.Body != want {
					t.Errorf("body = %q", e.Body)
				}
				return
			}
		}
	}
	t.Error("deterministic-slice not found")
}

func TestValidSlug(t *testing.T) {
	for _, s := range []string{"a", "retry-on-review", "v2-api"} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "Bad_Slug", "-x", "x-", "a--b", "a b"} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
}
