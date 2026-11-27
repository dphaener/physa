// Package consolidateguard checks that consolidation's output holds only
// what recovery wrote, and loses nothing unrecorded: every entry and term in
// it has a source among the recovered capabilities, word for word, with no
// anchor none of them had, and every recovered entry, anchor and term is in
// it or named in the record of what consolidation did. Slugs stay unique in
// the map and headings within each file.
package consolidateguard

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/consolidating"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
)

// productID is the capability part of a product rule's id.
const productID = "product"

// placed is one entry and the id it has where it sits.
type placed struct {
	id string
	e  capabilitytext.Entry
}

// guard is one check of an output against its inputs and record.
type guard struct {
	inputs  []placed
	outputs []placed
	rec     consolidating.Record
	aliases [][]string // per input, every id it has been known by
	out     []consolidating.Refusal
}

func (g *guard) refuse(item, format string, args ...any) {
	g.out = append(g.out, consolidating.Refusal{Step: consolidating.StepGuard, Item: item, Reason: fmt.Sprintf(format, args...)})
}

// Guard checks out, consolidation's output, against inputs, the
// capabilities as recovered, and rec, the record of what consolidation did,
// in both directions, and returns a refusal naming the entry for every
// violation; none when the output holds.
func Guard(inputs []capabilitytext.Capability, out consolidating.Output, rec consolidating.Record) []consolidating.Refusal {
	g := &guard{rec: rec}
	for _, c := range inputs {
		for _, e := range c.Entries() {
			g.inputs = append(g.inputs, placed{consolidating.ID(c.Name, e.Slug), e})
		}
	}
	for _, c := range out.Capabilities {
		for _, e := range c.Entries() {
			g.outputs = append(g.outputs, placed{consolidating.ID(c.Name, e.Slug), e})
		}
	}
	for _, r := range out.Rules {
		g.outputs = append(g.outputs, placed{consolidating.ID(productID, r.Slug), r})
	}
	g.alias()
	g.entries()
	g.capabilities(inputs, out)
	g.unique(out)
	g.terms(inputs, out)
	return g.out
}

// alias finds every id each input entry has been known by, through the
// record's slug renames, capability merges and moves.
func (g *guard) alias() {
	edges := map[string][]string{}
	for _, r := range g.rec.SlugRenames {
		edges[r.From] = append(edges[r.From], r.To)
	}
	for _, m := range g.rec.Moves {
		edges[m.From] = append(edges[m.From], m.To)
	}
	into := map[string]consolidating.CapabilityMerge{}
	for _, m := range g.rec.Merged {
		for _, r := range m.Renamed {
			edges[r.From] = append(edges[r.From], r.To)
		}
		for _, c := range m.Group {
			if c != m.Name {
				into[c] = m
			}
		}
	}
	g.aliases = make([][]string, len(g.inputs))
	for i, in := range g.inputs {
		seen := []string{in.id}
		for at := 0; at < len(seen); at++ {
			id := seen[at]
			next := slices.Clone(edges[id])
			c, slug, _ := strings.Cut(id, "/")
			// An id renamed or moved no longer names anything in its
			// capability, so a merge does not carry it over.
			if m, ok := into[c]; ok && len(edges[id]) == 0 {
				next = append(next, consolidating.ID(m.Name, slug))
			}
			for _, n := range next {
				if !slices.Contains(seen, n) {
					seen = append(seen, n)
				}
			}
		}
		g.aliases[i] = seen
	}
}

// input is the input entry an id at some time named, -1 for none.
func (g *guard) input(id string) int {
	for i, as := range g.aliases {
		if slices.Contains(as, id) {
			return i
		}
	}
	return -1
}

// named says whether id names input i at some time.
func (g *guard) named(i int, ids ...string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return slices.Contains(g.aliases[i], id) })
}

// sameWords says whether a and b are entries of one kind with the same
// heading, body and Because: lines, word for word.
func sameWords(a, b capabilitytext.Entry) bool {
	return a.Kind == b.Kind && slices.Equal(a.Words(), b.Words())
}

// home is the output entry input i ended in, or -1 when it ended in none;
// dropped says whether the record drops it as answered.
func (g *guard) home(i, depth int) (at int, dropped bool) {
	at = -1
	for o, p := range g.outputs {
		if !sameWords(p.e, g.inputs[i].e) {
			continue
		}
		_, slug, _ := strings.Cut(p.id, "/")
		if g.named(i, p.id) || (strings.HasPrefix(p.id, productID+"/") && slices.ContainsFunc(g.aliases[i], func(a string) bool { return strings.HasSuffix(a, "/"+slug) })) {
			return o, false
		}
		if at < 0 {
			at = o
		}
	}
	if at >= 0 || depth > len(g.inputs) {
		return at, false
	}
	for _, d := range g.rec.Dropped {
		if g.named(i, d.ID) {
			return -1, true
		}
	}
	kept := func(m consolidating.EntryMerge) string {
		if g.named(i, m.Merged...) {
			return m.Kept
		}
		return ""
	}
	var into string
	for _, m := range append(slices.Clone(g.rec.ExactMerges), g.rec.EntryMerges...) {
		if into = kept(m); into != "" {
			break
		}
	}
	for _, l := range g.rec.Lifts {
		if into == "" && g.named(i, l.With...) {
			into = l.Kept
		}
	}
	if k := g.input(into); into != "" && k >= 0 && k != i {
		return g.home(k, depth+1)
	}
	return -1, false
}

