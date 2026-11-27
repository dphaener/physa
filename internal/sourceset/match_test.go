package sourceset

import (
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, file string
		want          bool
	}{
		{"", "a.go", false},
		{"./a.go", "a.go", true},
		{"/a.go", "a.go", true},
		{"docs/", "docs/a.md", true},
		{"docs/", "docs/x/y.md", true},
		{"docs/", "docs", false},
		{"docs/", "other/docs/a.md", true},
		{"testdata/", "internal/mapfile/testdata/good/a.md", true},
		{"testdata/", "internal/mapfile/testdata.go", false},
		{"a/docs/", "b/a/docs/x.md", false},
		{"a/docs/", "a/docs/x.md", true},
		{"*_test.go", "x_test.go", true},
		{"*_test.go", "pkg/deep/x_test.go", true},
		{"*_test.go", "pkg/x.go", false},
		{"go.mod", "sub/go.mod", true},
		{"**/x_test.go", "x_test.go", true},
		{"**/x_test.go", "a/b/x_test.go", true},
		{"**/x_test.go", "a/y_test.go", false},
		{"spec/**", "spec/a.rb", true},
		{"spec/**", "spec/a/b.rb", true},
		{"spec/**", "spec", false},
		{"spec/**", "lib/spec/a.rb", false},
		{"a/**/b.go", "a/b.go", true},
		{"a/**/b.go", "a/x/y/b.go", true},
		{"src/*/m.go", "src/a/m.go", true},
		{"src/*/m.go", "src/a/b/m.go", false},
		{"cmd/physa/main.go", "cmd/physa/main.go", true},
		{"cmd/physa", "cmd/physa/main.go", true},
		{"cmd/physa", "cmd/physa2/main.go", false},
		{"docs", "docs/a.md", true},
		{"docs", "x/docs/a.md", false},
		{"[", "a.go", false},
		{"a/[", "a/b", false},
		{"/", "a.go", false},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.file); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.file, got, c.want)
		}
	}
}

func TestSpecificityOrder(t *testing.T) {
	file := "dir/sub/a.go"
	order := []string{"dir/sub/a.go", "dir/sub/", "dir/", "*.go"}
	prev := exact + 1
	for _, p := range order {
		s := Specificity(p, file)
		if s == 0 || s >= prev {
			t.Fatalf("Specificity(%q) = %d, want below %d and above 0", p, s, prev)
		}
		prev = s
	}
	if Specificity("*.rb", file) != 0 || Specificity("[", file) != 0 {
		t.Fatal("non-matching patterns must score 0")
	}
	if got := Specificity("dir/", file); got != len("dir/")+1 {
		t.Fatalf("dir/ = %d", got)
	}
	if got := Specificity("dir/*/a.go", file); got != len("dir/")+1 {
		t.Fatalf("glob prefix = %d", got)
	}
}

func TestSplit(t *testing.T) {
	files := []File{{Path: "a.go"}, {Path: "a_test.go"}, {Path: "docs/guide.md"}, {Path: "docs/x_test.go"}, {Path: "main.go"}}
	d := Split(files, []string{"*_test.go", "nothing/"}, []string{"docs/"}, nil)
	// docs/ is more specific than *_test.go, so docs/x_test.go is a doc.
	want := map[Kind][]string{
		Test:   {"a_test.go"},
		Doc:    {"docs/guide.md", "docs/x_test.go"},
		Other:  {},
		Source: {"a.go", "main.go"},
	}
	if !reflect.DeepEqual(d.Files, want) {
		t.Fatalf("Files = %v, want %v", d.Files, want)
	}
	if !reflect.DeepEqual(d.Unused, []string{"nothing/"}) {
		t.Fatalf("Unused = %v", d.Unused)
	}
	if len(d.Dropped) != 0 {
		t.Fatalf("Dropped = %v", d.Dropped)
	}
}

func TestSplitTieGoesToFirstKind(t *testing.T) {
	d := Split([]File{{Path: "a.go"}, {Path: "b.go"}}, []string{"a.go"}, []string{"a.go"}, nil)
	if !reflect.DeepEqual(d.Files[Test], []string{"a.go"}) || len(d.Files[Doc]) != 0 {
		t.Fatalf("tie: %v", d.Files)
	}
}

func TestSplitDropsPatternMatchingEveryFile(t *testing.T) {
	files := []File{{Path: "a.go"}, {Path: "b/c.go"}}
	d := Split(files, []string{"*.go"}, nil, []string{"b/"})
	if !reflect.DeepEqual(d.Dropped, []string{"*.go"}) {
		t.Fatalf("Dropped = %v", d.Dropped)
	}
	if !reflect.DeepEqual(d.Files[Source], []string{"a.go"}) || !reflect.DeepEqual(d.Files[Other], []string{"b/c.go"}) {
		t.Fatalf("Files = %v", d.Files)
	}
	if len(d.Unused) != 0 {
		t.Fatalf("Unused = %v", d.Unused)
	}
}
