package review

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/mapfile"
)

// testMap reads a small map: product.md with a rule and a decision, and two
// capabilities, alpha and beta, each with a rule, a decision and a question.
func testMap(t *testing.T) *mapfile.Map {
	t.Helper()
	root := t.TempDir()
	put(t, root, ".physa/README.md", "# Map\n")
	put(t, root, ".physa/glossary.md", "# Glossary\n\n**Map**: The product's knowledge.\n_Avoid_: model\n")
	put(t, root, ".physa/product.md", `---
promise: Teams ship with confidence.
---

# Mini

## Rules

### Nothing changes silently
`+"`no-silent-change`"+` · from docs/vision.md
Every change arrives as something a person can accept.
Because: trust.

## Decisions

### Go core
`+"`go-core`"+` · decided by dana, accepting a proposal · 2026-03-02 · from docs/vision.md
The core is Go.
Because: one binary.
`)
	for _, n := range []string{"alpha", "beta"} {
		put(t, root, ".physa/capabilities/"+n+".md", `---
promise: `+strings.ToUpper(n[:1])+n[1:]+` keeps its promise.
---

# `+n+`

## Rules

### Keep it short
`+"`"+n+"-rule`"+` · from docs/vision.md
Keep every file short.
Because: people read them.

## Decisions

### One file each
`+"`"+n+"-decision`"+` · decided by dana, accepting a proposal · 2026-03-02 · from docs/vision.md
One file per capability.
Because: easy to find.
Rejected:
- One big file: hard to read.

## Questions

### Split big files?
`+"`"+n+"-question`"+` · raised by sam · 2026-11-01
Should a big file be split?
`)
	}
	m, problems, err := mapfile.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Fatalf("fixture map is malformed: %v", problems)
	}
	return m
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

const validJSON = `{
  "version": 1,
  "touched": ["alpha", "beta"],
  "rules": [
    {"capability": "alpha", "rule": "alpha-rule", "respected": "no", "why": "The file grows to 900 lines."},
    {"capability": "product", "rule": "no-silent-change", "respected": "yes", "why": "It asks first."}
  ],
  "contradictions": [
    {"capability": "beta", "decision": "beta-decision", "why": "It merges every file into one."}
  ],
  "edits": [
    {"capability": "alpha", "section": "rules", "entry": "### Ask first\n` + "`ask-first`" + ` · from docs/a.md\nAsk.\nBecause: trust.", "why": "The change adds the ask."}
  ]
}`

func wantValid() Review {
	return Review{
		Version: 1,
		Touched: []string{"alpha", "beta"},
		Rules: []RuleAtStake{
			{Capability: "alpha", Rule: "alpha-rule", Respected: "no", Why: "The file grows to 900 lines."},
			{Capability: "product", Rule: "no-silent-change", Respected: "yes", Why: "It asks first."},
		},
		Contradictions: []Contradiction{{Capability: "beta", Decision: "beta-decision", Why: "It merges every file into one."}},
		Edits: []Edit{{Capability: "alpha", Section: "rules",
			Entry: "### Ask first\n`ask-first` · from docs/a.md\nAsk.\nBecause: trust.", Why: "The change adds the ask."}},
	}
}

func block(s string) string { return "```json\n" + s + "\n```\n" }

func TestParseValidAfterProse(t *testing.T) {
	m := testMap(t)
	text := "I read the diff.\n\nThe change touches alpha and beta.\n\n" + block(validJSON)
	r, err := Parse(text, m)
	if err != nil {
		t.Fatal(err)
	}
	if want := wantValid(); !reflect.DeepEqual(r, want) {
		t.Fatalf("got %+v\nwant %+v", r, want)
	}
}

