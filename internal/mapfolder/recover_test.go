package mapfolder

import (
	"os"
	"path/filepath"
	"testing"
)

const recovered = `---
promise: Notes leave the product.
---

# Export

Notes leave the product as files.

## Decisions

### One file per export
` + "`one-file`" + ` · decided by dana, in their words · 2026-11-20 · #12
Because: one file is easy to share.
`

func TestOriginMapsFolderLinesToTheSingleText(t *testing.T) {
	f, err := Split(recovered)
	if err != nil {
		t.Fatal(err)
	}
	// decisions.md: title 1, blank, ## Contents, blank, the contents line 5,
	// blank, ## Entries 7, blank, heading 9, meta 10, Because: 11.
	for _, c := range []struct {
		file       string
		line, want int
	}{
		{DecisionsFile, 9, 11}, {DecisionsFile, 10, 12}, {DecisionsFile, 11, 13},
		{DecisionsFile, 1, 5}, {DecisionsFile, 5, 0}, {DecisionsFile, 0, 0}, {DecisionsFile, 99, 0},
		{IndexFile, 7, 7}, {RulesFile, 1, 0},
	} {
		if got := f.Origin(c.file, c.line); got != c.want {
			t.Errorf("Origin(%s, %d) = %d, want %d", c.file, c.line, got, c.want)
		}
	}
}

func TestWriteMakesTheFolder(t *testing.T) {
	f, err := Split(recovered)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := f.Write(root, CapabilityDir("export")); err != nil {
		t.Fatal(err)
	}
	for name, want := range f.Files {
		got, err := os.ReadFile(filepath.Join(root, ".physa", "capabilities", "export", name))
		if err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".physa", "capabilities", "export", RulesFile)); !os.IsNotExist(err) {
		t.Errorf("rules.md written for no rules: %v", err)
	}
}
