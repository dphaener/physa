package consolidating

import (
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfolder"
)

// LiftChoice is a lift a session chose, by id: the rule kept and the rules
// it repeats.
type LiftChoice struct {
	Keep string   `json:"keep"`
	With []string `json:"with"`
}

// Lift applies one lift: the kept rule, with every anchor of the rules it
// repeats, goes to the product's rules, and all of them leave their
// capabilities. shown are the ids the session was shown and groups the
// candidate groups it was given. It is refused, with its reason and nothing
// changed, when an id is unknown, removed, not a rule, not shown, or not in
// keep's candidate group, when its rules are in fewer than two areas, when
// the kept rule's heading anchor is one the product's rules already have,
// or when the product's rules would pass their size limit. It says whether
// it lifted.
func (s *State) Lift(l LiftChoice, shown []string, groups [][]string) bool {
	item := "lift " + l.Keep + " with " + strings.Join(l.With, ", ")
	at := slices.IndexFunc(groups, func(g []string) bool { return slices.Contains(g, l.Keep) })
	var areas []capabilitytext.Area
	var with []capabilitytext.Entry
	var keep capabilitytext.Entry
	for i, id := range append([]string{l.Keep}, l.With...) {
		c, e, why := s.lookup(id)
		switch {
		case why != "":
			s.refuse(StepLift, item, "%s", why)
			return false
		case e.Kind != mapfolder.Rule:
			s.refuse(StepLift, item, "%s is a %s: only rules are lifted", id, e.Kind)
			return false
		case !slices.Contains(shown, id):
			s.refuse(StepLift, item, "%s was not shown to the session", id)
			return false
		case at < 0 || !slices.Contains(groups[at], id):
			s.refuse(StepLift, item, "%s is not in the candidate group of %s", id, l.Keep)
			return false
		case slices.Contains(l.With[:max(i-1, 0)], id) || (i > 0 && id == l.Keep):
			s.refuse(StepLift, item, "%s is named twice", id)
			return false
		}
		if !slices.Contains(areas, c.Area) {
			areas = append(areas, c.Area)
		}
		if i == 0 {
			keep = e.Clone()
		} else {
			with = append(with, e.Clone())
		}
	}
	if len(areas) < 2 {
		s.refuse(StepLift, item, "its rules are all in one area, and a rule repeated inside one area is never lifted")
		return false
	}
	if capabilitytext.Anchors(s.product, mapfolder.Rule, s.rules)[mapfolder.Anchor(keep.Heading)] {
		s.refuse(StepLift, item, "its heading %q has the anchor of a heading already in the product's rules", keep.Heading)
		return false
	}
	added := capabilitytext.AddAnchors(&keep, with...)
	rules := append(slices.Clone(s.rules), keep)
	if n := capabilitytext.Lines(ProductRules(s.product, rules)); n > mapcheck.MaxRulesLines {
		s.refuse(StepLift, item, "the product's rules would be %d lines, over the limit of %d", n, mapcheck.MaxRulesLines)
		return false
	}
	s.rules = rules
	s.remove(l.Keep, "lifted to the product's rules")
	for _, id := range l.With {
		s.remove(id, "lifted to the product's rules with "+l.Keep)
	}
	s.rec.Lifts = append(s.rec.Lifts, Lift{l.Keep, slices.Clone(l.With), areas, added})
	return true
}

// ProductRules is the product's rules file as the map writes it for the
// product named product: its title, its contents and the rules.
func ProductRules(product string, rules []capabilitytext.Entry) string {
	texts := make([]string, len(rules))
	for i, r := range rules {
		texts[i] = r.Text()
	}
	return mapfolder.KindFile(product, mapfolder.Rule, texts)
}
