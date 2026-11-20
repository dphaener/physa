// Package mapslice assembles a slice: the part of the map one task needs,
// from the capabilities an agent chose, within a token budget. It never
// matches or ranks capabilities; it takes only the ones named. The same map
// and request always give the same slice.
package mapslice

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dphaener/physa/internal/mapfile"
)

// Request is what an agent asks for: the capabilities its task touches,
// whether decisions or questions must be kept whatever the budget, and the
// budget in tokens. A slice has no default budget: 0 means none, and nothing
// is cut.
type Request struct {
	Capabilities                 []string
	WithDecisions, WithQuestions bool
	Budget                       int
}

// Slice is an assembled slice, ready to write as markdown or JSON. Field
// order is the JSON field order.
type Slice struct {
	Capabilities []string  `json:"capabilities"` // chosen, deduplicated, sorted
	Budget       int       `json:"budget"`       // 0 when the request named none
	Used         int       `json:"used"`         // estimated tokens of product, capabilities and terms
	OverBudget   bool      `json:"over_budget"`
	Floor        []string  `json:"floor,omitempty"`   // what is never cut, when it alone is over budget
	Product      *Section  `json:"product,omitempty"` // nil without product.md
	Sections     []Section `json:"sections"`          // one per chosen capability
	Terms        []Term    `json:"terms"`
	LeftOut      []Cut     `json:"left_out"`
	Trailers     []string  `json:"trailers"`
}

// Section is product.md or one capability, with the entries the budget kept.
type Section struct {
	Name    string  `json:"name"` // "product" for product.md
	Title   string  `json:"title"`
	File    string  `json:"file"`
	Promise string  `json:"promise"`
	Entries []Entry `json:"entries"` // rules, then decisions, then questions
}

// Entry is a rule, decision or question as the slice shows it. A part the
// budget cut is empty.
type Entry struct {
	Slug     string   `json:"slug"`
	Kind     string   `json:"kind"` // "rule", "decision" or "question"
	Heading  string   `json:"heading"`
	Role     string   `json:"role,omitempty"` // "decided" or "raised"
	Person   string   `json:"person,omitempty"`
	How      string   `json:"how,omitempty"`
	Date     string   `json:"date,omitempty"`
	Anchors  []string `json:"anchors,omitempty"` // "#12", "a1b2c3d", "from docs/x.md"
	Body     string   `json:"body,omitempty"`
	Because  string   `json:"because,omitempty"`
	Rejected []string `json:"rejected,omitempty"`
}

// Term is a glossary term that appears in the slice's text.
type Term struct {
	Name       string   `json:"name"`
	Definition string   `json:"definition"`
	Avoid      []string `json:"avoid,omitempty"`
}

// The pieces the budget can cut: a whole entry, its Because line, its
// rejected options, or a term.
const (
	PartEntry    = "entry"
	PartBecause  = "because"
	PartRejected = "rejected"
	PartTerm     = "term"
)

// Cut is one piece the budget left out and the command that shows it. A cut
// term has its name as Slug and "glossary" as Capability.
type Cut struct {
	Slug       string `json:"slug"`
	Capability string `json:"capability"` // "product" for product.md
	Part       string `json:"part"`       // PartEntry, PartBecause, PartRejected or PartTerm
	Command    string `json:"command"`
}

// Build assembles the slice r asks for from m. It fails when r names no
// capability, names one the map does not have, or has a negative budget.
func Build(m *mapfile.Map, r Request) (*Slice, error) {
	a, err := newAssembler(m, r)
	if err != nil {
		return nil, err
	}
	// Cost only falls as more of the cut order is cut, so the fewest cuts
	// that fit are found by a binary search, after trying both ends.
	fits := func(k int) bool {
		_, cost := a.assemble(k)
		return cost <= a.budget
	}
	k, over := 0, false
	switch n := len(a.cuts); {
	case a.budget == 0, fits(0):
	case !fits(n):
		k, over = n, true
	default:
		k = 1 + sort.Search(n-1, func(i int) bool { return fits(i + 1) })
	}
	s, cost := a.assemble(k)
	s.Used, s.OverBudget = cost, over
	if over {
		s.Floor = []string{"product rules", "promises", "rules"}
		if r.WithDecisions {
			s.Floor = append(s.Floor, "decisions")
		}
		if r.WithQuestions {
			s.Floor = append(s.Floor, "questions")
		}
	}
	return s, nil
}

