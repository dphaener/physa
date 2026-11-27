// Package consolidating is the code side of init's consolidation: over the
// capabilities recovered from the walk, it merges capabilities a session
// names as one promise, merges exact duplicate entries, applies an area's
// merges, moves and drops by id, and lifts rules repeated across areas to
// the product's rules. It never writes, rewords or invents an entry, a
// heading, a reason, a name or a promise: it only chooses among what
// recovery wrote and adds anchors, refuses with its reason whatever fails a
// check or a size limit, and records everything it did.
package consolidating

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/capabilitytext"
)

// Record is what consolidation did, as init's record stores and shows it.
type Record struct {
	// SlugRenames and TermConflicts are the pre-pass's: slugs and terms that
	// separate recoveries already shared before anything was merged.
	SlugRenames   []Rename          `json:"slug_renames"`
	TermConflicts []TermConflict    `json:"term_conflicts"`
	Merged        []CapabilityMerge `json:"merged"`
	ExactMerges   []EntryMerge      `json:"exact_merges"`
	EntryMerges   []EntryMerge      `json:"entry_merges"`
	Moves         []Move            `json:"moves"`
	Dropped       []DroppedQuestion `json:"dropped"`
	Lifts         []Lift            `json:"lifts"`
	Refused       []Refusal         `json:"refused"`
	// Consolidated are the capabilities whose text or terms changed, in walk
	// order; every other one is left as recovered.
	Consolidated []string `json:"consolidated"`
}

// Rename is an entry whose slug was taken, by id before and after.
type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// TermConflict is a term whose name an earlier capability already defines
// differently: the first definition is kept, and this one is listed whole.
type TermConflict struct {
	Capability string `json:"capability"`
	Name       string `json:"name"`
	KeptIn     string `json:"kept_in"`
	Text       string `json:"text"`
}

// CapabilityMerge is capabilities merged into one: the group, the name and
// whose promise it keeps, and what the merge renamed, dropped and listed.
type CapabilityMerge struct {
	Group            []string          `json:"group"`
	Name             string            `json:"name"`
	PromiseOf        string            `json:"promise_of"`
	Renamed          []Rename          `json:"renamed_slugs"`
	DroppedOverviews []DroppedOverview `json:"dropped_overviews"`
	TermConflicts    []TermConflict    `json:"term_conflicts"`
}

// DroppedOverview is the overview of a merged capability whose name was not
// kept.
type DroppedOverview struct {
	Capability string `json:"capability"`
	Text       string `json:"text"`
}

// EntryMerge is entries merged into a kept one, by id, and the anchors the
// kept one took from them.
type EntryMerge struct {
	Kept         string   `json:"kept"`
	Merged       []string `json:"merged"`
	AnchorsAdded []string `json:"anchors_added"`
}

// Move is a rule moved to another capability of its area, by id before and
// after.
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DroppedQuestion is a question dropped as answered, and the entry that
// answers it.
type DroppedQuestion struct {
	ID         string `json:"id"`
	AnsweredBy string `json:"answered_by"`
}

// Lift is a rule lifted to the product's rules: the one kept, the ones it
// repeats, the areas they came from and the anchors it took from them.
type Lift struct {
	Kept         string                `json:"kept"`
	With         []string              `json:"with"`
	Areas        []capabilitytext.Area `json:"areas"`
	AnchorsAdded []string              `json:"anchors_added"`
}

