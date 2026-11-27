package consolidating

import (
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfolder"
)

// Repeat is a rule repeated, as an exact duplicate, in different areas: a
// candidate to lift. IDs are in walk order, Areas each once.
type Repeat struct {
	IDs   []string              `json:"ids"`
	Areas []capabilitytext.Area `json:"areas"`
}

// exactIn merges the exact duplicates (by normal) among caps' entries of the
// same kind: the first in order is kept, every anchor of the rest is added
// to it, and the rest are removed. It returns the merges, by id.
func exactIn(caps []*capabilitytext.Capability, normal Normal) []EntryMerge {
	type at struct {
		c    *capabilitytext.Capability
		slug string
	}
	first := map[string]at{}
	var order []string
	dups := map[string][]at{}
	for _, c := range caps {
		for _, e := range c.Entries() {
			key := e.Kind.String() + "\x00" + normal(e.Heading, e.Body())
			if _, ok := first[key]; !ok {
				first[key] = at{c, e.Slug}
				order = append(order, key)
				continue
			}
			dups[key] = append(dups[key], at{c, e.Slug})
		}
	}
	var out []EntryMerge
	for _, key := range order {
		if len(dups[key]) == 0 {
			continue
		}
		k := first[key]
		m := EntryMerge{Kept: ID(k.c.Name, k.slug)}
		var removed []capabilitytext.Entry
		for _, d := range dups[key] {
			e, _ := d.c.Remove(d.slug)
			removed = append(removed, e)
			m.Merged = append(m.Merged, ID(d.c.Name, d.slug))
		}
		m.AnchorsAdded = capabilitytext.AddAnchors(k.c.Entry(k.slug), removed...)
		out = append(out, m)
	}
	return out
}

// ExactDuplicates merges, inside each area, the exact duplicates (by
// normal) among entries of the same kind, the first in walk order kept
// with every anchor of the rest. Rules that are exact duplicates in
// different areas are not merged: it returns them as repeats, candidates to
// lift.
func (s *State) ExactDuplicates(normal Normal) []Repeat {
	for _, a := range s.Areas() {
		var caps []*capabilitytext.Capability
		for i := range s.caps {
			if s.caps[i].Area == a {
				caps = append(caps, &s.caps[i])
			}
		}
		for _, m := range exactIn(caps, normal) {
			s.rec.ExactMerges = append(s.rec.ExactMerges, m)
			for _, id := range append([]string{m.Kept}, m.Merged...) {
				s.changed[strings.SplitN(id, "/", 2)[0]] = true
			}
			for _, id := range m.Merged {
				s.gone[id] = "merged into " + m.Kept
			}
		}
	}
	byKey := map[string]*Repeat{}
	var order []string
	for _, c := range s.caps {
		for _, e := range c.Kinded(mapfolder.Rule) {
			key := normal(e.Heading, e.Body())
			r, ok := byKey[key]
			if !ok {
				r = &Repeat{}
				byKey[key] = r
				order = append(order, key)
			}
			r.IDs = append(r.IDs, ID(c.Name, e.Slug))
			if !slices.Contains(r.Areas, c.Area) {
				r.Areas = append(r.Areas, c.Area)
			}
		}
	}
	var out []Repeat
	for _, k := range order {
		if r := byKey[k]; len(r.Areas) > 1 {
			out = append(out, *r)
		}
	}
	return out
}

// AreaChoices is what an area's session chose, by id: entries to merge,
// rules to move and questions to drop as answered.
type AreaChoices struct {
	Merges []MergeChoice `json:"merges"`
	Moves  []MoveChoice  `json:"moves"`
	Drops  []DropChoice  `json:"drops"`
}

// MergeChoice keeps one entry and drops others of its kind into it.
type MergeChoice struct {
	Keep string   `json:"keep"`
	Drop []string `json:"drop"`
}

// MoveChoice moves a rule to another capability of its area.
type MoveChoice struct {
	ID string `json:"id"`
	To string `json:"to"`
}

// DropChoice drops a question another live entry of its area answers.
type DropChoice struct {
	ID         string `json:"id"`
	AnsweredBy string `json:"answered_by"`
}

// inArea is the entry id names when it is live and in area, or why not.
func (s *State) inArea(area capabilitytext.Area, id string) (*capabilitytext.Capability, *capabilitytext.Entry, string) {
	c, e, why := s.lookup(id)
	if why == "" && c.Area != area {
		return nil, nil, id + " is not in the area " + area.String()
	}
	return c, e, why
}

// ApplyArea applies an area's choices, by id, in order: merges, then
// moves, then drops. Each item that fails a check is refused with its
// reason and the rest still apply.
func (s *State) ApplyArea(area capabilitytext.Area, ch AreaChoices) {
	for _, m := range ch.Merges {
		s.mergeEntries(area, m)
	}
	for _, m := range ch.Moves {
		s.move(area, m)
	}
	for _, d := range ch.Drops {
		s.drop(area, d)
	}
}

