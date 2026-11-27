package consolidating

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfolder"
)

// Normal is the form two entries share when they are exact duplicates: of
// a heading and a body without its Because: line, case, spacing and
// punctuation set aside.
type Normal func(heading, body string) string

// Merge merges the capabilities named in group into one named name, with
// the promise of promiseOf, both from the group (architecture 12 §3). Every
// rule, decision, question and term is kept, each in its kind; the group's
// exact duplicates (by normal) are merged first, keeping every anchor; a
// slug any other entry of the map holds is renamed; the footprint is the
// union, and the area is name's. It is refused, with its reason, and the
// capabilities stay apart, when the group has fewer than two capabilities
// or all are in one area, when two entries of a kind would share a
// heading's anchor, or when the merged folder would pass a size limit. It
// says whether it merged.
func (s *State) Merge(group []string, name, promiseOf string, normal Normal) bool {
	item := strings.Join(group, ", ") + " into " + name
	var members []capabilitytext.Capability
	for _, g := range group {
		i := s.capability(g)
		if i < 0 {
			s.refuse(StepMerge, item, "%q is not a capability", g)
			return false
		}
		if slices.ContainsFunc(members, func(c capabilitytext.Capability) bool { return c.Name == g }) {
			s.refuse(StepMerge, item, "%q is named twice", g)
			return false
		}
		members = append(members, s.caps[i])
	}
	taken := func(slug string) bool { return s.used[slug] }
	merged, m, exact, err := mergeGroup(members, name, promiseOf, taken, normal)
	if err != nil {
		s.refuse(StepMerge, item, "%v", err)
		return false
	}
	for _, r := range m.Renamed {
		s.used[strings.TrimPrefix(r.To, name+"/")] = true
	}
	at := s.capability(name)
	s.caps[at] = merged
	s.caps = slices.DeleteFunc(s.caps, func(c capabilitytext.Capability) bool {
		return c.Name != name && slices.Contains(group, c.Name)
	})
	for _, x := range exact {
		for _, id := range x.Merged {
			s.gone[id] = "merged into " + x.Kept
		}
	}
	s.rec.Merged = append(s.rec.Merged, m)
	s.rec.ExactMerges = append(s.rec.ExactMerges, exact...)
	s.changed[name] = true
	return true
}

// mergeGroup is Merge over members, its capabilities in walk order: the
// merged capability, its record and the exact merges made in it, or why it
// is refused. taken says whether a slug outside the group's entries is held
// in the map; a renamed slug takes the first of slug-2, slug-3… that no
// entry of the map holds.
func mergeGroup(members []capabilitytext.Capability, name, promiseOf string, taken func(string) bool, normal Normal) (capabilitytext.Capability, CapabilityMerge, []EntryMerge, error) {
	m := CapabilityMerge{Name: name, PromiseOf: promiseOf}
	var kept, promise *capabilitytext.Capability
	areas := map[capabilitytext.Area]bool{}
	for i := range members {
		c := &members[i]
		m.Group = append(m.Group, c.Name)
		areas[c.Area] = true
		if c.Name == name {
			kept = c
		}
		if c.Name == promiseOf {
			promise = c
		}
	}
	switch {
	case len(members) < 2:
		return capabilitytext.Capability{}, m, nil, fmt.Errorf("a merge needs two capabilities or more")
	case len(areas) < 2:
		return capabilitytext.Capability{}, m, nil, fmt.Errorf("every capability of the group is in one area")
	case kept == nil:
		return capabilitytext.Capability{}, m, nil, fmt.Errorf("the name %q is not one of the group's", name)
	case promise == nil:
		return capabilitytext.Capability{}, m, nil, fmt.Errorf("the promise of %q is not one of the group's", promiseOf)
	case kept.PromiseLine() < 0 || promise.PromiseLine() < 0:
		return capabilitytext.Capability{}, m, nil, fmt.Errorf("a capability of the group has no promise line")
	}
	out := kept.Clone()
	out.Head[out.PromiseLine()] = promise.Head[promise.PromiseLine()]
	held := map[string]bool{}
	for _, e := range out.Entries() {
		held[e.Slug] = true
	}
	free := func(base string) string {
		for n := 2; ; n++ {
			if to := fmt.Sprintf("%s-%d", base, n); !held[to] && !taken(to) {
				return to
			}
		}
	}
	for _, c := range members {
		if c.Name == name {
			continue
		}
		if o := c.Overview(); o != "" {
			m.DroppedOverviews = append(m.DroppedOverviews, DroppedOverview{c.Name, o})
		}
		for _, e := range c.Entries() {
			e = e.Clone()
			if held[e.Slug] {
				to := free(e.Slug)
				m.Renamed = append(m.Renamed, Rename{ID(c.Name, e.Slug), ID(name, to)})
				capabilitytext.Rename(&e, to)
			}
			held[e.Slug] = true
			out.Add(e)
		}
		for _, t := range c.Terms {
			i := slices.IndexFunc(out.Terms, func(o capabilitytext.Term) bool { return strings.EqualFold(o.Name, t.Name) })
			switch {
			case i < 0:
				out.AddTerm(t)
			case !out.Terms[i].Same(t):
				m.TermConflicts = append(m.TermConflicts, TermConflict{c.Name, t.Name, name, t.Text()})
			}
		}
		out.Footprint = capabilitytext.Union(out.Footprint, c.Footprint)
	}
	exact := exactIn([]*capabilitytext.Capability{&out}, normal)
	for _, k := range mapfolder.Kinds {
		if h := capabilitytext.Clash(out.Title(), k, out.Kinded(k)); h != "" {
			return capabilitytext.Capability{}, m, nil, fmt.Errorf("two %s would share the heading anchor of %q, and code never rewords a heading", strings.ToLower(k.Word()), h)
		}
	}
	if err := fits(out); err != nil {
		return capabilitytext.Capability{}, m, nil, err
	}
	return out, m, exact, nil
}

// fits says why c's folder, as the map writes it, would fail the check's
// size limits or not split; nil when it fits.
func fits(c capabilitytext.Capability) error {
	f, err := c.Folder()
	if err != nil {
		return fmt.Errorf("the merged text does not split into a folder: %v", err)
	}
	if n := capabilitytext.Lines(f.Files[mapfolder.RulesFile]); n > mapcheck.MaxRulesLines {
		return fmt.Errorf("its rules would be %d lines, over the limit of %d", n, mapcheck.MaxRulesLines)
	}
	if n := capabilitytext.Lines(f.Files[mapfolder.IndexFile]); n > mapcheck.MaxIndexLines {
		return fmt.Errorf("its index would be %d lines, over the limit of %d", n, mapcheck.MaxIndexLines)
	}
	return nil
}
