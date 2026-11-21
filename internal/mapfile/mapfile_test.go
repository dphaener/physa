package mapfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	if m.Product.Lines != 17 {
		t.Errorf("product lines = %d", m.Product.Lines)
	}
	rule := m.Product.Entries[0]
	if rule.Kind != Rule || rule.Slug != "no-emails" || rule.Heading != "Never log emails" || rule.Line != 9 ||
		rule.Because != "customers trust us with them." ||
		!reflect.DeepEqual(rule.Anchors, []Anchor{{Doc, "docs/vision.md"}}) ||
		rule.MetaHas != (Meta{Slug: true, Anchors: true}) {
		t.Errorf("rule: %+v", rule)
	}
	q := m.Product.Entries[1]
	if q.Kind != Question || q.Role != "raised" || q.Person != "sam" || q.Date != "2026-11-01" ||
		q.MetaHas != (Meta{Slug: true, Person: true, Date: true}) {
		t.Errorf("question: %+v", q)
	}
	if len(m.Capabilities) != 1 {
		t.Fatalf("capabilities: %d", len(m.Capabilities))
	}
	c := m.Capabilities[0]
	if c.Name != "delivery" || c.Path != ".physa/capabilities/delivery.md" || !reflect.DeepEqual(c.Stewards, []string{"dana", "sam"}) {
		t.Errorf("capability: %+v", c)
	}
	if got := c.Entries[0].Anchors; !reflect.DeepEqual(got, []Anchor{{PR, "1290"}, {Commit, "a1b2c3d"}}) {
		t.Errorf("anchors: %v", got)
	}
	d := c.Entries[1]
	if d.Kind != Decision || d.Slug != "retry-limit" || d.Role != "decided" || d.Person != "dana" ||
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
	expect(t, ps, ".physa/product.md", 3, `unknown front matter key "color"`)
	expect(t, ps, ".physa/capabilities/nofront.md", 1, "missing front matter")
	expect(t, ps, ".physa/capabilities/unclosed.md", 1, "not closed")
}

func TestMalformedMetaLine(t *testing.T) {
	_, ps := mustRead(t, "testdata/broken")
	// A meta line without its backticked slug, and reading goes on after it.
	expect(t, ps, ".physa/product.md", 11, "meta line must start with a `slug`")
	expect(t, ps, ".physa/capabilities/shapes.md", 14, "not a real date")
	expect(t, ps, ".physa/capabilities/shapes.md", 14, "out of order")
	expect(t, ps, ".physa/capabilities/shapes.md", 18, "unknown meta segment")
	expect(t, ps, ".physa/capabilities/shapes.md", 22, "missing meta line")
	expect(t, ps, ".physa/capabilities/shapes.md", 26, "one path without spaces")
}

func TestMalformedStructure(t *testing.T) {
	_, ps := mustRead(t, "testdata/broken")
	sh := ".physa/capabilities/shapes.md"
	expect(t, ps, sh, 7, "unknown section")
	expect(t, ps, sh, 11, "text before the first entry")
	expect(t, ps, sh, 15, "Because: has no text")
	expect(t, ps, sh, 19, "Rejected: has no items")
	expect(t, ps, sh, 28, "more than once")
	expect(t, ps, ".physa/capabilities/Bad_Name.md", 0, "not a capability file")
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
	for _, p := range []string{"product.md", "glossary.md", "README.md"} {
		expect(t, ps, ".physa/"+p, 0, "missing")
	}
	if has(ps, ".physa/capabilities", 0, "missing") {
		t.Errorf("a missing capabilities directory is not a problem: %v", ps)
	}
}

func TestNoCapabilitiesDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".physa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"product.md":  "---\npromise: p\n---\n# T\n",
		"glossary.md": "# Glossary\n\n**Map**: a thing.\n",
		"README.md":   "hi\n",
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
	files, _ := filepath.Glob("../../.physa/capabilities/*.md")
	headings := 0
	for _, f := range append(files, "../../.physa/product.md") {
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
	if len(m.Capabilities) != len(files) || entries != headings {
		t.Errorf("got %d capabilities, %d entries; want %d, %d", len(m.Capabilities), entries, len(files), headings)
	}
	if len(m.Glossary) < 40 {
		t.Errorf("got %d terms", len(m.Glossary))
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
	dir := filepath.Join(root, ".physa")
	if err := os.MkdirAll(filepath.Join(dir, "capabilities"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\npromise: p\n---\n\n# T\n\n## Rules\n\n### H\n`h`\n\nfirst\n\nsecond\n```\nfenced\n```\nBecause: x\n"
	if err := os.WriteFile(filepath.Join(dir, "product.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := mustRead(t, root)
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
