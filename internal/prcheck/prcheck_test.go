package prcheck

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/review"
)

func clean() Result {
	return Result{
		Version: Version, Range: "a..b",
		Commits:  []history.Commit{{Hash: "abc1234567", Short: "abc1234", Subject: "feat: x", Capabilities: []string{"learning"}, Loaded: []string{"map", "engine"}}},
		Findings: []history.Finding{},
		Map:      MapResult{Problems: []Problem{}, PRAnchors: PRAnchorsGitHub},
		Reviewer: ReviewerResult{Status: ReviewerSkipped, Reason: "not requested"},
	}
}

func ran() Result {
	r := clean()
	r.Commits[0].Loaded = []string{"map", "engine"}
	r.Slice = Measure(r.Commits, []string{"engine", "learning"})
	r.Reviewer = ReviewerResult{
		Status: ReviewerRan, Agent: "claude-code", Model: "m-1",
		Tokens: &adapter.Tokens{Input: 100, CachedInput: 60, Output: 7},
		Review: &review.Review{
			Version: review.Version, Touched: []string{"engine", "learning"},
			Rules:          []review.RuleAtStake{{Capability: "learning", Rule: "a-rule", Respected: review.RespectedNo, Why: "pipe | here\nand a break"}},
			Contradictions: []review.Contradiction{{Capability: "engine", Decision: "a-decision", Why: "it reverses it"}},
			Edits:          []review.Edit{{Capability: "learning", Section: review.SectionRules, Entry: "### SECRET ENTRY TEXT", Why: "the change makes it true"}},
		},
	}
	return r
}

func messy() Result {
	r := clean()
	r.PR = 7
	r.Commits = append(r.Commits, history.Commit{Hash: "def", Short: "def5678", Subject: "fix: y", Capabilities: []string{}, Loaded: []string{}})
	r.Findings = []history.Finding{{Commit: "def5678", Kind: history.KindMissing, Message: "no Capability trailer"}}
	r.Map = MapResult{Problems: []Problem{{Path: ".physa/product.md", Line: 4, Message: "bad anchor"}, {Path: "x.md", Message: "whole file"}}, PRAnchors: PRAnchorsFormOnly, Note: "gh is not installed"}
	return r
}

