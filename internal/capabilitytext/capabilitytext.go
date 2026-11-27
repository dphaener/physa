// Package capabilitytext reads a recovered capability's single text and its
// terms into a model of its parts (head, sections, entries with their meta
// lines and anchors, terms) and renders the model back in the same form, so a
// capability nothing changed renders byte for byte as it was stored. It
// never words an entry: it only keeps, moves and renders the lines it read,
// and adds anchors to a meta line.
package capabilitytext

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/recovering"
)

var (
	slugSeg   = regexp.MustCompile("^`([^`]*)`$")
	prTok     = regexp.MustCompile(`^#\d+$`)
	commitTok = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	termStart = regexp.MustCompile(`^\*\*([^*]+)\*\*:`)
)

// Area is an area as recovery's order names it: its name together with the
// area it was split from, empty when it is not a part.
type Area struct {
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
}

func (a Area) String() string {
	if a.Parent == "" {
		return a.Name
	}
	return a.Parent + "/" + a.Name
}

// Entry is one rule, decision or question as its capability's text holds
// it: its exact lines from the heading line to its last line, and the blank
// lines after it.
type Entry struct {
	Kind    mapfolder.Kind
	Slug    string
	Heading string           // the heading line's text after "### "
	Lines   []string         // the heading line, the meta line, the body lines
	Gap     int              // blank lines after the entry
	Anchors []mapfile.Anchor // the anchors of the meta line, in its order
}

// Text is the entry's lines, one line end apart, without a last line end.
func (e Entry) Text() string { return strings.Join(e.Lines, "\n") }

// Meta is the entry's meta line, empty when it has none.
func (e Entry) Meta() string {
	if len(e.Lines) > 1 && strings.HasPrefix(e.Lines[1], "`") {
		return e.Lines[1]
	}
	return ""
}

// Words is every line of the entry but its meta line: the heading, the body
// and the Because: lines, which code never changes.
func (e Entry) Words() []string {
	if e.Meta() == "" {
		return slices.Clone(e.Lines)
	}
	return append([]string{e.Lines[0]}, e.Lines[2:]...)
}

