package mapcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const front = "---\npromise: x\n---\n# T\n\n"

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newMap builds a repo with a good empty map, a doc and the given capability.
func newMap(t *testing.T, product, capability string) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, ".physa/product.md", front+product)
	write(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: a thing.\n")
	write(t, root, ".physa/README.md", "hi\n")
	write(t, root, ".physa/capabilities/a.md", front+capability)
	write(t, root, "docs/d.md", "doc\n")
	return root
}

func check(t *testing.T, root string) Result {
	t.Helper()
	r, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func expect(t *testing.T, r Result, path string, line int, part string) {
	t.Helper()
	for _, p := range r.Problems {
		if p.Path == path && p.Line == line && strings.Contains(p.Message, part) {
			return
		}
	}
	t.Errorf("missing problem %s:%d containing %q in:\n%v", path, line, part, r.Problems)
}

const goodCap = "## Rules\n\n### R\n`r` · from docs/d.md\nBecause: y.\n\n## Decisions\n\n### D\n`d` · decided by Ann, accepting a proposal · 2026-01-02 · #3\nBecause: y.\n\n## Questions\n\n### Q\n`q` · raised by Ann · 2026-01-03\n"

func TestGoodMap(t *testing.T) {
	r := check(t, newMap(t, "", goodCap))
	if len(r.Problems) != 0 {
		t.Fatalf("want none, got %v", r.Problems)
	}
	if r.Capabilities != 1 || r.Entries != 3 {
		t.Errorf("got %d capabilities, %d entries", r.Capabilities, r.Entries)
	}
}

func TestMissingMapIsError(t *testing.T) {
	if _, err := Check(t.TempDir()); err == nil {
		t.Fatal("want an error for a missing .physa")
	}
}

func TestRuleChecks(t *testing.T) {
	// Lines: front is 5 lines; "## Rules" 6, blank 7, heading 8, meta 9.
	cap := "## Rules\n\n### R\n`r` · decided by Ann, in their words · 2026-01-02\n\n### S\n`s` · from docs/d.md\n"
	r := check(t, newMap(t, "", cap))
	const p = ".physa/capabilities/a.md"
	expect(t, r, p, 9, "carries no person")
	expect(t, r, p, 9, "carries no date")
	expect(t, r, p, 9, "at least one anchor")
	expect(t, r, p, 8, "`Because:`")
	expect(t, r, p, 11, "`Because:`")
	if len(r.Problems) != 5 {
		t.Errorf("want 5 problems, got %v", r.Problems)
	}
}

func TestDecisionChecks(t *testing.T) {
	const p = ".physa/capabilities/a.md"
	cap := "## Decisions\n\n### A\n`a` · decided by Ann, at random · 2026-01-02 · #1\nBecause: y.\n\n### B\n`b` · decided by Ann\nBecause: y.\n\n### C\n`c` · raised by Ann · 2026-01-02 · #1\nBecause: y.\n\n### D\n`d` · #1\n"
	r := check(t, newMap(t, "", cap))
	expect(t, r, p, 9, "needs how")
	expect(t, r, p, 13, "needs how")
	expect(t, r, p, 13, "needs a date")
	expect(t, r, p, 13, "needs at least one anchor")
	expect(t, r, p, 17, "raised by belongs on questions")
	expect(t, r, p, 21, "needs a date")
	expect(t, r, p, 20, "`Because:`")
}

func TestDecisionHowsPass(t *testing.T) {
	var b strings.Builder
	b.WriteString("## Decisions\n\n")
	for i, h := range hows {
		b.WriteString("### D\n`d" + string(rune('a'+i)) + "` · decided by Ann, " + h + " · 2026-01-02 · #1\nBecause: y.\n\n")
	}
	if r := check(t, newMap(t, "", b.String())); len(r.Problems) != 0 {
		t.Fatalf("want none, got %v", r.Problems)
	}
}

func TestQuestionChecks(t *testing.T) {
	const p = ".physa/capabilities/a.md"
	cap := "## Questions\n\n### A\n`a` · decided by Ann, in their words · 2026-01-02\n\n### B\n`b` · raised by Ann\n\n### C\n`c` · 2026-01-02\n"
	r := check(t, newMap(t, "", cap))
	expect(t, r, p, 9, "decided by belongs on decisions")
	expect(t, r, p, 12, "needs a date")
	expect(t, r, p, 15, "raised by")
}

func TestDuplicateSlug(t *testing.T) {
	prod := "## Rules\n\n### R\n`dup` · from docs/d.md\nBecause: y.\n"
	cap := "## Rules\n\n### R\n`dup` · from docs/d.md\nBecause: y.\n\n### S\n`dup` · from docs/d.md\nBecause: y.\n"
	r := check(t, newMap(t, prod, cap))
	const p = ".physa/capabilities/a.md"
	expect(t, r, p, 9, ".physa/product.md:8")
	expect(t, r, p, 13, ".physa/product.md:8")
	if len(r.Problems) != 2 {
		t.Errorf("want 2 problems, got %v", r.Problems)
	}
}

func TestDuplicateTerm(t *testing.T) {
	root := newMap(t, "", goodCap)
	write(t, root, ".physa/glossary.md", "# G\n\n**Map**: a.\n\n**map**: b.\n")
	expect(t, check(t, root), ".physa/glossary.md", 5, "already defined at line 3")
}

func TestDocAnchors(t *testing.T) {
	const p = ".physa/capabilities/a.md"
	cap := "## Rules\n\n### A\n`a` · from docs/nope.md\nBecause: y.\n\n### B\n`b` · from ../outside.md\nBecause: y.\n\n### C\n`c` · from /etc/passwd\nBecause: y.\n\n### D\n`d` · from docs\nBecause: y.\n\n### E\n`e` · from docs/d.md · 1a2b3c4 · #99999\nBecause: y.\n"
	root := newMap(t, "", cap)
	write(t, root, "outside.md", "x\n")
	r := check(t, root)
	expect(t, r, p, 9, "does not name a file")
	expect(t, r, p, 13, "stay inside the repo")
	expect(t, r, p, 17, "stay inside the repo")
	expect(t, r, p, 21, "does not name a file")
	expect(t, r, p, 25, `anchor "1a2b3c4" cannot resolve: not a git repository`)
	if len(r.Problems) != 5 {
		t.Errorf("want 5 problems, got %v", r.Problems)
	}
}

// gitIn runs git in dir, isolated from the user's configuration, and returns
// its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	full := append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitCap(hash string) string {
	return "## Rules\n\n### A\n`a` · from docs/d.md · " + hash + "\nBecause: y.\n"
}