// entries checks that every input entry ends in the output with its
// anchors, or is dropped as answered, and that every output entry has a
// source among the inputs, with no anchor none of its sources had.
func (g *guard) entries() {
	from := make([][]int, len(g.outputs))
	for i, in := range g.inputs {
		at, dropped := g.home(i, 0)
		switch {
		case dropped && in.e.Kind != mapfolder.Question:
			g.refuse(in.id, "it is recorded as dropped as answered, and only a question is")
		case dropped:
		case at < 0:
			g.refuse(in.id, "it is not in the output, and the record names it neither merged, dropped nor lifted")
		default:
			from[at] = append(from[at], i)
			for _, a := range in.e.Anchors {
				if !slices.Contains(g.outputs[at].e.Anchors, a) {
					g.refuse(in.id, "its anchor %q is not on %s, the entry it ended in", capabilitytext.AnchorText(a), g.outputs[at].id)
				}
			}
		}
	}
	for o, p := range g.outputs {
		if !slices.ContainsFunc(g.inputs, func(in placed) bool { return sameWords(in.e, p.e) }) {
			g.refuse(p.id, "it has no source among the recovered entries: no %s has its heading, body and reason word for word", p.e.Kind)
			continue
		}
		if !slices.ContainsFunc(from[o], func(i int) bool {
			return sameWords(g.inputs[i].e, p.e) && slices.Equal(g.inputs[i].e.Rest(), p.e.Rest())
		}) {
			g.refuse(p.id, "its meta line's person or date is not its source's")
		}
		var had []mapfile.Anchor
		for _, i := range from[o] {
			had = append(had, g.inputs[i].e.Anchors...)
		}
		for _, a := range p.e.Anchors {
			if !slices.Contains(had, a) {
				g.refuse(p.id, "its anchor %q is on none of the recovered entries it holds", capabilitytext.AnchorText(a))
			}
		}
	}
}

// capabilities checks that every output capability has an input's name and
// promise, and that a merged one has its promise_of member's promise.
func (g *guard) capabilities(inputs []capabilitytext.Capability, out consolidating.Output) {
	promises := map[string]string{}
	for _, c := range inputs {
		promises[c.Name] = c.Promise()
	}
	var all []string
	for _, p := range promises {
		all = append(all, p)
	}
	for _, c := range out.Capabilities {
		if _, ok := promises[c.Name]; !ok {
			g.refuse(c.Name, "no recovered capability has this name")
		}
		if !slices.Contains(all, c.Promise()) {
			g.refuse(c.Name, "its promise %q is no recovered capability's", c.Promise())
		}
		for _, m := range g.rec.Merged {
			if m.Name == c.Name && c.Promise() != promises[m.PromiseOf] {
				g.refuse(c.Name, "its promise is not that of %q, whose promise the merge keeps", m.PromiseOf)
			}
		}
	}
}

// unique checks that slugs are unique across the output, and heading
// anchors within each kind file.
func (g *guard) unique(out consolidating.Output) {
	slugs := map[string]string{}
	for _, p := range g.outputs {
		_, slug, _ := strings.Cut(p.id, "/")
		if first, dup := slugs[slug]; dup {
			g.refuse(p.id, "its slug is also %s's", first)
		}
		slugs[slug] = p.id
	}
	check := func(owner, title string, k mapfolder.Kind, es []capabilitytext.Entry) {
		seen := capabilitytext.Taken(title, k)
		for _, e := range es {
			a := mapfolder.Anchor(e.Heading)
			if seen[a] {
				g.refuse(consolidating.ID(owner, e.Slug), "its heading %q has the anchor of another heading of its file", e.Heading)
			}
			seen[a] = true
		}
	}
	for _, c := range out.Capabilities {
		for _, k := range mapfolder.Kinds {
			check(c.Name, c.Title(), k, c.Kinded(k))
		}
	}
	check(productID, out.Product, mapfolder.Rule, out.Rules)
}

// terms checks that every input term is in the output or listed as a term
// conflict, and that every output term is an input's.
func (g *guard) terms(inputs []capabilitytext.Capability, out consolidating.Output) {
	conflicts := slices.Clone(g.rec.TermConflicts)
	for _, m := range g.rec.Merged {
		conflicts = append(conflicts, m.TermConflicts...)
	}
	var have, had []capabilitytext.Term
	for _, c := range out.Capabilities {
		have = append(have, c.Terms...)
	}
	for _, c := range inputs {
		had = append(had, c.Terms...)
		for _, t := range c.Terms {
			kept := slices.ContainsFunc(have, t.Same)
			listed := slices.ContainsFunc(conflicts, func(x consolidating.TermConflict) bool {
				return strings.EqualFold(x.Name, t.Name) && x.Text == t.Text()
			})
			if !kept && !listed {
				g.refuse(c.Name+": "+t.Name, "the term is not in the output, and the record lists no term conflict for it")
			}
		}
	}
	for _, c := range out.Capabilities {
		for _, t := range c.Terms {
			if !slices.ContainsFunc(had, t.Same) {
				g.refuse(c.Name+": "+t.Name, "the term has no source among the recovered terms")
			}
		}
	}
}