// Body is the entry's body without its Because: line, one line end apart.
func (e Entry) Body() string {
	var out []string
	for _, l := range e.Words()[1:] {
		if !strings.HasPrefix(l, "Because:") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// Rest is the meta line's segments after the slug that hold no anchor: the
// person and the date, which code never changes.
func (e Entry) Rest() []string {
	var out []string
	for i, s := range strings.Split(e.Meta(), " · ") {
		if s = strings.TrimSpace(s); i > 0 && !isAnchors(s) {
			out = append(out, s)
		}
	}
	return out
}

// Section is one "## " section of a capability's text: its line, the lines
// before its first entry, and its entries.
type Section struct {
	Kind    mapfolder.Kind
	Line    string
	Intro   []string
	Entries []Entry
}

// Term is one glossary term: its "**Name**:" line and the _Avoid_ lines
// right after it, and the other lines after those, up to the next term.
type Term struct {
	Name  string
	Lines []string
	After []string
}

// Same says whether t and o are one term written the same way: names equal
// apart from case, every other byte equal.
func (t Term) Same(o Term) bool {
	if !strings.EqualFold(t.Name, o.Name) || len(t.Lines) != len(o.Lines) {
		return false
	}
	for i := range t.Lines {
		a, b := t.Lines[i], o.Lines[i]
		if i == 0 {
			a, b = a[len(termStart.FindString(a)):], b[len(termStart.FindString(b)):]
		}
		if strings.TrimSpace(a) != strings.TrimSpace(b) {
			return false
		}
	}
	return true
}

// Text is the term's lines, one line end apart.
func (t Term) Text() string { return strings.Join(t.Lines, "\n") }

// Capability is one recovered capability in parts.
type Capability struct {
	Name      string
	Area      Area
	Footprint recovering.Footprint
	Head      []string // front matter, title and overview, with the blank lines after them
	Sections  []Section
	TermsHead []string // lines of the terms before the first term
	Terms     []Term
}

// Parse reads file, a capability's single text as recovering.File writes
// it, and terms, its glossary terms, into a Capability named name in area.
// A text mapfolder.Split rejects is an error.
func Parse(name string, area Area, file, terms string) (Capability, error) {
	if _, err := mapfolder.Split(file); err != nil {
		return Capability{}, fmt.Errorf("capability %q: %w", name, err)
	}
	c := Capability{Name: name, Area: area}
	ls := lines(file)
	i := 0
	if len(ls) > 0 && ls[0] == "---" {
		for i = 1; i < len(ls) && ls[i] != "---"; i++ {
		}
		i++
	}
	for ; i < len(ls) && !strings.HasPrefix(ls[i], "## ") && !strings.HasPrefix(ls[i], "### "); i++ {
	}
	c.Head = slices.Clone(ls[:i])
	var cur *Entry
	fenced := false
	for ; i < len(ls); i++ {
		l := ls[i]
		if !fenced && strings.HasPrefix(l, "## ") {
			k, _ := kindOfWord(strings.TrimSpace(l[3:]))
			c.Sections = append(c.Sections, Section{Kind: k, Line: l})
			cur = nil
			continue
		}
		// Split accepts no entry before the first section, so one is open.
		s := &c.Sections[len(c.Sections)-1]
		switch {
		case fenced:
			fenced = !strings.HasPrefix(l, "```")
		case strings.HasPrefix(l, "### "):
			s.Entries = append(s.Entries, Entry{Kind: s.Kind, Heading: strings.TrimSpace(l[4:])})
			cur = &s.Entries[len(s.Entries)-1]
		case cur != nil:
			fenced = strings.HasPrefix(l, "```")
		}
		if cur == nil {
			s.Intro = append(s.Intro, l)
		} else {
			cur.Lines = append(cur.Lines, l)
		}
	}
	for si := range c.Sections {
		for ei := range c.Sections[si].Entries {
			e := &c.Sections[si].Entries[ei]
			for len(e.Lines) > 1 && strings.TrimSpace(e.Lines[len(e.Lines)-1]) == "" {
				e.Lines, e.Gap = e.Lines[:len(e.Lines)-1], e.Gap+1
			}
			e.Slug, e.Anchors = parseMeta(e.Meta())
		}
	}
	c.TermsHead, c.Terms = parseTerms(terms)
	return c, nil
}

// lines splits text by line, without the last line end.
func lines(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }

func kindOfWord(w string) (mapfolder.Kind, bool) {
	for _, k := range mapfolder.Kinds {
		if k.Word() == w {
			return k, true
		}
	}
	return 0, false
}

// parseMeta is a meta line's slug and anchors.
func parseMeta(meta string) (string, []mapfile.Anchor) {
	segs := strings.Split(meta, " · ")
	slug := ""
	if m := slugSeg.FindStringSubmatch(strings.TrimSpace(segs[0])); m != nil {
		slug = m[1]
	}
	var out []mapfile.Anchor
	for _, s := range segs[1:] {
		s = strings.TrimSpace(s)
		if rest, ok := strings.CutPrefix(s, "from "); ok {
			out = append(out, mapfile.Anchor{Kind: mapfile.Doc, Value: strings.TrimSpace(rest)})
		} else if isAnchors(s) {
			for _, t := range strings.Fields(s) {
				if prTok.MatchString(t) {
					out = append(out, mapfile.Anchor{Kind: mapfile.PR, Value: t[1:]})
				} else {
					out = append(out, mapfile.Anchor{Kind: mapfile.Commit, Value: t})
				}
			}
		}
	}
	return slug, out
}

// isAnchors says whether a meta segment holds anchors: a document's path or
// PR numbers and commits.
func isAnchors(seg string) bool {
	if strings.HasPrefix(seg, "from ") {
		return true
	}
	toks := strings.Fields(seg)
	for _, t := range toks {
		if !prTok.MatchString(t) && !commitTok.MatchString(t) {
			return false
		}
	}
	return len(toks) > 0
}

// AnchorText is an anchor as the meta line writes it: "#12", a commit, or
// "from <path>".
func AnchorText(a mapfile.Anchor) string {
	switch a.Kind {
	case mapfile.PR:
		return "#" + a.Value
	case mapfile.Doc:
		return "from " + a.Value
	}
	return a.Value
}

// AddAnchors adds to e's meta line every anchor of others it does not have,
// PRs and commits as one segment, then each document as its own, after the
// segments it has, and returns the anchors added. The heading, body and
// Because: lines are never changed; an entry with no meta line gets none.
func AddAnchors(e *Entry, others ...Entry) []string {
	if e.Meta() == "" {
		return nil
	}
	have := map[mapfile.Anchor]bool{}
	for _, a := range e.Anchors {
		have[a] = true
	}
	var toks, docs, added []string
	for _, o := range others {
		for _, a := range o.Anchors {
			if have[a] {
				continue
			}
			have[a] = true
			e.Anchors = append(e.Anchors, a)
			added = append(added, AnchorText(a))
			if a.Kind == mapfile.Doc {
				docs = append(docs, AnchorText(a))
			} else {
				toks = append(toks, AnchorText(a))
			}
		}
	}
	meta := e.Lines[1]
	if len(toks) > 0 {
		meta += " · " + strings.Join(toks, " ")
	}
	for _, d := range docs {
		meta += " · " + d
	}
	e.Lines[1] = meta
	return added
}

// Rename gives e the slug s on its meta line.
func Rename(e *Entry, s string) {
	if m := e.Meta(); m != "" {
		_, rest, _ := strings.Cut(m, " · ")
		e.Lines[1] = "`" + s + "`"
		if strings.Contains(m, " · ") {
			e.Lines[1] += " · " + rest
		}
	}
	e.Slug = s
}

func parseTerms(terms string) ([]string, []Term) {
	if terms == "" {
		return nil, nil
	}
	var head []string
	var out []Term
	ls := lines(terms)
	for i := 0; i < len(ls); i++ {
		m := termStart.FindStringSubmatch(ls[i])
		if m == nil {
			if len(out) == 0 {
				head = append(head, ls[i])
			} else {
				out[len(out)-1].After = append(out[len(out)-1].After, ls[i])
			}
			continue
		}
		t := Term{Name: strings.TrimSpace(m[1]), Lines: []string{ls[i]}}
		for i+1 < len(ls) && strings.HasPrefix(ls[i+1], "_Avoid_:") {
			i++
			t.Lines = append(t.Lines, ls[i])
		}
		out = append(out, t)
	}
	return head, out
}

// Promise is the value of the front matter's promise line.
func (c Capability) Promise() string {
	if i := c.PromiseLine(); i >= 0 {
		_, v, _ := strings.Cut(c.Head[i], ":")
		return strings.TrimSpace(v)
	}
	return ""
}

// PromiseLine is the index in Head of the front matter's promise line, -1
// without one.
func (c Capability) PromiseLine() int {
	if len(c.Head) == 0 || c.Head[0] != "---" {
		return -1
	}
	for i := 1; i < len(c.Head) && c.Head[i] != "---"; i++ {
		if k, _, ok := strings.Cut(c.Head[i], ":"); ok && strings.TrimSpace(k) == "promise" {
			return i
		}
	}
	return -1
}

// Title is the text of the title line, after "# ".
func (c Capability) Title() string {
	for _, l := range c.Head {
		if strings.HasPrefix(l, "# ") {
			return strings.TrimSpace(l[2:])
		}
	}
	return ""
}

// Overview is the overview's lines, one line end apart, empty without one.
func (c Capability) Overview() string {
	var out []string
	titled := false
	for _, l := range c.Head {
		switch {
		case strings.HasPrefix(l, "# "):
			titled = true
		case titled && strings.TrimSpace(l) != "":
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// Entries is every entry, in section order and each section's order.
func (c Capability) Entries() []Entry {
	var out []Entry
	for _, s := range c.Sections {
		out = append(out, s.Entries...)
	}
	return out
}

// Kinded is the entries of kind k, in order.
func (c Capability) Kinded(k mapfolder.Kind) []Entry {
	var out []Entry
	for _, e := range c.Entries() {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// Find is where the entry with slug sits, false when none has it.
func (c *Capability) Find(slug string) (si, ei int, ok bool) {
	for si := range c.Sections {
		for ei := range c.Sections[si].Entries {
			if c.Sections[si].Entries[ei].Slug == slug {
				return si, ei, true
			}
		}
	}
	return 0, 0, false
}

// Entry is a pointer to the entry with slug, nil when none has it.
func (c *Capability) Entry(slug string) *Entry {
	if si, ei, ok := c.Find(slug); ok {
		return &c.Sections[si].Entries[ei]
	}
	return nil
}

// Remove takes out the entry with slug and returns it, false when none has
// it. The gap it leaves is kept.
func (c *Capability) Remove(slug string) (Entry, bool) {
	si, ei, ok := c.Find(slug)
	if !ok {
		return Entry{}, false
	}
	s := &c.Sections[si]
	e := s.Entries[ei]
	s.Entries = slices.Delete(s.Entries, ei, ei+1)
	return e, true
}

// Add puts e last in the section of its kind, opening the section, in the
// order rules, decisions, questions, when the text has none.
func (c *Capability) Add(e Entry) {
	at := slices.IndexFunc(c.Sections, func(s Section) bool { return s.Kind == e.Kind })
	if at < 0 {
		at = slices.IndexFunc(c.Sections, func(s Section) bool { return s.Kind > e.Kind })
		if at < 0 {
			at = len(c.Sections)
		}
		c.gapBefore(at)
		c.Sections = slices.Insert(c.Sections, at, Section{Kind: e.Kind, Line: "## " + e.Kind.Word(), Intro: []string{""}})
	}
	s := &c.Sections[at]
	if n := len(s.Entries); n > 0 {
		s.Entries[n-1].Gap = max(s.Entries[n-1].Gap, 1)
	} else if len(s.Intro) == 0 || strings.TrimSpace(s.Intro[len(s.Intro)-1]) != "" {
		s.Intro = append(s.Intro, "")
	}
	e.Gap = 1
	s.Entries = append(s.Entries, e)
}

// gapBefore makes sure a blank line comes before section at.
func (c *Capability) gapBefore(at int) {
	if at == 0 {
		if n := len(c.Head); n == 0 || strings.TrimSpace(c.Head[n-1]) != "" {
			c.Head = append(c.Head, "")
		}
		return
	}
	s := &c.Sections[at-1]
	if n := len(s.Entries); n > 0 {
		s.Entries[n-1].Gap = max(s.Entries[n-1].Gap, 1)
	} else if n := len(s.Intro); n == 0 || strings.TrimSpace(s.Intro[n-1]) != "" {
		s.Intro = append(s.Intro, "")
	}
}

// File renders the capability's single text: its head, then each section
// with its entries, the blank lines it read kept and none at the end.
func (c Capability) File() string {
	out := slices.Clone(c.Head)
	for _, s := range c.Sections {
		out = append(append(out, s.Line), s.Intro...)
		for _, e := range s.Entries {
			out = append(out, e.Lines...)
			for range e.Gap {
				out = append(out, "")
			}
		}
	}
	return closeText(out)
}

// TermsText renders the capability's terms, as they were read when no term
// was taken or added.
func (c Capability) TermsText() string {
	out := slices.Clone(c.TermsHead)
	for _, t := range c.Terms {
		out = append(append(out, t.Lines...), t.After...)
	}
	return closeText(out)
}

// AddTerm puts t last among the terms, a blank line before it.
func (c *Capability) AddTerm(t Term) {
	if n := len(c.Terms); n > 0 {
		if a := c.Terms[n-1].After; len(a) == 0 || strings.TrimSpace(a[len(a)-1]) != "" {
			c.Terms[n-1].After = append(c.Terms[n-1].After, "")
		}
	}
	t.After = nil
	c.Terms = append(c.Terms, t)
}

// closeText joins ls with line ends, without the blank lines at the end,
// ending in one line end; empty when nothing is left.
func closeText(ls []string) string {
	for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
		ls = ls[:len(ls)-1]
	}
	if len(ls) == 0 {
		return ""
	}
	return strings.Join(ls, "\n") + "\n"
}

// Clone is a deep copy of c, so changing it never changes c.
func (c Capability) Clone() Capability {
	c.Head = slices.Clone(c.Head)
	c.Sections = slices.Clone(c.Sections)
	for i := range c.Sections {
		s := &c.Sections[i]
		s.Intro = slices.Clone(s.Intro)
		s.Entries = slices.Clone(s.Entries)
		for j := range s.Entries {
			s.Entries[j] = s.Entries[j].Clone()
		}
	}
	c.TermsHead = slices.Clone(c.TermsHead)
	c.Terms = slices.Clone(c.Terms)
	for i := range c.Terms {
		c.Terms[i].Lines, c.Terms[i].After = slices.Clone(c.Terms[i].Lines), slices.Clone(c.Terms[i].After)
	}
	c.Footprint.Sources = slices.Clone(c.Footprint.Sources)
	c.Footprint.Tests = slices.Clone(c.Footprint.Tests)
	c.Footprint.Docs = slices.Clone(c.Footprint.Docs)
	c.Footprint.Other = slices.Clone(c.Footprint.Other)
	return c
}

// Clone is a deep copy of e.
func (e Entry) Clone() Entry {
	e.Lines, e.Anchors = slices.Clone(e.Lines), slices.Clone(e.Anchors)
	return e
}

// Folder is the capability's folder as the map writes it, split from File.
func (c Capability) Folder() (mapfolder.Folder, error) { return mapfolder.Split(c.File()) }

// Lines counts a file's lines as the map check does, as wc -l counts them.
func Lines(text string) int { return strings.Count(text, "\n") }

// Taken is the anchors a kind file's headings may not share: its title's,
// "Contents" and "Entries", for a file under the title title.
func Taken(title string, k mapfolder.Kind) map[string]bool {
	return map[string]bool{
		mapfolder.Anchor(title + " · " + k.Word()): true,
		mapfolder.Anchor("Contents"):               true,
		mapfolder.Anchor("Entries"):                true,
	}
}

// Anchors is the heading anchors of a kind file holding es under the title
// title, with its title's and its sections'.
func Anchors(title string, k mapfolder.Kind, es []Entry) map[string]bool {
	seen := Taken(title, k)
	for _, e := range es {
		seen[mapfolder.Anchor(e.Heading)] = true
	}
	return seen
}

// Clash is the first heading of es whose anchor a heading before it, or the
// file's title or sections, already has; empty when none.
func Clash(title string, k mapfolder.Kind, es []Entry) string {
	seen := Taken(title, k)
	for _, e := range es {
		a := mapfolder.Anchor(e.Heading)
		if seen[a] {
			return e.Heading
		}
		seen[a] = true
	}
	return ""
}

// Union is footprint a with every file of b it lacks, in order.
func Union(a, b recovering.Footprint) recovering.Footprint {
	a = Capability{Footprint: a}.Clone().Footprint
	for _, f := range b.Sources {
		if !slices.ContainsFunc(a.Sources, func(o recovering.SourceFile) bool { return o.Path == f.Path }) {
			a.Sources = append(a.Sources, f)
		}
	}
	add := func(to, from []string) []string {
		for _, p := range from {
			if !slices.Contains(to, p) {
				to = append(to, p)
			}
		}
		return to
	}
	a.Tests, a.Docs, a.Other = add(a.Tests, b.Tests), add(a.Docs, b.Docs), add(a.Other, b.Other)
	return a
}
