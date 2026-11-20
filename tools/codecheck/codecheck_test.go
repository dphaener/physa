package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rules(vs []Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.Dir+" "+v.Rule)
	}
	return out
}

func mustCheck(t *testing.T, root string) []Violation {
	t.Helper()
	vs, err := check(root)
	if err != nil {
		t.Fatalf("check(%s): %v", root, err)
	}
	return vs
}

func TestGoodFixtureHasNoViolations(t *testing.T) {
	if vs := mustCheck(t, "testdata/good"); len(vs) != 0 {
		t.Fatalf("want none, got %v", rules(vs))
	}
}

func TestBadImport(t *testing.T) {
	vs := mustCheck(t, "testdata/badimport")
	if got := rules(vs); len(got) != 1 || got[0] != "internal/core no-plugin-imports" {
		t.Fatalf("got %v", got)
	}
	if m := vs[0].Message; !strings.Contains(m, "core.go") || !strings.Contains(m, "example.com/badimport/plugins/p") {
		t.Errorf("message should name file and import: %q", m)
	}
}

func TestBadImportFromTestFile(t *testing.T) {
	vs := mustCheck(t, "testdata/badimporttest")
	if got := rules(vs); len(got) != 1 || got[0] != "internal/core no-plugin-imports" {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(vs[0].Message, "core_test.go") {
		t.Errorf("message should name the test file: %q", vs[0].Message)
	}
}

func TestNoDoc(t *testing.T) {
	got := rules(mustCheck(t, "testdata/nodoc"))
	want := []string{"internal/nodoc package-doc", "internal/wrongdoc package-doc"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func writeModule(t *testing.T, lines int, blank int) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "big")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/big\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("// Package big is large.\npackage big\n") // 2 lines
	for i := 0; i < lines-2; i++ {
		fmt.Fprintf(&b, "var v%d = %d\n", i, i)
	}
	b.WriteString(strings.Repeat("\n", blank))
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// test files do not count toward the budget
	test := "package big\n" + strings.Repeat("var y = 1\n", 50)
	if err := os.WriteFile(filepath.Join(dir, "big_test.go"), []byte(test), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestOverBudget(t *testing.T) {
	got := rules(mustCheck(t, writeModule(t, sizeBudget+1, 0)))
	if len(got) != 1 || got[0] != "big size-budget" {
		t.Fatalf("got %v", got)
	}
}

func TestAtBudgetAndBlankLinesIgnored(t *testing.T) {
	if vs := mustCheck(t, writeModule(t, sizeBudget, 200)); len(vs) != 0 {
		t.Fatalf("got %v", rules(vs))
	}
}

func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		root string
		code int
		out  string
		err  string
	}{
		{"testdata/good", 0, "codecheck: ok (3 packages)", ""},
		{"testdata/badimport", 1, "", "internal/core: no-plugin-imports: "},
		{t.TempDir(), 2, "", "codecheck:"},
	}
	for _, c := range cases {
		var o, e bytes.Buffer
		if code := run([]string{c.root}, &o, &e); code != c.code {
			t.Errorf("%s: code %d want %d", c.root, code, c.code)
		}
		if !strings.Contains(o.String(), c.out) || !strings.Contains(e.String(), c.err) {
			t.Errorf("%s: out=%q err=%q", c.root, o.String(), e.String())
		}
	}
	var o, e bytes.Buffer
	if code := run([]string{"a", "b"}, &o, &e); code != 2 {
		t.Errorf("two args: code %d want 2", code)
	}
}

func TestRealRepoPasses(t *testing.T) {
	if vs := mustCheck(t, "../.."); len(vs) != 0 {
		t.Fatalf("repo violates: %v", rules(vs))
	}
}
