package mapfolder

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAnchor(t *testing.T) {
	for _, c := range []struct{ heading, want string }{
		{"GitHub first", "github-first"},
		{"Installing a hook never disables the repo's own", "installing-a-hook-never-disables-the-repos-own"},
		{"Teams can't replace Physa's skills", "teams-cant-replace-physas-skills"},
		{"What does the person see while init runs?", "what-does-the-person-see-while-init-runs"},
		{"Engineering evidence from git and CI; runtime through OpenTelemetry", "engineering-evidence-from-git-and-ci-runtime-through-opentelemetry"},
		{"One server next to production, no SDK of our own", "one-server-next-to-production-no-sdk-of-our-own"},
		{`Count "needed a person"`, "count-needed-a-person"},
		{"Earned release is opt-in", "earned-release-is-opt-in"},
		{"`physa init` refuses: a mapped repo (always)", "physa-init-refuses-a-mapped-repo-always"},
		{"Stakes — worked out", "stakes--worked-out"},
		{"snake_case stays", "snake_case-stays"},
		{"Café über 2 maps", "café-über-2-maps"},
	} {
		if got := Anchor(c.heading); got != c.want {
			t.Errorf("Anchor(%q) = %q, want %q", c.heading, got, c.want)
		}
	}
}

func TestContents(t *testing.T) {
	got := Contents([]string{"GitHub first", "`physa map` is fast"})
	want := []string{"- [GitHub first](#github-first)", "- [`physa map` is fast](#physa-map-is-fast)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Contents = %q, want %q", got, want)
	}
}

func TestHeadingProblem(t *testing.T) {
	if p := HeadingProblem("Teams can't replace Physa's skills"); p != "" {
		t.Errorf("plain heading: %q", p)
	}
	for _, h := range []string{"A [link]", "x]", "a <b>", "c > d", "*bold*"} {
		if HeadingProblem(h) == "" {
			t.Errorf("HeadingProblem(%q) is empty", h)
		}
	}
}

const ruleA = "### Uncertain knowledge becomes a question\n`question-not-guess` · from docs/a.md\nWrite a question.\nBecause: trust."
const ruleB = "### A known footprint\n`init-footprint` · from docs/a.md\nOnly the map.  \n\n```\n### not a heading\n## nor this\n```\nBecause: teams."

func TestKindFile(t *testing.T) {
	got := KindFile("Init", Rule, []string{ruleA, ruleB + "\n\n"})
	want := "# Init · Rules\n\n## Contents\n\n" +
		"- [Uncertain knowledge becomes a question](#uncertain-knowledge-becomes-a-question)\n" +
		"- [A known footprint](#a-known-footprint)\n\n## Entries\n\n" + ruleA + "\n\n" + ruleB + "\n"
	if got != want {
		t.Errorf("KindFile =\n%s\nwant\n%s", got, want)
	}
	if got := KindFile("Init", Question, nil); got != "# Init · Questions\n\n## Contents\n\n## Entries\n" {
		t.Errorf("empty KindFile = %q", got)
	}
}

func TestIndex(t *testing.T) {
	got := Index("---\npromise: A map.\nstewards: [dphaener]\n---\n", "Init", "Init maps a repo.\nIt writes a PR.", []Kind{Question, Rule})
	want := "---\npromise: A map.\nstewards: [dphaener]\n---\n\n# Init\n\nInit maps a repo.\nIt writes a PR.\n\n- [Rules](rules.md)\n- [Questions](questions.md)\n"
	if got != want {
		t.Errorf("Index =\n%s\nwant\n%s", got, want)
	}
}

// sourceEntries cuts each entry out of a single text: its heading through its
// last non-blank line, fence-aware.
func sourceEntries(text string) []string {
	var out []string
	var cur []string
	fenced := false
	flush := func() {
		for len(cur) > 0 && strings.TrimSpace(cur[len(cur)-1]) == "" {
			cur = cur[:len(cur)-1]
		}
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
		}
		cur = nil
	}
	for _, s := range strings.Split(text, "\n") {
		switch {
		case fenced:
		case strings.HasPrefix(s, "### "):
			flush()
		case strings.HasPrefix(s, "#"):
			flush()
			continue
		}
		if strings.HasPrefix(s, "```") {
			fenced = !fenced
		}
		if cur != nil || strings.HasPrefix(s, "### ") {
			cur = append(cur, s)
		}
	}
	flush()
	return out
}

