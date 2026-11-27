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

func TestGitOutsideAdapter(t *testing.T) {
	vs := mustCheck(t, "testdata/gitexec")
	if got := rules(vs); len(got) != 2 || got[0] != "internal/runner git-adapter-only" || got[1] != "internal/runner git-adapter-only" {
		t.Fatalf("got %v", got)
	}
	for _, v := range vs {
		if !strings.Contains(v.Message, "runner.go:") || strings.Contains(v.Message, "_test.go") {
			t.Errorf("message should name the non-test file: %q", v.Message)
		}
	}
}

func TestPackageBudget(t *testing.T) {
	root := writeModule(t, 50, 0)
	vs, _, err := scan(root, map[string]int{"big": 20})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := rules(vs); len(got) != 1 || got[0] != "big size-budget" {
		t.Fatalf("got %v", got)
	}
	if m := vs[0].Message; !strings.Contains(m, "50 non-blank lines") || !strings.Contains(m, "budget is 20") {
		t.Errorf("message should name lines and budget: %q", m)
	}
	t.Logf("reported: %s: %s: %s", vs[0].Dir, vs[0].Rule, vs[0].Message)
	if vs, _, err := scan(root, map[string]int{"big": 50}); err != nil || len(vs) != 0 {
		t.Fatalf("at its package budget: err=%v got %v", err, rules(vs))
	}
	if vs, _, err := scan(root, map[string]int{"big": sizeBudget + 100}); err != nil || len(vs) != 0 {
		t.Fatalf("a package budget above the global one must not raise it: err=%v got %v", err, rules(vs))
	}
	if vs, _, err := scan(root, map[string]int{"big": 0}); err != nil || len(vs) != 1 {
		t.Fatalf("zero budget: err=%v got %v", err, rules(vs))
	}
	if vs, _, err := scan(root, map[string]int{"other": 1}); err != nil || len(vs) != 0 {
		t.Fatalf("an entry for another package must not apply: err=%v got %v", err, rules(vs))
	}
}

func TestEngineTablesWriterFlagged(t *testing.T) {
	vs := mustCheck(t, "testdata/tablewriter")
	if got := rules(vs); len(got) != 2 || got[0] != "internal/other engine-tables-writer" || got[1] != "internal/other engine-tables-writer" {
		t.Fatalf("got %v", got)
	}
	for i, want := range []string{"other.go:7: writes the engine's table ideas", "other.go:8: writes the engine's table events"} {
		if !strings.Contains(vs[i].Message, want) || !strings.Contains(vs[i].Message, "03 §5.1") {
			t.Errorf("violation %d: want %q in %q", i, want, vs[i].Message)
		}
		t.Logf("reported: %s: %s: %s", vs[i].Dir, vs[i].Rule, vs[i].Message)
	}
}

func TestEngineTablesWriterFlagsSpends(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "other")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(root, "go.mod"): "module example.com/spends\n",
		filepath.Join(dir, "other.go"): "// Package other writes a table only internal/state may.\npackage other\n\n" +
			"// Add inserts.\nvar Add = \"INSERT INTO spends (pursuit_id) VALUES (?)\"\n",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vs := mustCheck(t, root)
	if got := rules(vs); len(got) != 1 || got[0] != "internal/other engine-tables-writer" || !strings.Contains(vs[0].Message, "writes the engine's table spends") {
		t.Fatalf("got %v %v", got, vs)
	}
}

func TestEngineTablesWriterFlagsEvidence(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "other")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(root, "go.mod"): "module example.com/evidence\n",
		filepath.Join(dir, "other.go"): "// Package other writes a table only internal/state may.\npackage other\n\n" +
			"// Add inserts.\nvar Add = \"INSERT INTO evidence (kind) VALUES (?)\"\n\n" +
			"// Poll updates.\nvar Poll = \"UPDATE evidence_polls SET vcs_commit = ?\"\n",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vs := mustCheck(t, root)
	if got := rules(vs); len(got) != 2 || !strings.Contains(vs[0].Message, "writes the engine's table evidence") || !strings.Contains(vs[1].Message, "writes the engine's table evidence_polls") {
		t.Fatalf("got %v %v", got, vs)
	}
}

func TestEngineTablesWriterAllowed(t *testing.T) {
	if vs := mustCheck(t, "testdata/tablewriterok"); len(vs) != 0 {
		t.Fatalf("got %v", vs)
	}
}

func TestStateBudget(t *testing.T) {
	if got := packageBudgets["internal/state"]; got != 700 {
		t.Errorf("internal/state budget = %d, want 700", got)
	}
	if got := packageBudgets["internal/repos"]; got != 350 {
		t.Errorf("internal/repos budget = %d, want 350", got)
	}
}

func TestMapBelowEngine(t *testing.T) {
	vs := mustCheck(t, "testdata/mapengine")
	got := rules(vs)
	want := []string{"internal/mapfile map-below-engine", "internal/vcs map-below-engine"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	for i, file := range []string{"mapfile.go imports example.com/mapengine/internal/engine", "vcs_test.go imports example.com/mapengine/internal/state/work"} {
		if !strings.Contains(vs[i].Message, file) {
			t.Errorf("violation %d: want %q in %q", i, file, vs[i].Message)
		}
	}
	t.Logf("reported: %s: %s: %s", vs[0].Dir, vs[0].Rule, vs[0].Message)
}

func TestFreshContextOnMapSide(t *testing.T) {
	vs := mustCheck(t, "testdata/freshctx")
	var got []string
	for _, v := range vs {
		got = append(got, v.Dir+" "+v.Rule+" "+v.Message)
	}
	want := []string{
		"internal/runner context-from-command runner.go:10 makes a fresh context; take the command's ctx instead",
		"internal/runner context-from-command runner.go:13 makes a fresh context; take the command's ctx instead",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTestSwapReportsSwappedPackageVariables(t *testing.T) {
	vs := mustCheck(t, "testdata/testswap")
	var got []string
	for _, v := range vs {
		got = append(got, v.Dir+" "+v.Rule+" "+v.Message)
	}
	want := []string{
		"internal/config tests-inject-fakes config_test.go:10 assigns package variable Table; pass the fake in instead",
		"internal/config tests-inject-fakes config_test.go:15 assigns package variable hook; pass the fake in instead",
		"internal/config tests-inject-fakes config_test.go:6 assigns package variable Mode; pass the fake in instead",
		"internal/other tests-inject-fakes other_test.go:10 assigns package variable cfg.Mode; pass the fake in instead",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q, want %q", got, want)
	}
}
