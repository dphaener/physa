package mapfolder

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportFolder writes recovered's folder into a temp dir and returns it.
func exportFolder(t *testing.T) string {
	t.Helper()
	f, err := Split(recovered)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := f.Write(root, CapabilityDir("export")); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, filepath.FromSlash(CapabilityDir("export")))
}

func readIn(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const twoFiles = "### Two files per export\n`two-files` · decided by sam, in their words · 2026-11-21 · #13\nBecause: notes and links differ."

func TestAddEntryAppendsAndRegeneratesTheContents(t *testing.T) {
	dir := exportFolder(t)
	index := readIn(t, dir, IndexFile)
	first, last, err := AddEntry(dir, "Export", Decision, twoFiles)
	if err != nil {
		t.Fatal(err)
	}
	got := readIn(t, dir, DecisionsFile)
	want := "# Export · Decisions\n\n## Contents\n\n- [One file per export](#one-file-per-export)\n- [Two files per export](#two-files-per-export)\n\n## Entries\n\n" +
		"### One file per export\n`one-file` · decided by dana, in their words · 2026-11-20 · #12\nBecause: one file is easy to share.\n\n" + twoFiles + "\n"
	if got != want {
		t.Errorf("decisions.md =\n%s\nwant\n%s", got, want)
	}
	if ls := lines(got); first != 14 || last != 16 || ls[first-1] != "### Two files per export" {
		t.Errorf("lines %d-%d", first, last)
	}
	if readIn(t, dir, IndexFile) != index {
		t.Error("index.md changed though no kind file was made")
	}
}

func TestAddEntryMakesTheKindFileAndLinksIt(t *testing.T) {
	dir := exportFolder(t)
	q := "### Export to PDF?\n`pdf-export` · raised by sam · 2026-11-21"
	first, last, err := AddEntry(dir, "Export", Question, q)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readIn(t, dir, QuestionsFile), KindFile("Export", Question, []string{q}); got != want {
		t.Errorf("questions.md =\n%s\nwant\n%s", got, want)
	}
	if first != 9 || last != 10 {
		t.Errorf("lines %d-%d, want 9-10", first, last)
	}
	if ix := readIn(t, dir, IndexFile); !strings.HasSuffix(ix, "\n\n- [Decisions](decisions.md)\n- [Questions](questions.md)\n") {
		t.Errorf("index.md =\n%s", ix)
	}
}

func TestAddEntryNamesTheFileOutsideItsFormat(t *testing.T) {
	dir := exportFolder(t)
	_, _, err := AddEntry(dir, "Export", Decision, "## Notes\n"+twoFiles)
	var fe *FormatError
	if !errors.As(err, &fe) || !strings.HasPrefix(err.Error(), "decisions.md: line 13: ") {
		t.Errorf("err = %v", err)
	}
}

func TestRestorePutsTheFolderBack(t *testing.T) {
	dir := exportFolder(t)
	snap, err := Take(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddEntry(dir, "Export", Decision, twoFiles); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddEntry(dir, "Export", Rule, "### Files are UTF-8\n`utf8` · #14\nBecause: every tool reads it."); err != nil {
		t.Fatal(err)
	}
	if err := snap.Restore(dir); err != nil {
		t.Fatal(err)
	}
	now, err := Take(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(now) != len(snap) {
		t.Errorf("folder holds %d files, want %d", len(now), len(snap))
	}
	for name, b := range snap {
		if string(now[name]) != string(b) {
			t.Errorf("%s not restored:\n%s", name, now[name])
		}
	}
	if _, err := os.Stat(filepath.Join(dir, RulesFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("rules.md left behind: %v", err)
	}
}