func TestCommitAnchorsResolve(t *testing.T) {
	root := newMap(t, "", "")
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "first")
	full := gitIn(t, root, "rev-parse", "HEAD")
	for _, h := range []string{full[:7], full} {
		write(t, root, ".physa/capabilities/a.md", front+commitCap(h))
		if r := check(t, root); len(r.Problems) != 0 {
			t.Errorf("anchor %s: want none, got %v", h, r.Problems)
		}
	}
}

func TestCommitAnchorMissing(t *testing.T) {
	root := newMap(t, "", "")
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "first")
	write(t, root, ".physa/capabilities/a.md", front+commitCap("deadbee"))
	r := check(t, root)
	expect(t, r, ".physa/capabilities/a.md", 9, `anchor "deadbee" does not name a commit in this repo`)
	if len(r.Problems) != 1 || strings.Contains(r.Problems[0].Message, "shallow") {
		t.Errorf("want one problem without a shallow hint, got %v", r.Problems)
	}
}

// Without commit anchors no git is needed: a map with only #N anchors passes
// in a directory that is not a repo.
func TestPRAnchorsNeedNoGit(t *testing.T) {
	cap := "## Rules\n\n### A\n`a` · from docs/d.md · #12\nBecause: y.\n"
	if r := check(t, newMap(t, "", cap)); len(r.Problems) != 0 {
		t.Errorf("want none, got %v", r.Problems)
	}
}

