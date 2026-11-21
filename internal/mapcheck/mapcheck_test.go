package mapcheck

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// front is the head of a capability in the single-text shape, which folder
// splits into a folder; with no overview, the promise becomes it.
const front = "---\npromise: x\n---\n# T\n\n"

// The paths of capability a's files and the product's rules.
const (
	capDir   = ".physa/capabilities/a/"
	capRules = capDir + "rules.md"
)

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

// folder writes body, a capability's text after front in the single-text
// shape, as the folder dir, through mapfolder.Split.
func folder(t *testing.T, root, dir, body string) {
	t.Helper()
	f, err := mapfolder.Split(front + body)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range f.Files {
		write(t, root, dir+"/"+name, text)
	}
}

// newMap builds a repo with a good map, a doc and the given product and
// capability a, each in the single-text shape after front.
func newMap(t *testing.T, product, capability string) string {
	t.Helper()
	root := t.TempDir()
	folder(t, root, mapfolder.ProductDir, product)
	write(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: a thing.\n")
	write(t, root, ".physa/README.md", "hi\n")
	folder(t, root, mapfolder.CapabilityDir("a"), capability)
	write(t, root, "docs/d.md", "doc\n")
	return root
}

// lineOf is the line of the first line of rel that starts with prefix.
func lineOf(t *testing.T, root, rel, prefix string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, prefix) {
			return i + 1
		}
	}
	t.Fatalf("%s has no line starting with %q", rel, prefix)
	return 0
}

