package prcheck

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/history"
)

func clean() Result {
	return Result{
		Version: Version, Range: "a..b",
		Commits:  []history.Commit{{Hash: "abc1234567", Short: "abc1234", Subject: "feat: x", Capabilities: []string{"learning"}, Loaded: []string{"map", "engine"}}},
		Findings: []history.Finding{},
		Map:      MapResult{Problems: []Problem{}, PRAnchors: PRAnchorsGitHub},
	}
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
	if m["version"] != float64(1) {
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
}

func TestTitle(t *testing.T) {
	cases := []struct {
		r    Result
		want string
	}{
		{clean(), "1 commit · 0 trailer findings · no map problems"},
		{messy(), "2 commits · 1 trailer finding · 2 map problems"},
		{Result{Map: MapResult{Problems: []Problem{{}}}}, "0 commits · 0 trailer findings · 1 map problem"},
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
	for _, want := range []string{"## 1 commit", "| abc1234 | feat: x | learning | map, engine |", "No trailer findings.", "The map passes physa check.", "checked against GitHub", "Misses and excess: not computed yet"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
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
	for _, r := range []Result{clean(), messy()} {
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
	v2 := strings.Replace(o.Text, `"version": 1`, `"version": 2`, 1)
	_, err := Decode(v2)
	if err == nil || !strings.Contains(err.Error(), "version 2") || !strings.Contains(err.Error(), "version 1") {
		t.Errorf("version 2: err = %v", err)
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
