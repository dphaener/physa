// Package mapslice assembles a slice: the part of the map one task needs. A
// bare slice is the floor and the headings: the product's rules, and for each
// capability an agent chose its promise, overview and rules in full, with only
// the headings of its decisions and questions. The agent then takes the
// entries its task touches by their slugs, from anywhere in the map. It never
// matches, ranks or searches the map; it takes only what is named, and the
// same map and request always give the same slice. Every slice carries the
// slice skill, and no slice is built from a map the check would reject. It
// also parses the flags of physa map slice into a Request.
package mapslice

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
)

// skill is the slice skill: the judgment of which entries to take and what
// to do with them, printed as every slice's Reading further section. It is
// embedded in Physa only and never read from the repo, so a team cannot
// replace it.
//
//go:embed skill.md
var skill string

// Request is what an agent asks for: either the capabilities its task
// touches, with the kinds of entry to print in full in place of their
// headings, or the entries it chose, by slug. A request never asks for both.
type Request struct {
	Capabilities                 []string
	WithDecisions, WithQuestions bool
	Entries                      []string // slugs, in the order asked
}

// Slice is an assembled bare slice, ready to write as markdown or JSON. Field
// order is the JSON field order.
type Slice struct {
	Capabilities   []string  `json:"capabilities"`      // chosen, deduplicated, sorted
	Product        *Section  `json:"product,omitempty"` // nil without the product
	Sections       []Section `json:"sections"`          // one per chosen capability
	Terms          []Term    `json:"terms"`
	ReadingFurther string    `json:"reading_further"` // the slice skill
	Trailers       []string  `json:"trailers"`
}

// Section is the product or one capability: its rules and any kinds asked for
// in full as entries, and the other kinds as headings.
type Section struct {
	Name     string    `json:"name"` // "product" for the product
	Title    string    `json:"title"`
	File     string    `json:"file"`
	Promise  string    `json:"promise"`
	Overview string    `json:"overview"` // empty for the product
	Entries  []Entry   `json:"entries"`  // rules, then decisions, then questions
	Contents []Heading `json:"contents"` // decisions, then questions, shown by heading only
}

// Heading is one entry shown by its slug and heading alone, to be taken with
// physa map slice --entry.
type Heading struct {
	Kind    string `json:"kind"` // "decision" or "question"
	Slug    string `json:"slug"`
	Heading string `json:"heading"`
}

// Entry is a rule, decision or question in full.
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

// Term is a glossary term that appears in the capabilities' text.
type Term struct {
	Name       string   `json:"name"`
	Definition string   `json:"definition"`
	Avoid      []string `json:"avoid,omitempty"`
}

// Read reads the map in dir/.physa for a slice. It fails when the map has a
// problem physa check map would report, with anchors checked for their form
// only, so neither history nor the forge is read; the error names every
// problem by file and line.
func Read(dir string) (*mapfile.Map, error) {
	m, res, err := mapcheck.CheckRead(dir, mapcheck.Options{FormOnly: true})
	if err != nil {
		return nil, err
	}
	if n := len(res.Problems); n > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "the map has %s that physa check map reports; fix the map first:", plural(n, "problem", "problems"))
		for _, p := range res.Problems {
			b.WriteString("\n" + p.String())
		}
		return nil, errors.New(b.String())
	}
	return m, nil
}

// Write builds what f asks for from m and writes it to w, as JSON when f.JSON
// is set: the entries its request names, or else the slice of its
// capabilities. Nothing is written when the request fails.
func Write(w io.Writer, m *mapfile.Map, f Flags) error {
	if len(f.Req.Entries) > 0 {
		t, err := Take(m, f.Req.Entries)
		if err != nil {
			return err
		}
		if f.JSON {
			return WriteTakenJSON(w, t)
		}
		return WriteTakenMD(w, t)
	}
	s, err := Build(m, f.Req)
	if err != nil {
		return err
	}
	if f.JSON {
		return WriteJSON(w, s)
	}
	return WriteMD(w, s)
}

// Build assembles the bare slice r asks for from m. It fails when r names no
// capability, names one the map does not have, or names entries, which Take
// takes.
func Build(m *mapfile.Map, r Request) (*Slice, error) {
	if m == nil {
		return nil, errors.New("no map to slice")
	}
	if len(r.Entries) > 0 {
		return nil, errors.New("a slice names capabilities or entries, not both")
	}
	if len(r.Capabilities) == 0 {
		return nil, errors.New("no capabilities named; run physa map index to see them")
	}
	byName := map[string]*mapfile.File{}
	for _, c := range m.Capabilities {
		byName[c.Name] = c
	}
	seen := map[string]bool{}
	var names []string
	for _, n := range r.Capabilities {
		if byName[n] == nil {
			return nil, fmt.Errorf("unknown capability %q; run physa map index to see them", n)
		}
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	full := map[mapfile.Kind]bool{mapfile.Rule: true, mapfile.Decision: r.WithDecisions, mapfile.Question: r.WithQuestions}
	s := &Slice{Capabilities: names, Sections: []Section{}, Terms: []Term{}, ReadingFurther: skill, Trailers: []string{}}
	if m.Product != nil {
		p := toSection(m.Product, full, nil)
		s.Product = &p
	}
	// Only the capabilities' own text brings in a term, never the product's.
	var text strings.Builder
	for _, n := range names {
		s.Sections = append(s.Sections, toSection(byName[n], full, &text))
		s.Trailers = append(s.Trailers, "Capability: "+n)
	}
	used := make([]bool, len(m.Glossary))
	for _, h := range findTerms(text.String(), m.Glossary) {
		used[h.term] = true
	}
	for i, t := range m.Glossary {
		if used[i] {
			s.Terms = append(s.Terms, Term{Name: t.Name, Definition: t.Definition, Avoid: t.Avoid})
		}
	}
	return s, nil
}

// toSection turns f into a section, the kinds full marks in full and the rest
// as headings. With text set, f is a capability: its overview is kept, and
// the text shown is added to text, normalized, for the terms; each piece is
// ended by a byte that is never part of a word, so no term spans two pieces.
func toSection(f *mapfile.File, full map[mapfile.Kind]bool, text *strings.Builder) Section {
	s := Section{Name: f.Name, Title: f.Title, File: f.Path + "/", Promise: f.Promise, Entries: []Entry{}, Contents: []Heading{}}
	own := func(parts ...string) {
		if text == nil {
			return
		}
		for _, p := range parts {
			writeNormal(text, p)
			text.WriteString("\x00")
		}
	}
	if text != nil {
		s.Overview = f.Overview
	}
	own(s.Title, s.Promise, s.Overview)
	for _, k := range []mapfile.Kind{mapfile.Rule, mapfile.Decision, mapfile.Question} {
		for _, e := range f.Entries {
			switch {
			case e.Kind != k:
			case full[k]:
				s.Entries = append(s.Entries, toEntry(e))
				own(e.Heading, e.Body, e.Because)
				own(e.Rejected...)
			default:
				s.Contents = append(s.Contents, Heading{Kind: k.String(), Slug: e.Slug, Heading: e.Heading})
				own(e.Heading)
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