func TestBudgets(t *testing.T) {
	pad := func(total, used int) string { return strings.Repeat("\n", total-used) }
	root := newMap(t, pad(MaxProductLines, 5), pad(MaxCapabilityLines, 5))
	if r := check(t, root); len(r.Problems) != 0 {
		t.Fatalf("at budget should pass, got %v", r.Problems)
	}
	root = newMap(t, pad(MaxProductLines+1, 5), pad(MaxCapabilityLines+1, 5))
	r := check(t, root)
	expect(t, r, ".physa/capabilities/a.md", 0, "151 lines, budget is 150: propose a merge or a prune")
	expect(t, r, ".physa/product.md", 0, "101 lines, budget is 100: propose a merge or a prune")
}

// The map has no limit on how many capabilities it holds.
func TestNoCapabilityCountLimit(t *testing.T) {
	root := newMap(t, "", "")
	for i := 0; i < 60; i++ {
		write(t, root, ".physa/capabilities/c"+strings.Repeat("x", i+1)+".md", front)
	}
	if r := check(t, root); len(r.Problems) != 0 {
		t.Errorf("61 capabilities should pass, got %v", r.Problems)
	}
}

func TestProblemsSorted(t *testing.T) {
	cap := "## Rules\n\n### A\n`a` · from docs/d.md\n\n### B\n`b` · from docs/d.md\n"
	r := check(t, newMap(t, "", cap))
	for i := 1; i < len(r.Problems); i++ {
		a, b := r.Problems[i-1], r.Problems[i]
		if a.Path > b.Path || (a.Path == b.Path && a.Line > b.Line) {
			t.Errorf("not sorted: %v before %v", a, b)
		}
	}
}

func TestReaderProblemsIncluded(t *testing.T) {
	root := newMap(t, "", goodCap)
	os.Remove(filepath.Join(root, ".physa/README.md"))
	expect(t, check(t, root), ".physa/README.md", 0, "")
}

func TestOwnMapPasses(t *testing.T) {
	r := check(t, "../..")
	for _, p := range r.Problems {
		t.Error(p)
	}
}

// TestBrokenCopy copies the real map and docs, breaks two things and checks
// both are reported, in order.
func TestBrokenCopy(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "../../.physa", filepath.Join(root, ".physa"))
	copyTree(t, "../../docs", filepath.Join(root, "docs"))
	prod := filepath.Join(root, ".physa/product.md")
	b, err := os.ReadFile(prod)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	var out []string
	name, becauseLine, slugLine := false, 0, 0
	for i, l := range lines {
		if l == "### Named Physa" {
			name = true
		}
		if name && strings.HasPrefix(l, "Because:") {
			name, becauseLine = false, i+1
			continue
		}
		out = append(out, l)
	}
	write(t, root, ".physa/product.md", strings.Join(out, "\n"))
	// Give the first entry of map.md the slug of product.md's first entry.
	cb, err := os.ReadFile(filepath.Join(root, ".physa/capabilities/map.md"))
	if err != nil {
		t.Fatal(err)
	}
	cl := strings.Split(string(cb), "\n")
	for i, l := range cl {
		if strings.HasPrefix(l, "`") && strings.HasPrefix(cl[i-1], "### ") {
			slugLine = i + 1
			cl[i] = "`agents-customer-side`" + l[strings.Index(l[1:], "`")+2:]
			break
		}
	}
	write(t, root, ".physa/capabilities/map.md", strings.Join(cl, "\n"))
	r := check(t, root)
	t.Logf("because line %d, slug line %d", becauseLine, slugLine)
	for _, p := range r.Problems {
		t.Log(p)
	}
	if len(r.Problems) != 2 {
		t.Fatalf("want 2 problems, got %v", r.Problems)
	}
	if r.Problems[0].Path != ".physa/capabilities/map.md" || r.Problems[1].Path != ".physa/product.md" {
		t.Errorf("unexpected order: %v", r.Problems)
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
