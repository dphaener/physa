package mapindex

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/mapfile"
)

func render(t *testing.T, lines []Line) string {
	t.Helper()
	var b bytes.Buffer
	if err := Write(&b, lines); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func entries(rules, decisions, questions int) []mapfile.Entry {
	var es []mapfile.Entry
	for range rules {
		es = append(es, mapfile.Entry{Kind: mapfile.Rule})
	}
	for range decisions {
		es = append(es, mapfile.Entry{Kind: mapfile.Decision})
	}
	for range questions {
		es = append(es, mapfile.Entry{Kind: mapfile.Question})
	}
	return es
}

func TestBuildCountsAndKeepsOrder(t *testing.T) {
	m := &mapfile.Map{
		Product: &mapfile.File{Name: "product", Entries: entries(9, 9, 9)},
		Capabilities: []*mapfile.File{
			{Name: "alpha", Promise: "A.", Entries: entries(3, 2, 1)},
			{Name: "beta", Promise: "B.", Entries: entries(0, 1, 0)},
			{Name: "gamma", Promise: "C."},
		},
	}
	got := Build(m)
	want := []Line{
		{"alpha", "A.", 3, 2, 1},
		{"beta", "B.", 0, 1, 0},
		{"gamma", "C.", 0, 0, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWriteCounts(t *testing.T) {
	cases := []struct {
		line Line
		want string
	}{
		{Line{"a", "P.", 3, 2, 1}, "a  P.  3 rules · 2 decisions · 1 question\n"},
		{Line{"a", "P.", 5, 4, 0}, "a  P.  5 rules · 4 decisions\n"},
		{Line{"a", "P.", 1, 0, 0}, "a  P.  1 rule\n"},
		{Line{"a", "P.", 0, 1, 0}, "a  P.  1 decision\n"},
		{Line{"a", "P.", 0, 0, 2}, "a  P.  2 questions\n"},
		{Line{"a", "P.", 2, 0, 1}, "a  P.  2 rules · 1 question\n"},
		{Line{"a", "P.", 0, 0, 0}, "a  P.\n"},
	}
	for _, c := range cases {
		if got := render(t, []Line{c.line}); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.line, got, c.want)
		}
	}
}

func TestWritePadsNames(t *testing.T) {
	got := render(t, []Line{
		{"ab", "One.", 1, 0, 0},
		{"longer-name", "Two.", 0, 0, 1},
	})
	want := "ab           One.  1 rule\nlonger-name  Two.  1 question\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteMissingPromise(t *testing.T) {
	got := render(t, []Line{{"a", "", 0, 0, 0}, {"b", "", 1, 0, 0}})
	want := "a  (no promise)\nb  (no promise)  1 rule\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteEmptyMap(t *testing.T) {
	if got := render(t, Build(&mapfile.Map{})); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
	if got := render(t, nil); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}

func TestWriteNoTrailingSpaces(t *testing.T) {
	got := render(t, []Line{{"a", "P. ", 0, 0, 0}})
	if got != "a  P.\n" {
		t.Errorf("got %q", got)
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

func TestLatency(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".physa/README.md", "# Map\n\nRead product.md first.\n")
	put(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: The product's knowledge.\n_Avoid_: model\n")
	put(t, root, ".physa/product.md", "---\npromise: Teams ship with confidence.\n---\n\n# Mini\n\n## Rules\n\n### Never log emails\n`no-emails` · from docs/vision.md\nNever log customer emails.\nBecause: customers trust us.\n")
	for n := 1; n <= 100; n++ {
		put(t, root, fmt.Sprintf(".physa/capabilities/cap-%03d.md", n), capability(n))
	}

	start := time.Now()
	m, problems, err := mapfile.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	lines := Build(m)
	if err := Write(io.Discard, lines); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	if len(problems) != 0 {
		t.Fatalf("generated map has problems: %v", problems)
	}
	if len(lines) != 100 {
		t.Fatalf("got %d lines, want 100", len(lines))
	}
	if lines[0].Rules != 4 || lines[0].Decisions != 3 || lines[0].Questions != 1 {
		t.Fatalf("unexpected counts: %+v", lines[0])
	}
	if elapsed >= 50*time.Millisecond {
		t.Errorf("read, build and write took %v, want under 50ms", elapsed)
	}
}