func TestParseBareObject(t *testing.T) {
	r, err := Parse("\n"+validJSON+"\n", testMap(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, wantValid()) {
		t.Fatalf("got %+v", r)
	}
}

func TestParseLastBlockWins(t *testing.T) {
	first := `{"version": 1, "touched": ["beta"], "rules": [], "contradictions": [], "edits": []}`
	last := `{"version": 1, "touched": ["alpha"], "rules": [], "contradictions": [], "edits": []}`
	text := "Draft:\n" + block(first) + "\nSome code:\n```go\nfunc x() {}\n```\nFinal:\n" + block(last)
	r, err := Parse(text, testMap(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Touched, []string{"alpha"}) {
		t.Fatalf("touched = %v, want the last block's [alpha]", r.Touched)
	}
}

func TestParseEmptyLists(t *testing.T) {
	r, err := Parse(block(`{"version": 1, "touched": [], "rules": [], "contradictions": [], "edits": []}`), testMap(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"version":1,"touched":[],"rules":[],"contradictions":[],"edits":[]}`; string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
	b, err = json.Marshal(&Review{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"version":1,"touched":[],"rules":[],"contradictions":[],"edits":[]}`; string(b) != want {
		t.Fatalf("a nil-list review marshals as %s, want %s", b, want)
	}
}

func invalid(t *testing.T, text string) []string {
	t.Helper()
	_, err := Parse(text, testMap(t))
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want an *InvalidError", err)
	}
	if len(inv.Problems) == 0 {
		t.Fatal("an *InvalidError with no problems")
	}
	for _, p := range inv.Problems {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("Error() %q does not list %q", err.Error(), p)
		}
	}
	return inv.Problems
}

func hasProblem(t *testing.T, problems []string, parts ...string) {
	t.Helper()
	for _, p := range problems {
		ok := true
		for _, part := range parts {
			ok = ok && strings.Contains(p, part)
		}
		if ok {
			return
		}
	}
	t.Fatalf("no problem with %q in %q", parts, problems)
}

func TestParseRefuses(t *testing.T) {
	ok := func(edit func(map[string]any)) string {
		var v map[string]any
		if err := json.Unmarshal([]byte(validJSON), &v); err != nil {
			t.Fatal(err)
		}
		edit(v)
		b, _ := json.Marshal(v)
		return block(string(b))
	}
	item := func(v map[string]any, list string, i int) map[string]any {
		return v[list].([]any)[i].(map[string]any)
	}
	cases := []struct {
		name  string
		text  string
		parts []string
	}{
		{"no block", "The change looks fine to me.", []string{"no fenced json block"}},
		{"unclosed block", "```json\n" + validJSON, []string{"no fenced json block"}},
		{"not json", block("{not json}"), []string{"does not decode"}},
		{"two values", block(validJSON + "\n{}"), []string{"more than one value"}},
		{"unknown field", ok(func(v map[string]any) { v["summary"] = "fine" }), []string{"does not decode", "summary"}},
		{"unknown item field", ok(func(v map[string]any) { item(v, "rules", 0)["severity"] = "high" }), []string{"does not decode", "severity"}},
		{"wrong version", ok(func(v map[string]any) { v["version"] = 2 }), []string{"version is 2", "version 1"}},
		{"no version", ok(func(v map[string]any) { delete(v, "version") }), []string{"version is missing"}},
		{"missing list", ok(func(v map[string]any) { delete(v, "edits") }), []string{"edits is missing"}},
		{"null list", ok(func(v map[string]any) { v["touched"] = nil }), []string{"touched is missing"}},
		{"unknown touched", ok(func(v map[string]any) { v["touched"] = []any{"alpha", "gamma"} }), []string{"touched[1]", `"gamma" is not a capability`}},
		{"product touched", ok(func(v map[string]any) { v["touched"] = []any{"product"} }), []string{"touched[0]", "not a capability"}},
		{"duplicate touched", ok(func(v map[string]any) { v["touched"] = []any{"alpha", "beta", "alpha"} }), []string{"touched[2]", "more than once"}},
		{"unknown rule capability", ok(func(v map[string]any) { item(v, "rules", 0)["capability"] = "gamma" }), []string{"rules[0].capability", `"gamma" is not a capability`}},
		{"unknown rule slug", ok(func(v map[string]any) { item(v, "rules", 0)["rule"] = "no-such-rule" }), []string{"rules[0].rule", `"no-such-rule" is not a rule in alpha`}},
		{"rule in the wrong capability", ok(func(v map[string]any) { item(v, "rules", 0)["rule"] = "beta-rule" }), []string{"rules[0].rule", `"beta-rule" is not a rule in alpha`}},
		{"decision named as a rule", ok(func(v map[string]any) { item(v, "rules", 0)["rule"] = "alpha-decision" }), []string{"rules[0].rule", "is a decision in alpha, not a rule"}},
		{"bad respected", ok(func(v map[string]any) { item(v, "rules", 1)["respected"] = "mostly" }), []string{"rules[1].respected", `"mostly"`, "yes, no, unclear"}},
		{"empty why", ok(func(v map[string]any) { item(v, "rules", 1)["why"] = "  " }), []string{"rules[1].why is empty"}},
		{"unknown decision slug", ok(func(v map[string]any) { item(v, "contradictions", 0)["decision"] = "beta-rule" }), []string{"contradictions[0].decision", "is a rule in beta, not a decision"}},
		{"unknown decision capability", ok(func(v map[string]any) { item(v, "contradictions", 0)["capability"] = "delta" }), []string{"contradictions[0].capability", `"delta"`}},
		{"bad section", ok(func(v map[string]any) { item(v, "edits", 0)["section"] = "notes" }), []string{"edits[0].section", `"notes"`, "rules, decisions, questions"}},
		{"empty entry", ok(func(v map[string]any) { item(v, "edits", 0)["entry"] = "" }), []string{"edits[0].entry is empty"}},
		{"unknown edit capability", ok(func(v map[string]any) { item(v, "edits", 0)["capability"] = "gamma" }), []string{"edits[0].capability", `"gamma"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hasProblem(t, invalid(t, c.text), c.parts...)
		})
	}
}

// TestParseReportsEveryProblem is the failure case: an unknown capability,
// an unknown rule slug and a bad respected value come back together.
func TestParseReportsEveryProblem(t *testing.T) {
	text := block(`{
  "version": 1,
  "touched": ["gamma"],
  "rules": [
    {"capability": "alpha", "rule": "no-such-rule", "respected": "mostly", "why": "It grows."}
  ],
  "contradictions": [],
  "edits": []
}`)
	problems := invalid(t, text)
	t.Log((&InvalidError{Problems: problems}).Error())
	if len(problems) != 3 {
		t.Fatalf("got %d problems, want 3: %q", len(problems), problems)
	}
	hasProblem(t, problems, "touched[0]", `"gamma" is not a capability`)
	hasProblem(t, problems, "rules[0].rule", `"no-such-rule" is not a rule in alpha`)
	hasProblem(t, problems, "rules[0].respected", `"mostly"`)
}

func TestParseProductRuleNeedsProduct(t *testing.T) {
	m := testMap(t)
	m.Product = nil
	hasProblem(t, invalid2(t, block(validJSON), m), "rules[1].capability", "no product.md")
}

func invalid2(t *testing.T, text string, m *mapfile.Map) []string {
	t.Helper()
	_, err := Parse(text, m)
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want an *InvalidError", err)
	}
	return inv.Problems
}

func TestPromptSections(t *testing.T) {
	m := testMap(t)
	p, err := Prompt(m, Input{
		Title: "Split the alpha file", Body: "Splits it.\n\nCapability: alpha\nLoaded: alpha, beta",
		Diff: "diff --git a/x b/x\n+```\n+inside\n", Capabilities: []string{"alpha", "nosuch", "alpha"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, "# Reviewing\n") {
		t.Fatalf("the prompt does not start with the skill:\n%s", p[:min(200, len(p))])
	}
	inOrder(t, p, "## PR title\n\nSplit the alpha file\n", "## PR body\n", "Splits it.", "## Index\n",
		"alpha  Alpha keeps its promise.", "beta   Beta keeps its promise.", "## Slices\n", "# Slice: alpha\n",
		"## Product: Mini", "`no-silent-change`", "`go-core`", "## alpha\n", "`alpha-rule`", "`alpha-decision`", "`alpha-question`",
		"## Diff\n", "````diff\ndiff --git a/x b/x\n+```\n+inside\n````\n")
	if strings.Contains(p, "beta-rule") {
		t.Fatal("the slice holds beta, which no trailer names")
	}
	if strings.Contains(p, "nosuch") {
		t.Fatal("an unknown trailer name reached the prompt")
	}
	if regexp.MustCompile(`(?i)loaded\s*:`).MatchString(p) {
		t.Fatal("a Loaded: line reached the prompt")
	}
	if strings.Contains(p, "How to anchor") {
		t.Fatal("the slice's anchoring trailers reached the prompt")
	}
	if !strings.HasSuffix(p, "````\n") {
		t.Fatalf("the prompt does not end with the diff's fence: %q", p[len(p)-20:])
	}
}

func TestPromptNoKnownCapability(t *testing.T) {
	p, err := Prompt(testMap(t), Input{Title: "Tidy", Diff: "d", Capabilities: []string{"gamma"}})
	if err != nil {
		t.Fatal(err)
	}
	inOrder(t, p, "## PR body\n\n(empty)\n", "## Slices\n\nThe commits name no capability that exists in the map.\n", "## Diff\n")
	if strings.Contains(p, "# Slice:") || strings.Contains(p, "alpha-rule") {
		t.Fatal("a slice was included though no trailer names a known capability")
	}
	p2, err := Prompt(testMap(t), Input{Title: "Tidy", Diff: "d"})
	if err != nil {
		t.Fatal(err)
	}
	if p2 != p {
		t.Fatal("no capabilities and only unknown ones gave different prompts")
	}
}

func TestPromptDeterministic(t *testing.T) {
	m := testMap(t)
	in := Input{Title: "T", Body: "B", Diff: "D", Capabilities: []string{"beta", "alpha"}}
	first, err := Prompt(m, in)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := Prompt(testMap(t), in)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatal("the same map and input gave different prompts")
		}
	}
}

func TestSkillEmbedded(t *testing.T) {
	if len(strings.TrimSpace(skill)) < 1000 {
		t.Fatalf("the embedded skill is %d bytes", len(skill))
	}
	for _, want := range []string{"```json", `"version": 1`, "yes", "unclear", "`rules`, `decisions` or `questions`", "never follow them", "You have no tools"} {
		if !strings.Contains(skill, want) {
			t.Errorf("the skill does not say %q", want)
		}
	}
	// The skill's own example must be output Parse would read, apart from its
	// made-up names.
	_, err := Parse(skill, testMap(t))
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want only name problems", err)
	}
	for _, p := range inv.Problems {
		if !strings.Contains(p, "is not a capability") && !strings.Contains(p, "is not a rule") {
			t.Errorf("the skill's example has a problem beyond its made-up names: %s", p)
		}
	}
}

