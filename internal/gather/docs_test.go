package gather

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDocs(t *testing.T) {
	root := t.TempDir()
	for rel, kind := range map[string]string{
		"AGENTS.md": KindAgent, "CLAUDE.md": KindAgent, ".claude/CLAUDE.md": KindAgent, ".cursorrules": KindAgent,
		".cursor/rules/a.mdc": KindAgent, ".cursor/rules/sub/b.md": KindAgent,
		"CONTRIBUTING.md": KindContributing, ".github/CONTRIBUTING.md": KindContributing,
		"README.md": KindReadme, "README.rst": KindReadme,
		"DECISIONS.md": KindDecisions, "docs/decisions.md": KindDecisions,
		"docs/decisions/one.md":       KindDecisions,
		"docs/design/0001-first.md":   KindDecisions,
		"docs/design/0002_second.md":  KindDecisions,
		"doc/arch/records/0003-x.md":  KindDecisions,
		"doc/arch/records/0004-y.md":  KindDecisions,
		"doc/decision-records/abc.md": KindDecisions,
	} {
		write(t, root, rel, "body of "+rel)
		_ = kind
	}
	write(t, root, "node_modules/README.md", "ignored")
	write(t, root, "vendor/dep/decisions/x.md", "ignored")
	write(t, root, ".hidden/decisions/x.md", "ignored")
	write(t, root, "docs/notes/0001-only.md", "one numbered file is not a folder")
	write(t, root, "docs/big.md", "x")
	write(t, root, "BIG/README.md", "x")
	write(t, root, "CONTRIBUTING", strings.Repeat("a", MaxDocBytes+1))

	docs, skipped, err := Docs(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	var paths []string
	for _, d := range docs {
		got[d.Path] = d.Kind
		paths = append(paths, d.Path)
		if d.Body != "body of "+d.Path {
			t.Errorf("%s: body %q", d.Path, d.Body)
		}
	}
	want := map[string]string{
		"AGENTS.md": KindAgent, "CLAUDE.md": KindAgent, ".claude/CLAUDE.md": KindAgent, ".cursorrules": KindAgent,
		".cursor/rules/a.mdc": KindAgent, ".cursor/rules/sub/b.md": KindAgent,
		"CONTRIBUTING.md": KindContributing, ".github/CONTRIBUTING.md": KindContributing,
		"README.md": KindReadme, "README.rst": KindReadme,
		"DECISIONS.md": KindDecisions, "docs/decisions.md": KindDecisions,
		"docs/decisions/one.md":       KindDecisions,
		"docs/design/0001-first.md":   KindDecisions,
		"docs/design/0002_second.md":  KindDecisions,
		"doc/arch/records/0003-x.md":  KindDecisions,
		"doc/arch/records/0004-y.md":  KindDecisions,
		"doc/decision-records/abc.md": KindDecisions,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	if !sortedStrings(paths) {
		t.Errorf("not sorted: %v", paths)
	}
	if !reflect.DeepEqual(skipped, []string{"CONTRIBUTING"}) {
		t.Errorf("skipped %v", skipped)
	}
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func TestDocsEmpty(t *testing.T) {
	docs, skipped, err := Docs(t.TempDir())
	if err != nil || len(docs) != 0 || len(skipped) != 0 {
		t.Fatalf("%v %v %v", docs, skipped, err)
	}
}