func TestSplitInit(t *testing.T) {
	b, err := os.ReadFile("testdata/init.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	f, err := Split(text)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range f.Files {
		names = append(names, n)
	}
	if len(names) != 4 {
		t.Fatalf("files = %v, want index.md and the three kind files", names)
	}
	var got []string
	for _, k := range Kinds {
		got = append(got, sourceEntries(f.Files[k.File()])...)
		if !strings.HasPrefix(f.Files[k.File()], "# Init · "+k.Word()+"\n\n## Contents\n\n- [") {
			t.Errorf("%s starts %q", k.File(), f.Files[k.File()][:40])
		}
	}
	want := sourceEntries(text)
	if len(want) != 11 || !reflect.DeepEqual(got, want) {
		t.Errorf("entries differ from the source: got %d, want %d", len(got), len(want))
	}
	index := f.Files[IndexFile]
	wantIndex := "---\npromise: A team with an existing codebase gets a cited, reviewable map in minutes, without writing anything first.\nstewards: [dphaener]\n---\n\n# Init\n\n" +
		"A team with an existing codebase gets a cited, reviewable map in minutes, without writing anything first.\n\n" +
		"- [Rules](rules.md)\n- [Decisions](decisions.md)\n- [Questions](questions.md)\n"
	if index != wantIndex {
		t.Errorf("index.md =\n%s\nwant\n%s", index, wantIndex)
	}
	src := strings.Split(text, "\n")
	for name, body := range f.Files {
		ls := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
		if len(f.Source[name]) != len(ls) {
			t.Fatalf("%s: %d sources for %d lines", name, len(f.Source[name]), len(ls))
		}
		for i, at := range f.Source[name] {
			if at == 0 {
				continue
			}
			from := src[at-1]
			if name == IndexFile && i == 7 {
				from = strings.TrimPrefix(from, "promise: ")
			}
			if ls[i] != from && !strings.HasPrefix(ls[i], from+" · ") {
				t.Errorf("%s:%d = %q, but its source line %d is %q", name, i+1, ls[i], at, from)
			}
		}
	}
	for _, body := range f.Files {
		if again, err := Format(IndexFile, body); strings.HasPrefix(body, "---") && (err != nil || again != body) {
			t.Errorf("index.md is not formatted: %v", err)
		}
	}
	for _, k := range Kinds {
		if again, err := Format(k.File(), f.Files[k.File()]); err != nil || again != f.Files[k.File()] {
			t.Errorf("%s is not formatted: %v", k.File(), err)
		}
	}
}

func TestSplitOverviewAndFence(t *testing.T) {
	text := "---\npromise: \"Quoted.\"\n---\n\n# Map\n\nThe map is read.\nIt is checked.\n\n## Decisions\n\n" + ruleB + "\n"
	f, err := Split(text)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Files[RulesFile]; ok {
		t.Error("rules.md written for a capability without rules")
	}
	if !strings.Contains(f.Files[IndexFile], "\n# Map\n\nThe map is read.\nIt is checked.\n\n- [Decisions](decisions.md)\n") {
		t.Errorf("index.md = %q", f.Files[IndexFile])
	}
	want := KindFile("Map", Decision, []string{ruleB})
	if f.Files[DecisionsFile] != want {
		t.Errorf("decisions.md =\n%s\nwant\n%s", f.Files[DecisionsFile], want)
	}
	if got := f.Source[IndexFile][6]; got != 7 {
		t.Errorf("overview line source = %d, want 7", got)
	}
	f, err = Split("---\npromise: \"Quoted.\"\n---\n\n# Map\n")
	if err != nil {
		t.Fatal(err)
	}
	if f.Files[IndexFile] != "---\npromise: \"Quoted.\"\n---\n\n# Map\n\n\"Quoted.\"\n" {
		t.Errorf("promise as overview: %q", f.Files[IndexFile])
	}
}

func TestSplitErrors(t *testing.T) {
	for _, c := range []struct {
		name, text string
		line       int
		message    string
	}{
		{"entry outside a section", "---\npromise: P.\n---\n\n# Init\n\nOverview.\n\n### Stray\n`stray` · from a.md\n", 9, "entry outside a Rules, Decisions or Questions section"},
		{"unclosed front matter", "---\npromise: P.\n\n# Init\n\n## Rules\n", 1, "front matter is not closed by a --- line"},
		{"no front matter", "# Init\n", 1, "missing front matter: want --- on the first line"},
		{"two paragraphs", "---\npromise: P.\n---\n\n# Init\n\nOne.\n\nTwo.\n\n## Rules\n", 9, "a second paragraph: the overview is one paragraph"},
		{"unknown section", "---\npromise: P.\n---\n# Init\n## Notes\n", 5, `unknown section "Notes": want Rules, Decisions or Questions`},
		{"repeated section", "---\npromise: P.\n---\n# Init\n## Rules\n## Rules\n", 6, `section "Rules" appears more than once`},
		{"text in a section", "---\npromise: P.\n---\n# Init\n## Rules\nprose\n", 6, "text before the first entry of the section"},
		{"second title", "---\npromise: P.\n---\n# Init\n## Rules\n# Again\n", 6, "more than one title"},
	} {
		_, err := Split(c.text)
		var se *SplitError
		if !errors.As(err, &se) {
			t.Errorf("%s: err = %v, want a SplitError", c.name, err)
			continue
		}
		if se.Line != c.line || se.Message != c.message {
			t.Errorf("%s: got line %d %q, want line %d %q", c.name, se.Line, se.Message, c.line, c.message)
		}
		t.Logf("%s: SplitError line %d: %s", c.name, se.Line, se.Message)
	}
}

