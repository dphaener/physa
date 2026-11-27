package sourceset

import (
	"fmt"
	"strings"
	"testing"
)

func files(paths ...string) []File {
	out := make([]File, len(paths))
	for i, p := range paths {
		out[i] = File{Path: p, Lines: 10}
	}
	return out
}

func TestTreeOverviewKinds(t *testing.T) {
	fs := files("main.go", "app/b.rb", "app/a.rb", "app/x.js", "spec/m/a.rb")
	if got, want := Tree(fs), "./ (1 file, 10 lines): main.go\napp/ (3 files, 30 lines): a.rb b.rb x.js\nspec/m/ (1 file, 10 lines): a.rb"; got != want {
		t.Errorf("Tree:\n%s\nwant\n%s", got, want)
	}
	if got, want := Overview(fs), "./ (1 file, 10 lines)\napp/ (3 files, 30 lines)\nspec/m/ (1 file, 10 lines)"; got != want {
		t.Errorf("Overview:\n%s", got)
	}
	if got, want := Kinds(fs), "./ (1 file: .go 1)\napp/ (3 files: .rb 2 .js 1)\nspec/m/ (1 file: .rb 1)"; got != want {
		t.Errorf("Kinds:\n%s", got)
	}
}

func big() []File {
	var paths []string
	for _, top := range []string{"a", "b", "c"} {
		for _, sub := range []string{"x", "y"} {
			for i := 0; i < 4; i++ {
				paths = append(paths, fmt.Sprintf("%s/%s/d%d/file%d.go", top, sub, i, i))
			}
		}
	}
	return files(paths...)
}

func TestPartsCoverEveryFileOnce(t *testing.T) {
	fs := big()
	for _, size := range []int{1, 60, 130, 300, 100000} {
		parts := Cut(fs, size)
		seen := map[string]int{}
		for _, p := range parts {
			for _, f := range p.Files {
				seen[f]++
			}
		}
		for _, f := range fs {
			if seen[f.Path] != 1 {
				t.Errorf("size %d: %s in %d parts", size, f.Path, seen[f.Path])
			}
		}
		if size >= 60 {
			for _, p := range parts {
				if len(p.Text) > size && strings.Contains(p.Text, "\n") {
					t.Errorf("size %d: part of %d chars holds several lines", size, len(p.Text))
				}
			}
		}
	}
}

func TestPartsNeverSplitAFolderLine(t *testing.T) {
	fs := big()
	whole := strings.Split(Tree(fs), "\n")
	for _, p := range Cut(fs, 200) {
		for _, l := range strings.Split(p.Text, "\n") {
			ok := false
			for _, w := range whole {
				ok = ok || w == l
			}
			if !ok {
				t.Errorf("not a whole folder line: %q", l)
			}
		}
	}
}

func TestPartsOneWhenTreeFits(t *testing.T) {
	fs := big()
	parts := Cut(fs, len(Tree(fs))+1)
	if len(parts) != 1 || parts[0].Text != Tree(fs) || len(parts[0].Files) != len(fs) {
		t.Fatalf("want 1 part with the whole tree, got %d", len(parts))
	}
}

func TestPartsKeepGroupsTogether(t *testing.T) {
	fs := big()
	// Each first-two-segment group (a/x) is 4 lines; size fits one group but not two.
	one := len(Tree(files("a/x/d0/file0.go", "a/x/d1/file1.go", "a/x/d2/file2.go", "a/x/d3/file3.go"))) + 1
	for _, p := range Cut(fs, one+10) {
		groups := map[string]bool{}
		for _, f := range p.Files {
			groups[strings.Join(strings.Split(f, "/")[:2], "/")] = true
		}
		if len(groups) != 1 {
			t.Errorf("part spans groups %v", groups)
		}
	}
}

func TestPartsOversizeLineIsItsOwnPart(t *testing.T) {
	fs := files("a/one.go", "b/"+strings.Repeat("n", 100)+".go", "c/two.go")
	parts := Cut(fs, 60)
	if len(parts) != 3 {
		t.Fatalf("got %d parts", len(parts))
	}
	if len(parts[1].Files) != 1 || !strings.HasPrefix(parts[1].Files[0], "b/") {
		t.Errorf("middle part: %v", parts[1].Files)
	}
}

func TestPartsEmpty(t *testing.T) {
	if got := Cut(nil, 10); len(got) != 0 {
		t.Errorf("got %d parts", len(got))
	}
}