// Refusal is an item consolidation refused: the step, the item and why.
type Refusal struct {
	Step   string `json:"step"`
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

// The steps a Refusal names.
const (
	StepMerge = "merge"
	StepArea  = "area"
	StepLift  = "lift"
	StepGuard = "guard"
)

// Output is consolidation's result: the capabilities in walk order and the
// product's rules, under the product's name.
type Output struct {
	Product      string
	Capabilities []capabilitytext.Capability
	Rules        []capabilitytext.Entry
}

// State is a consolidation under way over the capabilities recovered from
// the walk.
type State struct {
	product string
	caps    []capabilitytext.Capability
	rules   []capabilitytext.Entry
	rec     Record
	used    map[string]bool   // every slug the map has held
	gone    map[string]string // removed ids, to why
	changed map[string]bool
}

// New starts a consolidation of caps, in walk order, for the product named
// product, whose rules are rules. Nothing given is changed. Before anything
// else it renames, in later capabilities in walk order, slugs an earlier one
// holds, and keeps once a term an earlier one defines the same way or lists
// it as a term conflict when it is defined differently: slugs and term names
// are unique in the whole map.
func New(product string, caps []capabilitytext.Capability, rules []capabilitytext.Entry) *State {
	s := &State{product: product, used: map[string]bool{}, gone: map[string]string{}, changed: map[string]bool{}}
	for _, c := range caps {
		s.caps = append(s.caps, c.Clone())
	}
	for _, r := range rules {
		s.rules = append(s.rules, r.Clone())
		s.used[r.Slug] = true
	}
	for _, c := range s.caps {
		for _, e := range c.Entries() {
			s.used[e.Slug] = true
		}
	}
	seen := map[string]bool{}
	for _, r := range s.rules {
		seen[r.Slug] = true
	}
	terms := map[string]string{} // lower-case name to the capability defining it
	byName := map[string]capabilitytext.Term{}
	for ci := range s.caps {
		c := &s.caps[ci]
		for si := range c.Sections {
			for ei := range c.Sections[si].Entries {
				e := &c.Sections[si].Entries[ei]
				if !seen[e.Slug] {
					seen[e.Slug] = true
					continue
				}
				to := s.free(e.Slug)
				s.rec.SlugRenames = append(s.rec.SlugRenames, Rename{ID(c.Name, e.Slug), ID(c.Name, to)})
				capabilitytext.Rename(e, to)
				seen[to], s.changed[c.Name] = true, true
			}
		}
		var kept []capabilitytext.Term
		for _, t := range c.Terms {
			key := strings.ToLower(t.Name)
			first, dup := terms[key]
			switch {
			case !dup:
				terms[key], byName[key] = c.Name, t
				kept = append(kept, t)
				continue
			case !byName[key].Same(t):
				s.rec.TermConflicts = append(s.rec.TermConflicts, TermConflict{c.Name, t.Name, first, t.Text()})
			}
			s.changed[c.Name] = true
		}
		c.Terms = kept
	}
	return s
}

// ID is an entry's id: its capability's name and its slug.
func ID(capability, slug string) string { return capability + "/" + slug }

// free is the first of base-2, base-3… that no entry of the map has held,
// and marks it held.
func (s *State) free(base string) string {
	for n := 2; ; n++ {
		if to := base + "-" + strconv.Itoa(n); !s.used[to] {
			s.used[to] = true
			return to
		}
	}
}

// refuse records an item refused at step, with its reason.
func (s *State) refuse(step, item, format string, args ...any) {
	s.rec.Refused = append(s.rec.Refused, Refusal{step, item, fmt.Sprintf(format, args...)})
}

// capability is the index of the capability named name, -1 when none is.
func (s *State) capability(name string) int {
	for i, c := range s.caps {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// lookup is the capability and entry an id names, or why it names none: an
// unknown id, or one consolidation already removed.
func (s *State) lookup(id string) (*capabilitytext.Capability, *capabilitytext.Entry, string) {
	if why, ok := s.gone[id]; ok {
		return nil, nil, fmt.Sprintf("%s was already removed: %s", id, why)
	}
	name, slug, _ := strings.Cut(id, "/")
	if i := s.capability(name); i >= 0 {
		if e := s.caps[i].Entry(slug); e != nil {
			return &s.caps[i], e, ""
		}
	}
	return nil, nil, fmt.Sprintf("%s is not an entry of any capability", id)
}

// remove takes the entry id out of its capability, recording why.
func (s *State) remove(id, why string) capabilitytext.Entry {
	name, slug, _ := strings.Cut(id, "/")
	c := &s.caps[s.capability(name)]
	e, _ := c.Remove(slug)
	s.gone[id], s.changed[name] = why, true
	return e
}

// Areas is the areas of the capabilities, in walk order, each once.
func (s *State) Areas() []capabilitytext.Area {
	var out []capabilitytext.Area
	seen := map[capabilitytext.Area]bool{}
	for _, c := range s.caps {
		if !seen[c.Area] {
			seen[c.Area] = true
			out = append(out, c.Area)
		}
	}
	return out
}

// Capabilities is the capabilities as they stand, in walk order, as copies.
func (s *State) Capabilities() []capabilitytext.Capability {
	out := make([]capabilitytext.Capability, len(s.caps))
	for i, c := range s.caps {
		out[i] = c.Clone()
	}
	return out
}

// Output is the capabilities and the product's rules as they stand, as
// copies.
func (s *State) Output() Output {
	o := Output{Product: s.product, Capabilities: s.Capabilities()}
	for _, r := range s.rules {
		o.Rules = append(o.Rules, r.Clone())
	}
	return o
}

// Record is what consolidation did so far, with the capabilities whose text
// or terms changed.
func (s *State) Record() Record {
	r := s.rec
	r.Consolidated = nil
	for _, c := range s.caps {
		if s.changed[c.Name] {
			r.Consolidated = append(r.Consolidated, c.Name)
		}
	}
	return r
}

// Refuse records an item refused at step with its reason, for a caller
// whose own check refused it, such as the guard.
func (s *State) Refuse(r Refusal) { s.rec.Refused = append(s.rec.Refused, r) }
