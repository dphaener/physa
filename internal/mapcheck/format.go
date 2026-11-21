package mapcheck

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
)

// The size limits of a folder's files (09 §6), the same for the product and
// every capability. Going over one fails the check; decisions.md and
// questions.md have none.
const (
	MaxIndexLines = 30
	MaxRulesLines = 150
)

// fmtHint closes every message about something `physa map fmt` rewrites.
const fmtHint = "run physa map fmt"

// folder checks the format of one folder (09 §2–§3) and its limits (§6).
// known holds the "path:line" of every problem the reader reported, so a
// fault it already named is not named twice.
func (c *checker) folder(f *mapfile.File, known map[string]bool) {
	var kinds []mapfile.Kind
	for _, p := range f.Parts {
		if p.Kind != 0 {
			kinds = append(kinds, p.Kind)
			c.kindPart(f, p)
		}
	}
	for _, p := range f.Parts {
		if p.Kind == 0 {
			c.indexPart(f, p, kinds, known)
		}
	}
}

// indexPart checks index.md: its one grammar (front matter, title, one
// overview paragraph, the links, nothing else), that it links exactly the
// kind files the folder has, in order, and its limit.
func (c *checker) indexPart(f *mapfile.File, p mapfile.Part, kinds []mapfile.Kind, known map[string]bool) {
	if p.Lines > MaxIndexLines {
		c.over(p.Path, p.Lines, MaxIndexLines)
	}
	if f.Title != "" {
		var le *mapfolder.FormatError
		_, err := mapfolder.FormatIndex(p.Text, nil)
		if errors.As(err, &le) && !known[at(p.Path, le.Line)] {
			c.add(p.Path, le.Line, "%s", le.Message)
		}
	}
	var linked []mapfile.Kind
	last := max(p.TitleLine, f.OverviewLine)
	for _, l := range p.Contents {
		i := slices.IndexFunc(mapfolder.Kinds, func(k mapfile.Kind) bool { return strings.TrimSpace(l.Text) == mapfolder.IndexLink(k) })
		if i < 0 {
			continue // not a link: the grammar names it
		}
		k := mapfolder.Kinds[i]
		last = l.No
		switch {
		case !slices.Contains(kinds, k):
			c.add(p.Path, l.No, "links %s, which this folder does not have: remove the link", k.File())
		case slices.Contains(linked, k):
			c.add(p.Path, l.No, "links %s twice; %s", k.File(), fmtHint)
		case len(linked) > 0 && k < linked[len(linked)-1]:
			c.add(p.Path, l.No, "link to %s is out of order: want rules.md, decisions.md, questions.md in that order; %s", k.File(), fmtHint)
			linked = append(linked, k)
		default:
			linked = append(linked, k)
		}
	}
	for _, k := range kinds {
		if !slices.Contains(linked, k) {
			c.add(p.Path, last, "missing the link %q to %s, which this folder has; %s", mapfolder.IndexLink(k), k.File(), fmtHint)
		}
	}
}

// kindPart checks rules.md, decisions.md or questions.md: that it has
// entries, its title, its two sections, its headings, that its contents
// match its entries, and its limit.
func (c *checker) kindPart(f *mapfile.File, p mapfile.Part) {
	var entries []*mapfile.Entry
	for i := range f.Entries {
		if f.Entries[i].Path == p.Path {
			entries = append(entries, &f.Entries[i])
		}
	}
	if len(entries) == 0 {
		c.add(p.Path, 0, "no entries: a kind with no entries has no file; delete it and its link in index.md")
	}
	if p.Kind == mapfile.Rule && p.Lines > MaxRulesLines {
		c.over(p.Path, p.Lines, MaxRulesLines)
	}
	if want := f.Title + " · " + p.Kind.Word(); f.Title != "" && p.Title != "" && p.Title != want {
		c.add(p.Path, p.TitleLine, "title %q: want %q, the index's title and the kind", "# "+p.Title, "# "+want)
	}
	c.sections(p)
	for _, n := range p.OutsideEntries {
		c.add(p.Path, n, "### heading outside ## Entries: every ### heading is an entry, and entries go under ## Entries")
	}
	c.headings(p)
	c.contents(p, entries)
}