// messy is a decisions file with stale contents and loose spacing, and the
// entries that must survive byte for byte.
const messy = "\n# Init · Decisions\n## Contents\n- [Old heading](#old-heading)\n\n\n## Entries\n" + ruleA + "\n\n\n\n" + ruleB + "\n\n\n"

func TestFormatKindFile(t *testing.T) {
	got, err := Format(".physa/capabilities/init/decisions.md", messy)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Init · Decisions\n\n## Contents\n\n" +
		"- [Uncertain knowledge becomes a question](#uncertain-knowledge-becomes-a-question)\n" +
		"- [A known footprint](#a-known-footprint)\n\n## Entries\n\n" + ruleA + "\n\n" + ruleB + "\n"
	if got != want {
		t.Errorf("Format =\n%q\nwant\n%q", got, want)
	}
	for _, e := range []string{ruleA, ruleB} {
		if !strings.Contains(got, e+"\n") {
			t.Errorf("entry changed: %q", e)
		}
	}
	again, err := Format("decisions.md", got)
	if err != nil || again != got {
		t.Errorf("Format is not idempotent: %v\n%q", err, again)
	}
	got, err = Format("rules.md", "# Init · Rules\n## Entries\n"+ruleA)
	if err != nil || got != KindFile("Init", Rule, []string{ruleA}) {
		t.Errorf("missing contents not added: %v\n%q", err, got)
	}
}

func TestFormatIndex(t *testing.T) {
	text := "---\npromise: P.\n---\n# Init\n\n\nInit maps a repo.  \nIt writes a PR.\n- [Questions](questions.md)\n- [Rules](rules.md)\n\n"
	got, err := Format(IndexFile, text)
	want := "---\npromise: P.\n---\n\n# Init\n\nInit maps a repo.  \nIt writes a PR.\n\n- [Rules](rules.md)\n- [Questions](questions.md)\n"
	if err != nil || got != want {
		t.Errorf("Format(index.md) = %v\n%q\nwant\n%q", err, got, want)
	}
	got, err = FormatIndex(text, []Kind{Decision})
	if err != nil || !strings.HasSuffix(got, "It writes a PR.\n\n- [Decisions](decisions.md)\n") {
		t.Errorf("FormatIndex = %v\n%q", err, got)
	}
	if again, _ := FormatIndex(got, []Kind{Decision}); again != got {
		t.Errorf("FormatIndex is not idempotent: %q", again)
	}
}

