package consolidating

import (
	"slices"
	"testing"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/mapfolder"
)

// seatGroup is the candidate group of areaMap's seat rule, every id shown.
var seatGroup = []string{"a1/a1-seat", "a2/a2-seat", "b1/b1-seat", "p1/p1-seat"}

func TestLiftMovesTheKeptRuleWithEveryAnchor(t *testing.T) {
	s := New("Product", areaMap(t), nil)
	if !s.Lift(LiftChoice{Keep: "b1/b1-seat", With: []string{"a1/a1-seat", "p1/p1-seat"}}, seatGroup, [][]string{seatGroup}) {
		t.Fatalf("refused %+v", s.Record().Refused)
	}
	out := s.Output()
	if len(out.Rules) != 1 || out.Rules[0].Lines[1] != "`b1-seat` · #5 · #1 #6" || out.Rules[0].Heading != "Seats are paid" {
		t.Errorf("product rules %+v", out.Rules)
	}
	a1, b1, p1 := find(t, out.Capabilities, "a1"), find(t, out.Capabilities, "b1"), find(t, out.Capabilities, "p1")
	if a1.Entry("a1-seat") != nil || b1.Entry("b1-seat") != nil || p1.Entry("p1-seat") != nil {
		t.Error("a lifted rule stayed in its capability")
	}
	r := s.Record()
	if len(r.Lifts) != 1 || !slices.Equal(r.Lifts[0].Areas, []capabilitytext.Area{areaB, areaA, areaBPart}) || !slices.Equal(r.Lifts[0].AnchorsAdded, []string{"#1", "#6"}) {
		t.Errorf("lifts %+v", r.Lifts)
	}
	if !slices.Equal(r.Consolidated, []string{"a1", "b1", "p1"}) {
		t.Errorf("consolidated %v", r.Consolidated)
	}
	checkForm(t, out)
}

func TestLiftOneAreaRefused(t *testing.T) {
	s := New("Product", areaMap(t), nil)
	if s.Lift(LiftChoice{Keep: "a1/a1-seat", With: []string{"a2/a2-seat"}}, seatGroup, [][]string{seatGroup}) {
		t.Fatal("lifted a rule repeated inside one area")
	}
	refused(t, s.Record(), StepLift, "all in one area")
	if len(s.Output().Rules) != 0 {
		t.Error("the product's rules changed")
	}
	if s.Lift(LiftChoice{Keep: "b1/b1-seat"}, seatGroup, [][]string{seatGroup}) {
		t.Fatal("lifted a rule alone")
	}
}

func TestLiftRefusesIDsOutsideItsGroupOrSession(t *testing.T) {
	caps := areaMap(t)
	caps = append(caps, parse(t, "b2", areaB, text("b2", "B2.", "", nil, []string{
		"### We bill seats\n`b2-bill` · decided by dphaener, in their words · 2026-01-02 · #8\nSeats are billed.\nBecause: b.\n",
	}, nil), ""))
	groups := [][]string{seatGroup, {"a1/a1-expire", "b1/b1-seat"}}
	for _, tc := range []struct {
		name  string
		l     LiftChoice
		shown []string
		want  string
	}{
		{"outside the group", LiftChoice{"a1/a1-seat", []string{"a1/a1-expire"}}, append(slices.Clone(seatGroup), "a1/a1-expire"), "not in the candidate group"},
		{"not shown", LiftChoice{"a1/a1-seat", []string{"b1/b1-seat"}}, []string{"a1/a1-seat"}, "not shown"},
		{"a decision", LiftChoice{"a1/a1-seat", []string{"b2/b2-bill"}}, append(slices.Clone(seatGroup), "b2/b2-bill"), "only rules"},
		{"unknown", LiftChoice{"a1/a1-seat", []string{"b1/nope"}}, seatGroup, "not an entry"},
		{"kept not in any group", LiftChoice{"a1/a1-renew", []string{"b1/b1-seat"}}, []string{"a1/a1-renew", "b1/b1-seat"}, "only rules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New("Product", caps, nil)
			if s.Lift(tc.l, tc.shown, groups) {
				t.Fatal("lifted")
			}
			refused(t, s.Record(), StepLift, tc.want)
			if len(s.Output().Rules) != 0 || len(s.Record().Consolidated) != 0 {
				t.Error("something changed")
			}
		})
	}
}

func TestLiftRefusesHeadingAnchorInProductRules(t *testing.T) {
	product := parse(t, "product", areaA, text("product", "P.", "", []string{entry("Seats are paid.", "p-seat", "#99", "Already lifted.", "p.")}, nil, nil), "")
	s := New("Product", areaMap(t), product.Kinded(mapfolder.Rule))
	if s.Lift(LiftChoice{Keep: "a1/a1-seat", With: []string{"b1/b1-seat"}}, seatGroup, [][]string{seatGroup}) {
		t.Fatal("lifted onto a heading anchor the product's rules have")
	}
	refused(t, s.Record(), StepLift, "already in the product's rules")
}

func TestLiftBudgetRefusedAndProductRulesUnchanged(t *testing.T) {
	// 24 rules of five lines make the product's rules 150 lines: one more
	// passes the limit.
	product := parse(t, "product", areaA, text("product", "P.", "", many("lifted", 24, "#99"), nil, nil), "")
	rules := product.Kinded(mapfolder.Rule)
	if n := capabilitytext.Lines(ProductRules("Product", rules)); n != 150 {
		t.Fatalf("product rules %d lines, want 150", n)
	}
	s := New("Product", areaMap(t), rules)
	if s.Lift(LiftChoice{Keep: "a1/a1-seat", With: []string{"b1/b1-seat"}}, seatGroup, [][]string{seatGroup}) {
		t.Fatal("lifted past the product rules budget")
	}
	refused(t, s.Record(), StepLift, "over the limit of 150")
	out := s.Output()
	if got := ProductRules("Product", out.Rules); got != ProductRules("Product", rules) {
		t.Errorf("product rules changed:\n%s", got)
	}
	a1 := find(t, out.Capabilities, "a1")
	if a1.Entry("a1-seat") == nil || len(s.Record().Consolidated) != 0 {
		t.Error("the refused lift took a rule out")
	}
	if len(s.Record().Lifts) != 0 {
		t.Errorf("lifts %+v", s.Record().Lifts)
	}
}