// check checks the map under root with the git adapter for commit anchors.
func check(t *testing.T, root string) Result {
	t.Helper()
	r, err := CheckWith(root, Options{OpenRepo: git.Open})
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

func TestMapWithoutCapabilitiesPasses(t *testing.T) {
	root := t.TempDir()
	folder(t, root, mapfolder.ProductDir, "")
	write(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: a thing.\n")
	write(t, root, ".physa/README.md", "hi\n")
	r := check(t, root)
	if len(r.Problems) != 0 {
		t.Fatalf("want none, got %v", r.Problems)
	}
	if r.Capabilities != 0 {
		t.Errorf("capabilities = %d, want 0", r.Capabilities)
	}
}

func TestMissingMapIsError(t *testing.T) {
	if _, err := Check(t.TempDir()); err == nil {
		t.Fatal("want an error for a missing .physa")
	}
}

func TestRuleChecks(t *testing.T) {
	cap := "## Rules\n\n### R\n`r` · decided by Ann, in their words · 2026-01-02\n\n### S\n`s` · from docs/d.md\n"
	root := newMap(t, "", cap)
	r := check(t, root)
	meta := lineOf(t, root, capRules, "`r` ·")
	expect(t, r, capRules, meta, "carries no person")
	expect(t, r, capRules, meta, "carries no date")
	expect(t, r, capRules, meta, "at least one anchor")
	expect(t, r, capRules, meta-1, "`Because:`")
	expect(t, r, capRules, lineOf(t, root, capRules, "### S"), "`Because:`")
	if len(r.Problems) != 5 {
		t.Errorf("want 5 problems, got %v", r.Problems)
	}
}

func TestDecisionChecks(t *testing.T) {
	const p = capDir + "decisions.md"
	cap := "## Decisions\n\n### A\n`a` · decided by Ann, at random · 2026-01-02 · #1\nBecause: y.\n\n### B\n`b` · decided by Ann\nBecause: y.\n\n### C\n`c` · raised by Ann · 2026-01-02 · #1\nBecause: y.\n\n### D\n`d` · #1\n"
	root := newMap(t, "", cap)
	r := check(t, root)
	meta := func(slug string) int { return lineOf(t, root, p, "`"+slug+"` ·") }
	expect(t, r, p, meta("a"), "needs how")
	expect(t, r, p, meta("b"), "needs how")
	expect(t, r, p, meta("b"), "needs a date")
	expect(t, r, p, meta("b"), "needs at least one anchor")
	expect(t, r, p, meta("c"), "raised by belongs on questions")
	expect(t, r, p, meta("d"), "needs a date")
	expect(t, r, p, meta("d")-1, "`Because:`")
}

func TestDecisionHowsPass(t *testing.T) {
	var b strings.Builder
	b.WriteString("## Decisions\n\n")
	for i, h := range hows {
		c := string(rune('a' + i))
		b.WriteString("### D" + c + "\n`d" + c + "` · decided by Ann, " + h + " · 2026-01-02 · #1\nBecause: y.\n\n")
	}
	if r := check(t, newMap(t, "", b.String())); len(r.Problems) != 0 {
		t.Fatalf("want none, got %v", r.Problems)
	}
}

func TestQuestionChecks(t *testing.T) {
	const p = capDir + "questions.md"
	cap := "## Questions\n\n### A\n`a` · decided by Ann, in their words · 2026-01-02\n\n### B\n`b` · raised by Ann\n\n### C\n`c` · 2026-01-02\n"
	root := newMap(t, "", cap)
	r := check(t, root)
	expect(t, r, p, lineOf(t, root, p, "`a` ·"), "decided by belongs on decisions")
	expect(t, r, p, lineOf(t, root, p, "`b` ·"), "needs a date")
	expect(t, r, p, lineOf(t, root, p, "`c` ·"), "raised by")
}

func TestDuplicateSlug(t *testing.T) {
	prod := "## Rules\n\n### R\n`dup` · from docs/d.md\nBecause: y.\n"
	cap := "## Rules\n\n### R\n`dup` · from docs/d.md\nBecause: y.\n\n### S\n`dup` · from docs/d.md\nBecause: y.\n"
	root := newMap(t, prod, cap)
	r := check(t, root)
	first := ".physa/product/rules.md:" + itoa(lineOf(t, root, ".physa/product/rules.md", "### R"))
	expect(t, r, capRules, lineOf(t, root, capRules, "`dup`"), first)
	expect(t, r, capRules, lineOf(t, root, capRules, "### S")+1, first)
	if len(r.Problems) != 2 {
		t.Errorf("want 2 problems, got %v", r.Problems)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestDuplicateTerm(t *testing.T) {
	root := newMap(t, "", goodCap)
	write(t, root, ".physa/glossary.md", "# G\n\n**Map**: a.\n\n**map**: b.\n")
	expect(t, check(t, root), ".physa/glossary.md", 5, "already defined at line 3")
}

func TestDocAnchors(t *testing.T) {
	cap := "## Rules\n\n### A\n`a` · from docs/nope.md\nBecause: y.\n\n### B\n`b` · from ../outside.md\nBecause: y.\n\n### C\n`c` · from /etc/passwd\nBecause: y.\n\n### D\n`d` · from docs\nBecause: y.\n\n### E\n`e` · from docs/d.md · 1a2b3c4 · #99999\nBecause: y.\n"
	root := newMap(t, "", cap)
	write(t, root, "outside.md", "x\n")
	r := check(t, root)
	meta := func(slug string) int { return lineOf(t, root, capRules, "`"+slug+"` ·") }
	expect(t, r, capRules, meta("a"), "does not name a file")
	expect(t, r, capRules, meta("b"), "stay inside the repo")
	expect(t, r, capRules, meta("c"), "stay inside the repo")
	expect(t, r, capRules, meta("d"), "does not name a file")
	expect(t, r, capRules, meta("e"), `anchor "1a2b3c4" cannot resolve: not a repository under version control`)
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
		folder(t, root, mapfolder.CapabilityDir("a"), commitCap(h))
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
	folder(t, root, mapfolder.CapabilityDir("a"), commitCap("deadbee"))
	r := check(t, root)
	expect(t, r, capRules, 10, `anchor "deadbee" does not name a commit in this repo`)
	if len(r.Problems) != 1 || strings.Contains(r.Problems[0].Message, "shallow") {
		t.Errorf("want one problem without a shallow hint, got %v", r.Problems)
	}
}

// Without version control, a commit anchor cannot resolve.
func TestCommitAnchorNoVersionControl(t *testing.T) {
	root := newMap(t, "", commitCap("1a2b3c4"))
	r, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, r, capRules, 10, `anchor "1a2b3c4" cannot resolve: no version control`)
	if len(r.Problems) != 1 {
		t.Errorf("want one problem, got %v", r.Problems)
	}
}

// The opener is called once, however many commit anchors the map has, and
// its error is each anchor's problem.
func TestCommitAnchorOpenerError(t *testing.T) {
	cap := commitCap("1a2b3c4") + "\n### B\n`b` · from docs/d.md · 5e6f7a8\nBecause: y.\n"
	root := newMap(t, "", cap)
	calls := 0
	open := func(string) (vcs.Repo, error) { calls++; return nil, errors.New("hg is not installed") }
	r := checkWith(t, root, Options{OpenRepo: open})
	if calls != 1 {
		t.Errorf("opener called %d times, want 1", calls)
	}
	expect(t, r, capRules, lineOf(t, root, capRules, "`a` ·"), `anchor "1a2b3c4" cannot resolve: hg is not installed`)
	expect(t, r, capRules, lineOf(t, root, capRules, "`b` ·"), `anchor "5e6f7a8" cannot resolve: hg is not installed`)
}

// An anchor to a change a shallow clone lacks carries the hint to fetch.
func TestCommitAnchorShallowHint(t *testing.T) {
	src := newMap(t, "", "")
	gitIn(t, src, "init", "-q")
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-q", "-m", "first")
	first := gitIn(t, src, "rev-parse", "HEAD")
	gitIn(t, src, "commit", "-q", "--allow-empty", "-m", "second")
	root := filepath.Join(t.TempDir(), "clone")
	gitIn(t, src, "clone", "-q", "--depth", "1", "file://"+src, root)
	folder(t, root, mapfolder.CapabilityDir("a"), commitCap(first[:7]))
	r := check(t, root)
	expect(t, r, capRules, 10, "(the clone is shallow; fetch its full history)")
}

// Without commit anchors no git is needed: a map with only #N anchors passes
// in a directory that is not a repo.
func TestPRAnchorsNeedNoGit(t *testing.T) {
	cap := "## Rules\n\n### A\n`a` · from docs/d.md · #12\nBecause: y.\n"
	if r := check(t, newMap(t, "", cap)); len(r.Problems) != 0 {
		t.Errorf("want none, got %v", r.Problems)
	}
}

// grow returns a rule with slug whose body is padded until its capability's rules.md,
// written by folder, has exactly n lines.
func grow(slug string, n int) string {
	head := "## Rules\n\n### R\n`" + slug + "` · from docs/d.md\n"
	// rules.md with one entry: 8 lines before the heading, then the
	// heading, the meta line, the body and Because:.
	return head + strings.Repeat("x\n", n-11) + "Because: y.\n"
}

// index is an index.md with an overview of n lines and links to kinds.
func index(n int, kinds ...mapfolder.Kind) string {
	return mapfolder.Index("---\npromise: x\n---", "T", strings.TrimSuffix(strings.Repeat("a line of the overview\n", n), "\n"), kinds)
}

func TestLimits(t *testing.T) {
	// index.md at 30 lines: 3 of front matter, a blank, the title, a blank,
	// the overview, a blank and one link.
	root := newMap(t, grow("p", MaxRulesLines), grow("r", MaxRulesLines))
	write(t, root, capDir+"index.md", index(MaxIndexLines-8, mapfolder.Rule))
	var long strings.Builder
	for i := range 400 {
		long.WriteString("### D" + itoa(i) + "\n`d" + itoa(i) + "` · decided by Ann, in their words · 2026-01-02 · #1\nBecause: y.\n\n")
	}
	folder(t, root, mapfolder.CapabilityDir("b"), "## Decisions\n\n"+long.String())
	if r := check(t, root); len(r.Problems) != 0 {
		t.Fatalf("at the limits should pass, got %v", r.Problems)
	}
	root = newMap(t, grow("p", MaxRulesLines+1), grow("r", MaxRulesLines+1))
	write(t, root, capDir+"index.md", index(MaxIndexLines-7, mapfolder.Rule))
	write(t, root, ".physa/product/index.md", index(MaxIndexLines-7, mapfolder.Rule))
	want := []string{
		capDir + "index.md: 31 lines, budget is 30: propose a merge, a prune, or a split into two capabilities",
		capRules + ": 151 lines, budget is 150: propose a merge, a prune, or a split into two capabilities",
		".physa/product/index.md: 31 lines, budget is 30: propose a merge, a prune, or a split into two capabilities",
		".physa/product/rules.md: 151 lines, budget is 150: propose a merge, a prune, or a split into two capabilities",
	}
	exact(t, check(t, root), want)
}

// exact fails unless r's problems are exactly want, as path:line: message.
func exact(t *testing.T, r Result, want []string) {
	t.Helper()
	var got []string
	for _, p := range r.Problems {
		got = append(got, p.String())
	}
	slices.Sort(got)
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(got, want) {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The map has no limit on how many capabilities it holds.
func TestNoCapabilityCountLimit(t *testing.T) {
	root := newMap(t, "", "")
	for i := 0; i < 60; i++ {
		folder(t, root, mapfolder.CapabilityDir("c"+strings.Repeat("x", i+1)), "")
	}
	if r := check(t, root); len(r.Problems) != 0 {
		t.Errorf("61 capabilities should pass, got %v", r.Problems)
	}
}

// The files of a good folder for capability a, each written as fmt writes
// it; formatCases change them one at a time.
const (
	goodIndex = "---\npromise: x\n---\n\n# T\n\nT does a thing.\n\n- [Rules](rules.md)\n- [Decisions](decisions.md)\n"
	// Lines: title 1, ## Contents 3, contents 5-6, ## Entries 8, R one 10,
	// R two 14.
	goodRules     = "# T · Rules\n\n## Contents\n\n- [R one](#r-one)\n- [R two](#r-two)\n\n## Entries\n\n" + ruleOne + "\n" + ruleTwo
	ruleOne       = "### R one\n`r1` · from docs/d.md\nBecause: y.\n"
	ruleTwo       = "### R two\n`r2` · from docs/d.md\nBecause: y.\n"
	goodDecisions = "# T · Decisions\n\n## Contents\n\n- [D](#d)\n\n## Entries\n\n### D\n`d` · decided by Ann, accepting a proposal · 2026-01-02 · #3\nBecause: y.\n"
	fmtHintTail   = "; run physa map fmt"
)

// rulesWith is goodRules with its two contents lines and its entries replaced.
func rulesWith(contents, entries string) string {
	return "# T · Rules\n\n## Contents\n\n" + contents + "\n## Entries\n\n" + entries
}

var formatCases = []struct {
	name  string
	files map[string]string // file name to text; "" removes the file
	want  []string          // after capDir
}{
	{"good", nil, nil},
	{"missing index", map[string]string{"index.md": ""}, []string{"index.md: missing file"}},
	{"empty kind file",
		map[string]string{"questions.md": "# T · Questions\n\n## Contents\n\n## Entries\n", "index.md": goodIndex + "- [Questions](questions.md)\n"},
		[]string{"questions.md: no entries: a kind with no entries has no file; delete it and its link in index.md"}},
	{"link to a missing file", map[string]string{"decisions.md": ""},
		[]string{"index.md:10: links decisions.md, which this folder does not have: remove the link"}},
	{"missing link", map[string]string{"index.md": strings.TrimSuffix(goodIndex, "- [Decisions](decisions.md)\n")},
		[]string{`index.md:9: missing the link "- [Decisions](decisions.md)" to decisions.md, which this folder has` + fmtHintTail}},
	{"links out of order", map[string]string{"index.md": "---\npromise: x\n---\n\n# T\n\nT does a thing.\n\n- [Decisions](decisions.md)\n- [Rules](rules.md)\n"},
		[]string{"index.md:10: link to rules.md is out of order: want rules.md, decisions.md, questions.md in that order" + fmtHintTail}},
	{"link twice", map[string]string{"index.md": goodIndex + "- [Rules](rules.md)\n"},
		[]string{"index.md:11: links rules.md twice" + fmtHintTail}},
	{"front matter in a kind file", map[string]string{"rules.md": "---\npromise: x\n---\n" + goodRules},
		[]string{"rules.md:1: front matter belongs only in index.md"}},
	{"index front matter not closed", map[string]string{"index.md": strings.Replace(goodIndex, "---\n\n", "\n", 1)},
		[]string{"index.md:1: front matter is not closed by a --- line"}},
	{"missing overview", map[string]string{"index.md": strings.Replace(goodIndex, "T does a thing.\n\n", "", 1)},
		[]string{"index.md:7: missing overview: want one paragraph after the title"}},
	{"second paragraph", map[string]string{"index.md": strings.Replace(goodIndex, "thing.\n", "thing.\n\nMore.\n", 1)},
		[]string{"index.md:9: a second paragraph: the overview is one paragraph"}},
	{"text after the links", map[string]string{"index.md": goodIndex + "\nMore.\n"},
		[]string{`index.md:12: "More." after the links: index.md ends with links to rules.md, decisions.md and questions.md`}},
	{"section in index", map[string]string{"index.md": strings.Replace(goodIndex, "thing.\n", "thing.\n\n## Notes\n", 1)},
		[]string{`index.md:9: unexpected heading "## Notes": want one title, then the overview`}},
	{"kind file title", map[string]string{"rules.md": strings.Replace(goodRules, "# T · Rules", "# T Rules", 1)},
		[]string{`rules.md:1: title "# T Rules": want "# T · Rules", the index's title and the kind`}},
	{"other section", map[string]string{"rules.md": goodRules + "\n## Notes\n"},
		[]string{`rules.md:18: section "## Notes": a kind file has only ## Contents, then ## Entries`}},
	{"section twice", map[string]string{"rules.md": goodRules + "\n## Entries\n"},
		[]string{`rules.md:18: "## Entries" a second time: a kind file has one ## Contents, then one ## Entries`}},
	{"missing contents section", map[string]string{"rules.md": "# T · Rules\n\n## Entries\n\n" + ruleOne + "\n" + ruleTwo},
		[]string{
			"rules.md:1: missing ## Contents: want ## Contents, then ## Entries" + fmtHintTail,
			`rules.md:5: entry "R one" is missing from the contents` + fmtHintTail,
			`rules.md:9: entry "R two" is missing from the contents` + fmtHintTail,
		}},
	{"entries before contents", map[string]string{"rules.md": "# T · Rules\n\n## Entries\n\n" + ruleOne + "\n" + ruleTwo + "\n## Contents\n\n- [R one](#r-one)\n- [R two](#r-two)\n"},
		[]string{"rules.md:13: ## Contents after ## Entries: want ## Contents, then ## Entries"}},
	{"entry outside entries", map[string]string{"rules.md": strings.Replace(goodRules, "## Contents\n", "### Stray\n`s` · from docs/d.md\nBecause: y.\n\n## Contents\n", 1)},
		[]string{
			"rules.md:3: ### heading outside ## Entries: every ### heading is an entry, and entries go under ## Entries",
			`rules.md:3: entry "Stray" is missing from the contents` + fmtHintTail,
		}},
	{"duplicate heading", map[string]string{"rules.md": rulesWith("- [R one](#r-one)\n- [R one](#r-one)\n", ruleOne+"\n"+strings.Replace(ruleTwo, "R two", "R one", 1))},
		[]string{
			`rules.md:6: contents line "R one" is there twice` + fmtHintTail,
			`rules.md:14: heading "R one" is already used at line 10: no two headings in a file may be the same`,
		}},
	{"same anchor", map[string]string{"rules.md": rulesWith("- [R one](#r-one)\n- [R-one](#r-one)\n", ruleOne+"\n"+strings.Replace(ruleTwo, "R two", "R-one", 1))},
		[]string{`rules.md:14: heading "R-one" has the anchor #r-one, as "R one" at line 10 does: reword one`}},
	{"anchor of a section", map[string]string{"rules.md": rulesWith("- [R one](#r-one)\n- [entries](#entries)\n", ruleOne+"\n"+strings.Replace(ruleTwo, "R two", "entries", 1))},
		[]string{`rules.md:14: heading "entries" has the anchor #entries, as "Entries" at line 8 does: reword one`}},
	{"heading breaks its contents line", map[string]string{"rules.md": rulesWith("- [R one](#r-one)\n- [R *two*](#r-two)\n", ruleOne+"\n"+strings.Replace(ruleTwo, "R two", "R *two*", 1))},
		[]string{`rules.md:14: heading "R *two*" contains "*", which breaks its contents line: reword it without [, ], <, > or *`}},
	{"contents line missing", map[string]string{"rules.md": rulesWith("- [R one](#r-one)\n", ruleOne+"\n"+ruleTwo)},
		[]string{`rules.md:13: entry "R two" is missing from the contents` + fmtHintTail}},
	{"stale heading", map[string]string{"rules.md": rulesWith("- [R one](#r-one)\n- [R 2](#r-2)\n", ruleOne+"\n"+ruleTwo)},
		[]string{
			`rules.md:6: contents line "R 2" names no entry in this file (a stale heading)` + fmtHintTail,
			`rules.md:14: entry "R two" is missing from the contents` + fmtHintTail,
		}},
	{"wrong link", map[string]string{"rules.md": rulesWith("- [R one](#r-one)\n- [R two](#r-2)\n", ruleOne+"\n"+ruleTwo)},
		[]string{`rules.md:6: contents line "R two" links #r-2, want #r-two` + fmtHintTail}},
	{"different order", map[string]string{"rules.md": rulesWith("- [R two](#r-two)\n- [R one](#r-one)\n", ruleOne+"\n"+ruleTwo)},
		[]string{`rules.md:6: contents line "R one" is out of order: the contents follow the entries' order` + fmtHintTail}},
	{"malformed contents line", map[string]string{"rules.md": rulesWith("- [R one](#r-one)\n- R two\n", ruleOne+"\n"+ruleTwo)},
		[]string{
			`rules.md:6: contents line "- R two": want - [<heading>](#<anchor>)` + fmtHintTail,
			`rules.md:14: entry "R two" is missing from the contents` + fmtHintTail,
		}},
}

// TestFolderFormat has one fixture per fault of the folder format (09 §2,
// §3), each changing one file of a good folder.
func TestFolderFormat(t *testing.T) {
	for _, tc := range formatCases {
		t.Run(tc.name, func(t *testing.T) {
			root := newMap(t, "", "")
			files := map[string]string{"index.md": goodIndex, "rules.md": goodRules, "decisions.md": goodDecisions}
			for name, text := range tc.files {
				files[name] = text
			}
			for name, text := range files {
				if text != "" {
					write(t, root, capDir+name, text)
				} else if err := os.Remove(filepath.Join(root, capDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			}
			var want []string
			for _, w := range tc.want {
				want = append(want, capDir+w)
			}
			exact(t, check(t, root), want)
		})
	}
}

// The product is held to the same format as a capability.
func TestProductFolderFormat(t *testing.T) {
	root := newMap(t, "", "")
	write(t, root, ".physa/product/index.md", strings.Replace(goodIndex, "# T", "# Product", 1))
	write(t, root, ".physa/product/rules.md", rulesWith("- [R two](#r-two)\n- [R one](#r-one)\n", ruleOne+"\n"+ruleTwo))
	write(t, root, ".physa/product/decisions.md", goodDecisions)
	exact(t, check(t, root), []string{
		`.physa/product/decisions.md:1: title "# T · Decisions": want "# Product · Decisions", the index's title and the kind`,
		`.physa/product/rules.md:1: title "# T · Rules": want "# Product · Rules", the index's title and the kind`,
		`.physa/product/rules.md:6: contents line "R one" is out of order: the contents follow the entries' order` + fmtHintTail,
	})
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
	// Take the Because: line out of the product's decision "Named Physa".
	const decisions = ".physa/product/decisions.md"
	b, err := os.ReadFile(filepath.Join(root, decisions))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	name, heading := false, 0
	for i, l := range strings.Split(string(b), "\n") {
		if l == "### Named Physa" {
			name, heading = true, i+1
		}
		if name && strings.HasPrefix(l, "Because:") {
			name = false
			continue
		}
		out = append(out, l)
	}
	write(t, root, decisions, strings.Join(out, "\n"))
	// Give the first rule of map the slug of the product's first rule.
	const rules = ".physa/capabilities/map/rules.md"
	cb, err := os.ReadFile(filepath.Join(root, rules))
	if err != nil {
		t.Fatal(err)
	}
	cl := strings.Split(string(cb), "\n")
	slugLine := 0
	for i, l := range cl {
		if strings.HasPrefix(l, "`") && strings.HasPrefix(cl[i-1], "### ") {
			slugLine = i + 1
			cl[i] = "`agents-customer-side`" + l[strings.Index(l[1:], "`")+2:]
			break
		}
	}
	write(t, root, rules, strings.Join(cl, "\n"))
	r := check(t, root)
	// The copy has no version control, so the live map's commit anchors
	// can't resolve here; only the two problems made above count.
	var problems []mapfile.Problem
	for _, p := range r.Problems {
		if !strings.Contains(p.Message, "cannot resolve: not a repository") {
			problems = append(problems, p)
		}
	}
	if len(problems) != 2 {
		t.Fatalf("want 2 problems, got %v", problems)
	}
	first := ".physa/product/rules.md:" + itoa(lineOf(t, root, ".physa/product/rules.md", "`agents-customer-side`")-1)
	if p := problems[0]; p.Path != rules || p.Line != slugLine || !strings.Contains(p.Message, first) {
		t.Errorf("want the slug problem at %s:%d naming %s, got %v", rules, slugLine, first, p)
	}
	if p := problems[1]; p.Path != decisions || p.Line != heading || !strings.Contains(p.Message, "`Because:`") {
		t.Errorf("want the Because: problem at %s:%d, got %v", decisions, heading, p)
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

func prCap(nums ...string) string {
	out := "## Rules\n\n"
	for i, n := range nums {
		out += "### R" + string(rune('A'+i)) + "\n`r" + string(rune('a'+i)) + "` · from docs/d.md · #" + n + "\nBecause: y.\n\n"
	}
	return out
}

func checkWith(t *testing.T, root string, o Options) Result {
	t.Helper()
	r, err := CheckWith(root, o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPRAnchorsResolved(t *testing.T) {
	root := newMap(t, "", prCap("10"))
	r := checkWith(t, root, Options{PRExists: func(n int) (bool, error) { return n == 10, nil }})
	if len(r.Problems) != 0 || r.PRAnchors != "github" || r.PRNote != "" {
		t.Errorf("want none and github, got %v %q %q", r.Problems, r.PRAnchors, r.PRNote)
	}
}

func TestPRAnchorUnknown(t *testing.T) {
	root := newMap(t, "", prCap("10", "999"))
	r := checkWith(t, root, Options{PRExists: func(n int) (bool, error) { return n == 10, nil }})
	expect(t, r, capRules, lineOf(t, root, capRules, "`rb` ·"), "anchor #999 names no PR in this repo")
	if len(r.Problems) != 1 || r.PRAnchors != "github" {
		t.Errorf("want one problem and github, got %v %q", r.Problems, r.PRAnchors)
	}
}

func TestPRAnchorResolvedOnce(t *testing.T) {
	root := newMap(t, "", prCap("10", "10", "11"))
	calls := map[int]int{}
	checkWith(t, root, Options{PRExists: func(n int) (bool, error) { calls[n]++; return true, nil }})
	if calls[10] != 1 || calls[11] != 1 {
		t.Errorf("want one call per number, got %v", calls)
	}
}

func TestPRAnchorsFallBackOnError(t *testing.T) {
	root := newMap(t, "", prCap("10", "999"))
	calls := 0
	r := checkWith(t, root, Options{PRExists: func(n int) (bool, error) { calls++; return false, errors.New("gh is not logged in") }})
	if len(r.Problems) != 0 || r.PRAnchors != "form-only" || r.PRNote != "gh is not logged in" || calls != 1 {
		t.Errorf("got %v %q %q after %d calls", r.Problems, r.PRAnchors, r.PRNote, calls)
	}
}

func TestPRAnchorsWithoutResolverAreFormOnly(t *testing.T) {
	r := check(t, newMap(t, "", prCap("10")))
	if r.PRAnchors != "form-only" || r.PRNote != "" {
		t.Errorf("got %q %q", r.PRAnchors, r.PRNote)
	}
}

func TestDocRootResolvesDocAnchors(t *testing.T) {
	cap := "## Rules\n\n### A\n`a` · from docs/a.md\nBecause: y.\n"
	root := newMap(t, "", cap)
	docs := t.TempDir()
	write(t, docs, "docs/a.md", "doc\n")
	r := checkWith(t, root, Options{})
	expect(t, r, capRules, 10, "anchor \"docs/a.md\" does not name a file in the repo")
	r = checkWith(t, root, Options{DocRoot: docs})
	if len(r.Problems) != 0 {
		t.Errorf("want no problems with DocRoot, got %v", r.Problems)
	}
}