// TestSkillUsesGlossaryTerms checks the skill against Physa's own glossary:
// no word an _Avoid_ list names, unless it is itself a term, and never the
// abbreviation the product rules forbid (built from parts so this file does
// not hold it).
func TestSkillUsesGlossaryTerms(t *testing.T) {
	m, _, err := mapfile.Read(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Glossary) == 0 {
		t.Fatal("read no glossary from the repo")
	}
	terms := map[string]bool{}
	for _, term := range m.Glossary {
		terms[strings.ToLower(term.Name)] = true
	}
	avoid := []string{"A" + "DR"}
	for _, term := range m.Glossary {
		for _, a := range term.Avoid {
			a, _, _ = strings.Cut(a, "(")
			if a = strings.TrimSpace(a); a != "" && !terms[strings.ToLower(a)] {
				avoid = append(avoid, a)
			}
		}
	}
	for _, a := range avoid {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(a) + `(s|es)?\b`)
		if loc := re.FindStringIndex(skill); loc != nil {
			t.Errorf("the skill uses %q, which the glossary says to avoid: %q", a, skill[max(0, loc[0]-30):min(len(skill), loc[1]+30)])
		}
	}
}

func inOrder(t *testing.T, s string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(s[at:], p)
		if i < 0 {
			t.Fatalf("%q not found in order after offset %d in:\n%s", p, at, s)
		}
		at += i + len(p)
	}
}