// cut is one step of the budget's cut order: a part of secs[sec].Entries[ent],
// or terms[term] when part is PartTerm.
type cut struct {
	sec, ent, term int
	part           string
}

// assembler holds a request's full, uncut content and its cut order.
type assembler struct {
	req    Request
	names  []string
	budget int
	secs   []Section // product.md first when present, then the capabilities
	prod   bool      // secs[0] is product.md
	cuts   []cut
	terms  []mapfile.Term
	// matches[sec][ent] are the terms each part of an entry mentions;
	// header[sec] are those of a section's title and promise.
	header  [][]bool
	matches [][]partTerms
	buf     strings.Builder
	command string // the command that shows everything the budget cut
}

type partTerms struct{ core, because, rejected []bool }

func newAssembler(m *mapfile.Map, r Request) (*assembler, error) {
	if m == nil {
		return nil, errors.New("no map to slice")
	}
	if len(r.Capabilities) == 0 {
		return nil, errors.New("no capabilities named; run physa index to see them")
	}
	if r.Budget < 0 {
		return nil, fmt.Errorf("budget must be positive, got %d", r.Budget)
	}
	byName := map[string]*mapfile.File{}
	for _, c := range m.Capabilities {
		byName[c.Name] = c
	}
	seen := map[string]bool{}
	var names []string
	for _, n := range r.Capabilities {
		if byName[n] == nil {
			return nil, fmt.Errorf("unknown capability %q; run physa index to see them", n)
		}
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	a := &assembler{req: r, names: names, budget: r.Budget, terms: m.Glossary,
		command: "physa slice " + strings.Join(names, " ")}
	if m.Product != nil {
		a.secs, a.prod = append(a.secs, toSection(m.Product)), true
	}
	for _, n := range names {
		a.secs = append(a.secs, toSection(byName[n]))
	}
	a.order()
	a.match()
	return a, nil
}

func toSection(f *mapfile.File) Section {
	s := Section{Name: f.Name, Title: f.Title, File: f.Path, Promise: f.Promise, Entries: []Entry{}}
	for _, k := range []mapfile.Kind{mapfile.Rule, mapfile.Decision, mapfile.Question} {
		for _, e := range f.Entries {
			if e.Kind == k {
				s.Entries = append(s.Entries, toEntry(e))
			}
		}
	}
	return s
}

func toEntry(e mapfile.Entry) Entry {
	out := Entry{
		Slug: e.Slug, Kind: e.Kind.String(), Heading: e.Heading,
		Role: e.Role, Person: e.Person, How: e.How, Date: e.Date,
		Body: e.Body, Because: e.Because,
	}
	if out.Person != "" && out.Role == "" {
		out.Role = "decided"
		if e.Kind == mapfile.Question {
			out.Role = "raised"
		}
	}
	for _, an := range e.Anchors {
		switch an.Kind {
		case mapfile.PR:
			out.Anchors = append(out.Anchors, "#"+an.Value)
		case mapfile.Doc:
			out.Anchors = append(out.Anchors, "from "+an.Value)
		default:
			out.Anchors = append(out.Anchors, an.Value)
		}
	}
	if len(e.Rejected) > 0 {
		out.Rejected = append([]string(nil), e.Rejected...)
	}
	return out
}

// cuttable reports whether the budget may cut an entry of kind k.
func (a *assembler) cuttable(k string) bool {
	switch k {
	case "decision":
		return !a.req.WithDecisions
	case "question":
		return !a.req.WithQuestions
	}
	return false
}

// order lists the cuts in the budget's order: product.md's decisions and
// questions, then every rejected-option list, then terms, then questions,
// then decisions' Because lines, then decisions. Within each tier the pieces
// lowest in the output go first.
func (a *assembler) order() {
	add := func(si int, part string, take func(Entry) bool) {
		es := a.secs[si].Entries
		for ei := len(es) - 1; ei >= 0; ei-- {
			if a.cuttable(es[ei].Kind) && take(es[ei]) {
				a.cuts = append(a.cuts, cut{sec: si, ent: ei, part: part})
			}
		}
	}
	each := func(part string, take func(Entry) bool) {
		for si := len(a.secs) - 1; si >= 0; si-- {
			if si > 0 || !a.prod {
				add(si, part, take)
			}
		}
	}
	kind := func(k string) func(Entry) bool { return func(e Entry) bool { return e.Kind == k } }
	if a.prod {
		add(0, PartEntry, func(e Entry) bool { return e.Kind != "rule" })
	}
	each(PartRejected, func(e Entry) bool { return len(e.Rejected) > 0 })
	for ti := len(a.terms) - 1; ti >= 0; ti-- {
		a.cuts = append(a.cuts, cut{term: ti, part: PartTerm})
	}
	each(PartEntry, kind("question"))
	each(PartBecause, func(e Entry) bool { return e.Kind == "decision" && e.Because != "" })
	each(PartEntry, kind("decision"))
}

// match finds, once, the terms each piece of the capabilities' text mentions;
// product.md's text never brings in a term. All pieces are joined into one
// text, split by a byte that is never part of a word, so each term's name and
// Avoid words are searched for once.
func (a *assembler) match() {
	var (
		text   strings.Builder
		starts []int // starts[p] is where piece p begins in text
	)
	piece := func(parts ...string) []bool {
		starts = append(starts, text.Len())
		for _, p := range parts {
			writeNormal(&text, p)
			text.WriteString(" ")
		}
		text.WriteString("\x00")
		return make([]bool, len(a.terms))
	}
	var hits [][]bool // hits[p] is piece p's terms
	a.header = make([][]bool, len(a.secs))
	a.matches = make([][]partTerms, len(a.secs))
	for si, s := range a.secs {
		own := func(parts ...string) []string {
			if a.prod && si == 0 {
				return nil
			}
			return parts
		}
		a.header[si] = piece(own(s.Title, s.Promise)...)
		hits = append(hits, a.header[si])
		a.matches[si] = make([]partTerms, len(s.Entries))
		for ei, e := range s.Entries {
			mt := partTerms{piece(own(e.Heading, e.Body)...), piece(own(e.Because)...), piece(own(e.Rejected...)...)}
			a.matches[si][ei] = mt
			hits = append(hits, mt.core, mt.because, mt.rejected)
		}
	}
	all := text.String()
	for _, h := range findTerms(all, a.terms) {
		hits[sort.SearchInts(starts, h.at+1)-1][h.term] = true
	}
}

// normalize lowercases s, turns every run of white space into one space and
// trims it.
func normalize(s string) string {
	var b strings.Builder
	writeNormal(&b, s)
	return b.String()
}

func writeNormal(b *strings.Builder, s string) {
	space := false
	n := b.Len()
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > n {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(unicode.ToLower(r))
	}
}

type hit struct{ at, term int }

// findTerms returns where in text (normalized) each term's name or Avoid
// words occur with no letter or digit directly before or after them. It
// walks text's words once, trying only the phrases that start with each.
func findTerms(text string, terms []mapfile.Term) []hit {
	type phrase struct {
		text string
		term int
	}
	byFirst := map[string][]phrase{}
	var odd []phrase // phrases that do not start with a letter or digit
	for i, t := range terms {
		for _, w := range append([]string{t.Name}, t.Avoid...) {
			p := phrase{normalize(w), i}
			if first := p.text[:wordEnd(p.text, 0)]; first != "" {
				byFirst[first] = append(byFirst[first], p)
			} else if p.text != "" {
				odd = append(odd, p)
			}
		}
	}
	var hits []hit
	match := func(at int, p phrase) {
		if strings.HasPrefix(text[at:], p.text) {
			after, _ := utf8.DecodeRuneInString(text[at+len(p.text):])
			before, _ := utf8.DecodeLastRuneInString(text[:at])
			if !isWord(after) && !isWord(before) {
				hits = append(hits, hit{at, p.term})
			}
		}
	}
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if !isWord(r) {
			for _, p := range odd {
				match(i, p)
			}
			i += size
			continue
		}
		end := wordEnd(text, i)
		for _, p := range byFirst[text[i:end]] {
			match(i, p)
		}
		i = end
	}
	return hits
}