// sections checks that a kind file has ## Contents, then ## Entries, and no
// other section.
func (c *checker) sections(p mapfile.Part) {
	contents, entries := false, false
	for _, h := range p.Headings {
		if !strings.HasPrefix(h.Text, "## ") {
			continue
		}
		switch name := strings.TrimSpace(h.Text[3:]); {
		case name == "Contents" && contents, name == "Entries" && entries:
			c.add(p.Path, h.No, "%q a second time: a kind file has one ## Contents, then one ## Entries", h.Text)
		case name == "Contents" && entries:
			c.add(p.Path, h.No, "## Contents after ## Entries: want ## Contents, then ## Entries")
			contents = true
		case name == "Contents":
			contents = true
		case name == "Entries":
			entries = true
		default:
			c.add(p.Path, h.No, "section %q: a kind file has only ## Contents, then ## Entries", h.Text)
		}
	}
	if !contents {
		c.add(p.Path, p.TitleLine, "missing ## Contents: want ## Contents, then ## Entries; %s", fmtHint)
	}
	if !entries {
		c.add(p.Path, p.TitleLine, "missing ## Entries: want ## Contents, then ## Entries")
	}
}

// headings checks that no two headings in a kind file, ## sections
// included, are the same or share an anchor, and that every entry heading
// can make a stable contents line.
func (c *checker) headings(p mapfile.Part) {
	type first struct {
		line int
		text string
	}
	byText, byAnchor, sections := map[string]int{}, map[string]first{}, map[string]bool{}
	for _, h := range p.Headings {
		text := strings.TrimSpace(strings.TrimLeft(h.Text, "#"))
		if strings.HasPrefix(h.Text, "### ") {
			if msg := mapfolder.HeadingProblem(text); msg != "" {
				c.add(p.Path, h.No, "%s", msg)
			}
		}
		anchor := mapfolder.Anchor(text)
		if strings.HasPrefix(h.Text, "## ") && sections[text] {
			continue // a section a second time: sections names it
		}
		sections[text] = sections[text] || strings.HasPrefix(h.Text, "## ")
		if line, dup := byText[text]; dup {
			c.add(p.Path, h.No, "heading %q is already used at line %d: no two headings in a file may be the same", text, line)
			continue
		}
		if f, dup := byAnchor[anchor]; dup {
			c.add(p.Path, h.No, "heading %q has the anchor #%s, as %q at line %d does: reword one", text, anchor, f.text, f.line)
			continue
		}
		byText[text], byAnchor[anchor] = h.No, first{h.No, text}
	}
}

// contents checks that a kind file's contents list its entries exactly:
// one line per entry, in entry order, each linking its heading's anchor.
func (c *checker) contents(p mapfile.Part, entries []*mapfile.Entry) {
	index := map[string]int{}
	for i, e := range entries {
		if _, dup := index[e.Heading]; !dup {
			index[e.Heading] = i
		}
	}
	listed := map[int]bool{}
	last := -1
	for _, l := range p.Contents {
		text, link, ok := contentsLine(l.Text)
		i, has := index[text]
		switch {
		case !ok:
			c.add(p.Path, l.No, "contents line %q: want - [<heading>](#<anchor>); %s", l.Text, fmtHint)
		case !has:
			c.add(p.Path, l.No, "contents line %q names no entry in this file (a stale heading); %s", text, fmtHint)
		case listed[i]:
			c.add(p.Path, l.No, "contents line %q is there twice; %s", text, fmtHint)
		default:
			listed[i] = true
			if want := "#" + mapfolder.Anchor(text); link != want {
				c.add(p.Path, l.No, "contents line %q links %s, want %s; %s", text, link, want, fmtHint)
			}
			if i < last {
				c.add(p.Path, l.No, "contents line %q is out of order: the contents follow the entries' order; %s", text, fmtHint)
			}
			last = max(last, i)
		}
	}
	for i, e := range entries {
		if !listed[i] && index[e.Heading] == i {
			c.add(p.Path, e.Line, "entry %q is missing from the contents; %s", e.Heading, fmtHint)
		}
	}
}

// contentsLine takes "- [text](link)" apart.
func contentsLine(s string) (text, link string, ok bool) {
	rest, ok := strings.CutPrefix(strings.TrimRight(s, " \t"), "- [")
	i := strings.Index(rest, "](")
	if !ok || i < 0 || !strings.HasSuffix(rest, ")") {
		return "", "", false
	}
	return rest[:i], rest[i+2 : len(rest)-1], true
}

func at(p string, line int) string { return fmt.Sprintf("%s:%d", p, line) }

func (c *checker) over(p string, lines, limit int) {
	c.add(p, 0, "%d lines, budget is %d: propose a merge, a prune, or a split into two capabilities", lines, limit)
}