func TestFormatRefusesToDropText(t *testing.T) {
	for _, c := range []struct {
		name, file, text string
		line             int
	}{
		{"no entries section", RulesFile, "# Init · Rules\n\n## Contents\n\n" + ruleA + "\n", 0},
		{"prose before contents", RulesFile, "# Init · Rules\n\nA note.\n\n## Contents\n\n## Entries\n", 3},
		{"prose in contents", RulesFile, "# Init · Rules\n\n## Contents\n\nA note.\n\n## Entries\n", 5},
		{"prose before the first entry", RulesFile, "# Init · Rules\n\n## Contents\n\n## Entries\n\nA note.\n\n" + ruleA + "\n", 7},
		{"a section in entries", RulesFile, "# Init · Rules\n\n## Contents\n\n## Entries\n\n" + ruleA + "\n\n## More\n", 12},
		{"front matter in a kind file", RulesFile, "---\npromise: P.\n---\n# Init · Rules\n", 1},
		{"second index paragraph", IndexFile, "---\npromise: P.\n---\n\n# Init\n\nOne.\n\nTwo.\n", 9},
		{"text after the links", IndexFile, "---\npromise: P.\n---\n\n# Init\n\nOne.\n\n- [Rules](rules.md)\n\nMore.\n", 11},
		{"index section", IndexFile, "---\npromise: P.\n---\n\n# Init\n\nOne.\n\n## Rules\n", 9},
		{"unknown link", IndexFile, "---\npromise: P.\n---\n\n# Init\n\nOne.\n\n- [Notes](notes.md)\n", 9},
		{"no overview", IndexFile, "---\npromise: P.\n---\n\n# Init\n\n- [Rules](rules.md)\n", 7},
	} {
		_, err := Format(c.file, c.text)
		var fe *FormatError
		if !errors.As(err, &fe) {
			t.Errorf("%s: err = %v, want a FormatError", c.name, err)
			continue
		}
		if c.line != 0 && fe.Line != c.line {
			t.Errorf("%s: line %d (%s), want %d", c.name, fe.Line, fe.Message, c.line)
		}
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFormatMap(t *testing.T) {
	root := t.TempDir()
	good := KindFile("Init", Rule, []string{ruleA})
	stale := strings.Replace(good, "- [Uncertain knowledge becomes a question]", "- [Uncertain knowledge]", 1)
	stray := "# Map · Rules\n\nA note someone wrote.\n\n## Contents\n\n## Entries\n\n" + ruleA + "\n"
	index := Index("---\npromise: P.\n---", "Init", "Init maps a repo.", []Kind{Rule})
	writeFiles(t, root, map[string]string{
		".physa/product/index.md":              Index("---\npromise: P.\n---", "Physa", "The product.", nil),
		".physa/capabilities/init/index.md":    index,
		".physa/capabilities/init/rules.md":    stale,
		".physa/capabilities/map/index.md":     Index("---\npromise: P.\n---", "Map", "The map.", nil),
		".physa/capabilities/map/rules.md":     stray,
		".physa/capabilities/map/questions.md": KindFile("Map", Question, []string{ruleA}),
	})
	changed, problems, err := FormatMap(root, false)
	if err != nil {
		t.Fatal(err)
	}
	wantChanged := []string{".physa/capabilities/init/rules.md", ".physa/capabilities/map/index.md"}
	if !reflect.DeepEqual(changed, wantChanged) {
		t.Errorf("changed = %q, want %q", changed, wantChanged)
	}
	wantProblems := []Problem{{".physa/capabilities/map/rules.md", 3, "text between the title and ## Contents"}}
	if !reflect.DeepEqual(problems, wantProblems) {
		t.Errorf("problems = %v, want %v", problems, wantProblems)
	}
	if read(t, filepath.Join(root, ".physa/capabilities/init/rules.md")) != stale {
		t.Error("FormatMap wrote without write")
	}
	if _, _, err := FormatMap(root, true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, ".physa/capabilities/init/rules.md")); got != good {
		t.Errorf("rules.md not fixed:\n%s", got)
	}
	if got := read(t, filepath.Join(root, ".physa/capabilities/map/index.md")); !strings.HasSuffix(got, "The map.\n\n- [Rules](rules.md)\n- [Questions](questions.md)\n") {
		t.Errorf("map index links not regenerated:\n%s", got)
	}
	if got := read(t, filepath.Join(root, ".physa/capabilities/map/rules.md")); got != stray {
		t.Errorf("file with stray prose changed:\n%s", got)
	}
	changed, problems, err = FormatMap(root, false)
	if err != nil || len(changed) != 0 || len(problems) != 1 {
		t.Errorf("second run: changed %q, problems %v, err %v", changed, problems, err)
	}
}

func TestFormatMapOldLayout(t *testing.T) {
	for _, old := range []string{".physa/product.md", ".physa/capabilities/init.md"} {
		root := t.TempDir()
		stale := strings.Replace(KindFile("Map", Rule, []string{ruleA}), "- [Uncertain", "- [Stale", 1)
		writeFiles(t, root, map[string]string{
			old:                                "---\npromise: P.\n---\n\n# Old\n",
			".physa/capabilities/map/rules.md": stale,
		})
		changed, _, err := FormatMap(root, true)
		var ol ErrOldLayout
		if !errors.As(err, &ol) || ol.Path != old {
			t.Errorf("%s: err = %v, want ErrOldLayout naming it", old, err)
		}
		if !strings.Contains(err.Error(), old) || !strings.Contains(err.Error(), "convert") {
			t.Errorf("%s: message %q", old, err)
		}
		if len(changed) != 0 || read(t, filepath.Join(root, ".physa/capabilities/map/rules.md")) != stale {
			t.Errorf("%s: formatted before reporting the old layout", old)
		}
	}
	if _, _, err := FormatMap(t.TempDir(), false); err == nil {
		t.Error("no error for a root without .physa")
	}
}