// wordEnd returns the end of the run of letters and digits starting at i.
func wordEnd(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isWord(r) {
			break
		}
		i += size
	}
	return i
}

func isWord(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// assemble builds the slice with the first k cuts applied and returns it with
// its budget cost: the estimated tokens of its product, capability and term
// sections.
func (a *assembler) assemble(k int) (*Slice, int) {
	type gone struct{ entry, because, rejected bool }
	cutOf := make([][]gone, len(a.secs))
	for si, s := range a.secs {
		cutOf[si] = make([]gone, len(s.Entries))
	}
	termCut := make([]bool, len(a.terms))
	for _, c := range a.cuts[:k] {
		if c.part == PartTerm {
			termCut[c.term] = true
			continue
		}
		g := &cutOf[c.sec][c.ent]
		switch c.part {
		case PartEntry:
			g.entry = true
		case PartBecause:
			g.because = true
		case PartRejected:
			g.rejected = true
		}
	}
	s := &Slice{
		Capabilities: a.names, Budget: a.budget,
		Sections: []Section{}, Terms: []Term{}, LeftOut: []Cut{}, Trailers: []string{},
	}
	used := make([]bool, len(a.terms))
	union := func(hits []bool) {
		for i, h := range hits {
			used[i] = used[i] || h
		}
	}
	for si, sec := range a.secs {
		out := sec
		out.Entries = []Entry{}
		union(a.header[si])
		for ei, e := range sec.Entries {
			g, mt := cutOf[si][ei], a.matches[si][ei]
			if g.entry {
				s.LeftOut = append(s.LeftOut, a.leftOut(sec.Name, e, PartEntry))
				continue
			}
			union(mt.core)
			if g.because {
				e.Because = ""
				s.LeftOut = append(s.LeftOut, a.leftOut(sec.Name, e, PartBecause))
			} else {
				union(mt.because)
			}
			if g.rejected {
				e.Rejected = nil
				s.LeftOut = append(s.LeftOut, a.leftOut(sec.Name, e, PartRejected))
			} else {
				union(mt.rejected)
			}
			out.Entries = append(out.Entries, e)
		}
		if a.prod && si == 0 {
			s.Product = &out
		} else {
			s.Sections = append(s.Sections, out)
		}
	}
	for i, t := range a.terms {
		switch {
		case !used[i]:
		case termCut[i]:
			s.LeftOut = append(s.LeftOut, Cut{Slug: t.Name, Capability: "glossary", Part: PartTerm, Command: a.command})
		default:
			s.Terms = append(s.Terms, Term{Name: t.Name, Definition: t.Definition, Avoid: t.Avoid})
		}
	}
	for _, n := range a.names {
		s.Trailers = append(s.Trailers, "Capability: "+n)
	}
	if len(a.names) > 0 {
		s.Trailers = append(s.Trailers, "Loaded: "+strings.Join(a.names, ", "))
	}
	a.buf.Reset()
	writeBody(&a.buf, s)
	return s, tokens(a.buf.Len())
}

func (a *assembler) leftOut(capability string, e Entry, part string) Cut {
	return Cut{Slug: e.Slug, Capability: capability, Part: part, Command: a.command}
}

// tokens estimates the tokens of n bytes of text: one per four bytes,
// rounded up.
func tokens(n int) int { return (n + 3) / 4 }
