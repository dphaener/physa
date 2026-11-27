package walk

import (
	"slices"
	"testing"
)

func TestParseTakesTheLastJSONBlock(t *testing.T) {
	text := "thinking\n" + "```json\n{\"capabilities\":[{\"name\":\"a\",\"promise\":\"old\"}]}\n```\nlater\n```json\n{\"capabilities\":[{\"name\":\"b\",\"promise\":\"x  y\",\"sources\":[\"s/\"]}],\"shared\":[{\"path\":\"p\",\"reason\":\"r\"}]}\n```\n"
	a, err := Parse(text)
	if err != nil || a.Capabilities[0].Name != "b" || a.Capabilities[0].Promise != "x y" || a.Shared[0].Path != "p" {
		t.Fatalf("%+v, %v", a, err)
	}
}

func TestParseRefusesNeitherKeyAndNoBlock(t *testing.T) {
	for _, text := range []string{"```json\n{\"shared\":[]}\n```", "```json\n{}\n```", "no block", "```json\n{bad\n```"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestParseReadsASplit(t *testing.T) {
	a, err := Parse("```json\n{\"split\":[{\"name\":\"a\",\"promise\":\"p\",\"sources\":[\"x/\"]},{\"name\":\"b\",\"promise\":\"q\",\"sources\":[\"y/\"]}]}\n```")
	if err != nil || len(a.Split) != 2 {
		t.Fatalf("%+v, %v", a, err)
	}
}

func TestParseFixAndApply(t *testing.T) {
	f, err := ParseFix("```json\n{\"add\":{\"order-placing\":[\"util/glue.go\"]},\"shared\":[{\"path\":\"x.go\",\"reason\":\"r\"}],\"new\":[{\"name\":\"fresh\",\"promise\":\"p\",\"sources\":[\"n/\"]}],\"drop\":[\"client/form.ts\"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	before := whole()
	got := Apply(before, f)
	if !slices.Equal(got.Capabilities[0].Sources, []string{"server/refunds/"}) {
		t.Errorf("drop: %v", got.Capabilities[0].Sources)
	}
	if !slices.Contains(got.Capabilities[1].Sources, "util/glue.go") || len(got.Capabilities) != 3 || len(got.Shared) != 2 {
		t.Errorf("apply: %+v", got)
	}
	if len(before.Capabilities[0].Sources) != 2 {
		t.Error("Apply changed its input")
	}
	got = Apply(Answer{Split: []Part{{Name: "a", Sources: []string{"x"}}}}, Fix{Add: map[string][]string{"a": {"y"}}, Drop: []string{"x"}})
	if !slices.Equal(got.Split[0].Sources, []string{"y"}) {
		t.Errorf("split apply: %+v", got.Split)
	}
	if _, err := ParseFix("nothing"); err == nil {
		t.Error("no block accepted")
	}
}

// A real walk named capabilities with spaces, then added kebab-case copies:
// add must move a file claimed twice to one owner, and remove must take the
// misnamed capabilities out, or the area never ends whole.
func TestApplyMovesAddedPathsAndRemovesByName(t *testing.T) {
	before := Answer{Capabilities: []Capability{
		{Name: "pursuit plans", Sources: []string{"plan.go", "read.go"}, Tests: []string{"plan_test.go"}},
		{Name: "pursuit-plans", Sources: []string{"plan.go"}},
		{Name: "progress", Sources: []string{"work.go", "read.go"}},
	}, Shared: []Shared{{Path: "work.go", Reason: "r"}}}
	got := Apply(before, Fix{Add: map[string][]string{"pursuit-plans": {"read.go"}}, Remove: []string{"pursuit plans"}})
	if len(got.Capabilities) != 2 || got.Capabilities[0].Name != "pursuit-plans" {
		t.Fatalf("capabilities: %+v", got.Capabilities)
	}
	if !slices.Equal(got.Capabilities[0].Sources, []string{"plan.go", "read.go"}) || !slices.Equal(got.Capabilities[1].Sources, []string{"work.go"}) {
		t.Errorf("read.go not moved to one owner: %+v", got.Capabilities)
	}
	if len(got.Shared) != 1 || len(before.Capabilities) != 3 {
		t.Errorf("shared %+v, or the input changed", got.Shared)
	}
}

func TestParseReadsOtherAndApplyDropsFromIt(t *testing.T) {
	a, err := Parse("```json\n{\"capabilities\":[{\"name\":\"a\",\"promise\":\"p\",\"sources\":[\"s/\"],\"other\":[\"cfg/a.yml\",\"cfg/b.yml\"]}]}\n```")
	if err != nil || !slices.Equal(a.Capabilities[0].Other, []string{"cfg/a.yml", "cfg/b.yml"}) {
		t.Fatalf("%+v, %v", a, err)
	}
	b := Apply(a, Fix{Drop: []string{"cfg/a.yml"}})
	if !slices.Equal(b.Capabilities[0].Other, []string{"cfg/b.yml"}) || len(a.Capabilities[0].Other) != 2 {
		t.Errorf("applied %v, original %v", b.Capabilities[0].Other, a.Capabilities[0].Other)
	}
}