func TestJSONShape(t *testing.T) {
	r := Result{Version: Version, Range: "a..b"}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["version"] != float64(2) {
		t.Errorf("version = %v", m["version"])
	}
	if _, ok := m["pr"]; ok {
		t.Error("pr should be omitted when 0")
	}
	if c, ok := m["commits"].([]any); !ok || len(c) != 0 {
		t.Errorf("commits = %v", m["commits"])
	}
	if f, ok := m["findings"].([]any); !ok || len(f) != 0 {
		t.Errorf("findings = %v", m["findings"])
	}
	if p, ok := m["map"].(map[string]any)["problems"].([]any); !ok || len(p) != 0 {
		t.Errorf("problems = %v", m["map"])
	}
	s, ok := m["slice"].(map[string]any)
	if !ok {
		t.Fatalf("slice missing: %s", data)
	}
	for _, k := range []string{"touched", "misses", "excess"} {
		v, present := s[k]
		if !present || v != nil {
			t.Errorf("slice.%s = %v, present %v; want null", k, v, present)
		}
	}
	if !strings.Contains(string(data), `"slice":{"touched":null,"misses":null,"excess":null}`) {
		t.Errorf("slice encoding: %s", data)
	}
	data, err = json.Marshal(Result{Version: Version, Slice: Measure(nil, []string{})})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"slice":{"touched":[],"misses":[],"excess":[]}`) {
		t.Errorf("measured empty slice encoding: %s", data)
	}
}

func TestMeasure(t *testing.T) {
	c := func(loaded ...string) history.Commit { return history.Commit{Loaded: loaded} }
	cases := []struct {
		name    string
		commits []history.Commit
		touched []string
		want    SliceResult
	}{
		{"miss", []history.Commit{c("a")}, []string{"a", "b"}, SliceResult{[]string{"a", "b"}, []string{"b"}, []string{}}},
		{"excess", []history.Commit{c("a", "z")}, []string{"a"}, SliceResult{[]string{"a"}, []string{}, []string{"z"}}},
		{"both", []history.Commit{c("m")}, []string{"b", "a"}, SliceResult{[]string{"a", "b"}, []string{"a", "b"}, []string{"m"}}},
		{"none", []history.Commit{c("a")}, []string{"a"}, SliceResult{[]string{"a"}, []string{}, []string{}}},
		{"union", []history.Commit{c("a"), c("b", "q"), c("a")}, []string{"b", "c"}, SliceResult{[]string{"b", "c"}, []string{"c"}, []string{"a", "q"}}},
		{"duplicates", []history.Commit{c("a", "a")}, []string{"x", "x", "a"}, SliceResult{[]string{"a", "x"}, []string{"x"}, []string{}}},
		{"nothing", nil, nil, SliceResult{[]string{}, []string{}, []string{}}},
	}
	for _, tc := range cases {
		if got := Measure(tc.commits, tc.touched); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestTitle(t *testing.T) {
	cases := []struct {
		r    Result
		want string
	}{
		{clean(), "1 commit · 0 trailer findings · no map problems · reviewer skipped"},
		{messy(), "2 commits · 1 trailer finding · 2 map problems · reviewer skipped"},
		{Result{Map: MapResult{Problems: []Problem{{}}}}, "0 commits · 0 trailer findings · 1 map problem"},
		{Result{Reviewer: ReviewerResult{Status: ReviewerFailed}}, "0 commits · 0 trailer findings · no map problems · reviewer failed"},
		{ran(), "1 commit · 0 trailer findings · no map problems · 2 touched · 1 miss · 1 excess"},
		{Result{Reviewer: ReviewerResult{Status: ReviewerRan}, Slice: Measure(nil, []string{"a"})}, "0 commits · 0 trailer findings · no map problems · 1 touched · 1 miss · 0 excess"},
	}
	for _, c := range cases {
		if got := c.r.Title(); got != c.want {
			t.Errorf("Title() = %q, want %q", got, c.want)
		}
	}
}

func md(t *testing.T, r Result) string {
	t.Helper()
	var b strings.Builder
	if err := WriteMarkdown(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestMarkdownClean(t *testing.T) {
	got := md(t, clean())
	for _, want := range []string{"## 1 commit", "| abc1234 | feat: x | learning | map, engine |", "No trailer findings.", "The map passes physa check.", "checked against GitHub", "Reviewer: skipped (not requested)."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownReviewer(t *testing.T) {
	got := md(t, ran())
	for _, want := range []string{
		"Reviewer: claude-code, m-1, 100 in (60 cached), 7 out.",
		"Touched: engine, learning", "Misses: learning", "Excess: map",
		"| Capability | Rule | Respected | Why |",
		`| learning | a-rule | no | pipe \| here and a break |`,
		"- engine, a-decision: it reverses it",
		"- learning, rules: the change makes it true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SECRET ENTRY TEXT") {
		t.Errorf("entry text must stay in the JSON:\n%s", got)
	}
	r := ran()
	r.Slice = Measure(r.Commits, []string{"map", "engine"})
	r.Reviewer.Review.Rules = nil
	empty := md(t, r)
	if !strings.Contains(empty, "Misses: -") || !strings.Contains(empty, "Excess: -") {
		t.Errorf("empty lists should show -:\n%s", empty)
	}
	r.Reviewer = ReviewerResult{Status: ReviewerFailed, Reason: "bad output", Review: ran().Reviewer.Review}
	failed := md(t, r)
	if !strings.Contains(failed, "Reviewer: failed (bad output).") || strings.Contains(failed, "Touched:") || strings.Contains(failed, "a-rule") {
		t.Errorf("failed:\n%s", failed)
	}
	r.Reviewer = ReviewerResult{Status: ReviewerSkipped, Reason: "no secret"}
	if got := md(t, r); !strings.Contains(got, "Reviewer: skipped (no secret).") || strings.Contains(got, "Touched:") {
		t.Errorf("skipped:\n%s", got)
	}
}

func TestMarkdownMessy(t *testing.T) {
	got := md(t, messy())
	for _, want := range []string{"| def5678 | fix: y | - | - |", "- def5678: no Capability trailer", "- .physa/product.md:4: bad anchor", "- x.md: whole file", "checked for form only (gh is not installed)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, r := range []Result{clean(), messy(), ran()} {
		o, err := CheckRunOutput(r)
		if err != nil {
			t.Fatal(err)
		}
		if o.Title != r.Title() || o.Summary != md(t, r) {
			t.Errorf("title or summary differ: %+v", o)
		}
		got, err := Decode(o.Text)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, r) {
			t.Errorf("round trip:\n got %+v\nwant %+v", got, r)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	o, _ := CheckRunOutput(clean())
	v3 := strings.Replace(o.Text, `"version": 2`, `"version": 3`, 1)
	_, err := Decode(v3)
	if err == nil || !strings.Contains(err.Error(), "version 3") || !strings.Contains(err.Error(), "versions 1 and 2") {
		t.Errorf("version 3: err = %v", err)
	}
	if _, err := Decode(strings.Replace(o.Text, `"version": 2`, `"version": 0`, 1)); err == nil {
		t.Error("version 0: want error")
	}
	if _, err := Decode("just some text"); err == nil {
		t.Error("plain text: want error")
	}
	if _, err := Decode("```json\n{broken\n```\n"); err == nil {
		t.Error("broken JSON: want error")
	}
	if _, err := Decode("```json\n{\"version\":1}"); err == nil {
		t.Error("unclosed fence: want error")
	}
}

func TestSizeLimit(t *testing.T) {
	r := clean()
	r.Commits[0].Subject = strings.Repeat("x", MaxFieldBytes)
	if _, err := CheckRunOutput(r); err == nil {
		t.Error("want error over the limit")
	}
	r = clean()
	r.Map.Note = strings.Repeat("n", MaxFieldBytes)
	if _, err := CheckRunOutput(r); err == nil {
		t.Error("want error for oversize summary")
	}
}

func TestDecodeVersion1(t *testing.T) {
	text := "```json\n" + `{"version":1,"range":"a..b","commits":[],"findings":[],"map":{"problems":[],"pr_anchors":"github"},"slice":{"touched":null,"misses":null,"excess":null}}` + "\n```\n"
	r, err := Decode(text)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != 1 || !reflect.DeepEqual(r.Reviewer, ReviewerResult{}) || r.Slice.Touched != nil {
		t.Errorf("v1 result: %+v", r)
	}
}

func TestOverflowFallsBackToFailedReviewer(t *testing.T) {
	r := ran()
	r.Reviewer.Review.Rules[0].Why = strings.Repeat("w", MaxFieldBytes+10)
	o, err := CheckRunOutput(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Summary) > MaxFieldBytes || len(o.Text) > MaxFieldBytes {
		t.Fatalf("still too big: %d, %d", len(o.Summary), len(o.Text))
	}
	got, err := Decode(o.Text)
	if err != nil {
		t.Fatal(err)
	}
	rv := got.Reviewer
	if rv.Status != ReviewerFailed || rv.Review != nil || !strings.Contains(rv.Reason, "the review is ") || !strings.Contains(rv.Reason, "bytes; the check run holds 65535") {
		t.Errorf("reviewer = %+v", rv)
	}
	if rv.Tokens == nil || rv.Agent != "claude-code" {
		t.Errorf("agent and tokens should stay: %+v", rv)
	}
	if got.Slice.Touched != nil || got.Slice.Misses != nil || got.Slice.Excess != nil {
		t.Errorf("slice = %+v", got.Slice)
	}
	if !strings.HasSuffix(o.Title, "reviewer failed") {
		t.Errorf("title = %q", o.Title)
	}
}
