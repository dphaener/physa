package repeats

import (
	"reflect"
	"slices"
	"testing"
)

func rule(id, capab, area, heading, body string) Rule {
	return Rule{ID: id, Capability: capab, Area: area, Kind: "rule", Heading: heading, Body: body}
}

func ids(g RuleGroup) []string {
	var out []string
	for _, r := range g.Rules {
		out = append(out, r.ID)
	}
	return out
}

func TestNormal(t *testing.T) {
	a := Normal("Pull  before, relying!", "On a machine.\nBecause: reasons.")
	b := Normal("pull before relying", "on a MACHINE")
	if a != b || a != "pull before relying on a machine" {
		t.Errorf("%q %q", a, b)
	}
}

func TestSuffixPairs(t *testing.T) {
	caps := []Capability{{"walk", "a"}, {"walk-2", "b"}, {"other", "b"}, {"x", "c"}, {"x-y", "d"}, {"y", "d"}, {"y-2", "d"}}
	ren := []Rename{{"b", "walk", "walk-2"}, {"d", "x", "x-y"}, {"d", "y", "y-2"}, {"b", "gone", "gone-2"}}
	got := SuffixPairs(caps, ren)
	if len(got) != 1 || got[0].Original != (Capability{"walk", "a"}) || got[0].Renamed != (Capability{"walk-2", "b"}) || got[0].Reason == "" {
		t.Errorf("%+v", got)
	}
}

func TestNearAndExact(t *testing.T) {
	rules := []Rule{
		rule("r1", "a1", "a", "Refuse an unknown id", "The session may only name ids it was given."),
		rule("r2", "b1", "b", "Refuse an unknown id!", "The session may only name ids it was given.\nBecause: other."),
		rule("r3", "c1", "c", "Refuse unknown ids", "A session may only name the ids it was given in its prompt."),
		rule("r4", "d1", "d", "Cache stays warm", "Entries are kept for ten minutes."),
	}
	got := RuleGroups(rules)
	if len(got) != 1 || !reflect.DeepEqual(ids(got[0]), []string{"r1", "r2", "r3"}) || got[0].Exact {
		t.Errorf("%+v", got)
	}
	got = RuleGroups(rules[:2])
	if len(got) != 1 || !got[0].Exact {
		t.Errorf("%+v", got)
	}
}

func TestExactKindDiffers(t *testing.T) {
	a := rule("a", "c1", "x", "Same words", "same body")
	b := rule("b", "c2", "y", "Same words", "same body")
	b.Kind = "question"
	if got := RuleGroups([]Rule{a, b}); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestOneAreaNotReturned(t *testing.T) {
	rules := []Rule{
		rule("r1", "a1", "a", "Refuse an unknown id", "Name only ids given."),
		rule("r2", "a2", "a", "Refuse an unknown id", "Name only ids given."),
		rule("r3", "a3", "a", "Refuse unknown ids", "Name only the ids given."),
	}
	if got := RuleGroups(rules); len(got) != 0 {
		t.Errorf("%+v", got)
	}
	if got := Find(Input{Capabilities: []Capability{{"a1", "a"}, {"a2", "a"}, {"a3", "a"}}, Rules: rules}); len(got.Capabilities) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestUnrelatedBelowOverlap(t *testing.T) {
	rules := []Rule{
		rule("r1", "a1", "a", "Walk each area", "Every area gets its own session for capabilities."),
		rule("r2", "b1", "b", "Walk each tree", "Every area gets cached output under budget limits."),
		rule("r3", "c1", "c", "Pay the invoice", "Customers receive monthly statements."),
	}
	if got := RuleGroups(rules); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestCapabilityGroups(t *testing.T) {
	caps := []Capability{{"walk", "a"}, {"walk-2", "b"}, {"sync", "c"}, {"sync-tool", "d"}, {"solo", "e"}}
	in := Input{
		Capabilities: caps,
		Renamed:      []Rename{{"b", "walk", "walk-2"}},
		Rules: []Rule{
			rule("r1", "sync", "c", "Retry on a rate limit", "Wait out the limit then retry."),
			rule("r2", "sync-tool", "d", "Retry on a rate limit", "Wait out the limit then retry."),
			rule("r3", "solo", "e", "Unrelated thing", "Nothing alike here."),
		},
	}
	got := Find(in).Capabilities
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].Capabilities[0].Name != "sync" || got[0].Capabilities[1].Name != "sync-tool" || len(got[0].Evidence) != 1 {
		t.Errorf("%+v", got[0])
	}
	if got[1].Capabilities[0].Name != "walk" || got[1].Capabilities[1].Name != "walk-2" || len(got[1].Evidence) != 1 {
		t.Errorf("%+v", got[1])
	}
}

func TestDeterministic(t *testing.T) {
	in := Input{
		Capabilities: []Capability{{"walk", "a"}, {"walk-2", "b"}, {"s", "c"}, {"t", "d"}},
		Renamed:      []Rename{{"b", "walk", "walk-2"}},
		Rules: []Rule{
			rule("r1", "s", "c", "Retry on a rate limit", "Wait out the limit then retry."),
			rule("r2", "t", "d", "Retry on a rate limit", "Wait then retry the limit."),
			rule("r3", "walk", "a", "Retry on rate limit", "Wait out the limit and retry."),
			rule("r4", "walk-2", "b", "Retry rate limit", "Wait out limit then retry it."),
		},
	}
	want := Find(in)
	if len(want.Rules) == 0 {
		t.Fatal("no groups")
	}
	for i := range 20 {
		c := in
		c.Rules = slices.Clone(in.Rules)
		c.Capabilities = slices.Clone(in.Capabilities)
		if i%2 == 1 {
			slices.Reverse(c.Rules)
			slices.Reverse(c.Capabilities)
		}
		if got := Find(c); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: %+v != %+v", i, got, want)
		}
	}
}