func (s *State) mergeEntries(area capabilitytext.Area, m MergeChoice) {
	item := "merge " + m.Keep + " with " + strings.Join(m.Drop, ", ")
	_, keep, why := s.inArea(area, m.Keep)
	if why != "" {
		s.refuse(StepArea, item, "%s", why)
		return
	}
	if len(m.Drop) == 0 {
		s.refuse(StepArea, item, "it names no entry to merge into %s", m.Keep)
		return
	}
	for i, id := range m.Drop {
		_, e, why := s.inArea(area, id)
		switch {
		case why != "":
			s.refuse(StepArea, item, "%s", why)
			return
		case id == m.Keep || slices.Contains(m.Drop[:i], id):
			s.refuse(StepArea, item, "%s is named twice", id)
			return
		case e.Kind != keep.Kind:
			s.refuse(StepArea, item, "%s is a %s and %s a %s: only entries of one kind merge", id, e.Kind, m.Keep, keep.Kind)
			return
		}
	}
	var removed []capabilitytext.Entry
	for _, id := range m.Drop {
		removed = append(removed, s.remove(id, "merged into "+m.Keep))
	}
	_, keep, _ = s.lookup(m.Keep)
	added := capabilitytext.AddAnchors(keep, removed...)
	s.changed[strings.SplitN(m.Keep, "/", 2)[0]] = true
	s.rec.EntryMerges = append(s.rec.EntryMerges, EntryMerge{m.Keep, slices.Clone(m.Drop), added})
}

func (s *State) move(area capabilitytext.Area, m MoveChoice) {
	item := "move " + m.ID + " to " + m.To
	from, e, why := s.inArea(area, m.ID)
	if why != "" {
		s.refuse(StepArea, item, "%s", why)
		return
	}
	at := s.capability(m.To)
	switch {
	case e.Kind != mapfolder.Rule:
		s.refuse(StepArea, item, "%s is a %s: only rules move", m.ID, e.Kind)
		return
	case at < 0:
		s.refuse(StepArea, item, "%q is not a capability", m.To)
		return
	case s.caps[at].Area != area:
		s.refuse(StepArea, item, "%q is not in the area %s", m.To, area)
		return
	case m.To == from.Name:
		s.refuse(StepArea, item, "%s is already in %q", m.ID, m.To)
		return
	}
	to := s.caps[at].Clone()
	if capabilitytext.Anchors(to.Title(), mapfolder.Rule, to.Kinded(mapfolder.Rule))[mapfolder.Anchor(e.Heading)] {
		s.refuse(StepArea, item, "its heading %q has the anchor of a heading already in the rules of %q", e.Heading, m.To)
		return
	}
	to.Add(e.Clone())
	f, err := to.Folder()
	if err != nil {
		s.refuse(StepArea, item, "the rules of %q would not split into a folder: %v", m.To, err)
		return
	}
	if n := capabilitytext.Lines(f.Files[mapfolder.RulesFile]); n > mapcheck.MaxRulesLines {
		s.refuse(StepArea, item, "the rules of %q would be %d lines, over the limit of %d", m.To, n, mapcheck.MaxRulesLines)
		return
	}
	dest := ID(m.To, e.Slug)
	s.remove(m.ID, "moved to "+dest)
	delete(s.gone, dest)
	s.caps[at] = to
	s.changed[m.To] = true
	s.rec.Moves = append(s.rec.Moves, Move{m.ID, dest})
}

func (s *State) drop(area capabilitytext.Area, d DropChoice) {
	item := "drop " + d.ID + " as answered by " + d.AnsweredBy
	_, e, why := s.inArea(area, d.ID)
	if why != "" {
		s.refuse(StepArea, item, "%s", why)
		return
	}
	if e.Kind != mapfolder.Question {
		s.refuse(StepArea, item, "%s is a %s: only questions are dropped as answered", d.ID, e.Kind)
		return
	}
	if d.AnsweredBy == "" {
		s.refuse(StepArea, item, "it names no entry that answers %s", d.ID)
		return
	}
	if d.AnsweredBy == d.ID {
		s.refuse(StepArea, item, "a question does not answer itself")
		return
	}
	if _, _, why := s.inArea(area, d.AnsweredBy); why != "" {
		s.refuse(StepArea, item, "%s", why)
		return
	}
	s.remove(d.ID, "dropped as answered by "+d.AnsweredBy)
	s.rec.Dropped = append(s.rec.Dropped, DroppedQuestion{d.ID, d.AnsweredBy})
}
