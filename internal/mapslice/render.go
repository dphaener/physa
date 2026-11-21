package mapslice

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteMD writes s as markdown, its sections in a fixed order: the header,
// the product, each capability, the terms, what was left out and how to
// anchor. Empty sections are omitted. The same slice always gives the same
// bytes.
func WriteMD(w io.Writer, s *Slice) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Slice: %s\n", strings.Join(s.Capabilities, ", "))
	if s.Budget == 0 {
		fmt.Fprintf(&b, "Tokens: %d, no budget.\n", s.Used)
	} else {
		fmt.Fprintf(&b, "Budget: %d of %s.\n", s.Used, plural(s.Budget, "token", "tokens"))
	}
	if s.OverBudget {
		fmt.Fprintf(&b, "Over budget: what is never cut (%s) is %s, over the %d-token budget.\n", inProse(s.Floor), plural(s.Used, "token", "tokens"), s.Budget)
	}
	if body := renderBody(s); body != "" {
		b.WriteString("\n" + body)
	}
	if len(s.LeftOut) > 0 {
		b.WriteString("\n## Left out\n\n")
		for _, c := range s.LeftOut {
			for _, part := range []string{"- `", c.Slug, "` (", c.Capability, "): ", partName(c.Part), ". Show it with `", c.Command, "`.\n"} {
				b.WriteString(part)
			}
		}
	}
	if len(s.Trailers) > 0 {
		b.WriteString("\n## How to anchor\n\nAnchor each commit to the one capability it serves, with a trailer:\n\n")
		for _, t := range s.Trailers {
			b.WriteString("- `" + t + "`\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteJSON writes s as one line of JSON with the same content as WriteMD,
// its fields in a fixed order.
func WriteJSON(w io.Writer, s *Slice) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// inProse joins items as a list in prose: "a, b and c".
func inProse(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func partName(part string) string {
	switch part {
	case PartTerm:
		return "the term"
	case PartBecause:
		return "its Because line"
	case PartRejected:
		return "its rejected options"
	}
	return "the entry"
}

func renderBody(s *Slice) string {
	var b strings.Builder
	writeBody(&b, s)
	return b.String()
}

// writeBody writes the sections the budget counts: the product, each
// capability and the terms, a blank line between sections.
func writeBody(b *strings.Builder, s *Slice) {
	sep := func() {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
	}
	if s.Product != nil {
		heading := "Product"
		if s.Product.Title != "" {
			heading += ": " + s.Product.Title
		}
		writeSection(b, heading, s.Product)
	}
	for i := range s.Sections {
		sep()
		heading := s.Sections[i].Title
		if heading == "" {
			heading = s.Sections[i].Name
		}
		writeSection(b, heading, &s.Sections[i])
	}
	if len(s.Terms) > 0 {
		sep()
		b.WriteString("## Terms\n")
		for _, t := range s.Terms {
			b.WriteString("\n**" + t.Name + "**: " + t.Definition + "\n")
			if len(t.Avoid) > 0 {
				b.WriteString("_Avoid_: " + strings.Join(t.Avoid, ", ") + "\n")
			}
		}
	}
}

var groups = []struct{ kind, heading string }{
	{"rule", "Rules"}, {"decision", "Decisions"}, {"question", "Questions"},
}

func writeSection(b *strings.Builder, heading string, s *Section) {
	line(b, "## ", heading)
	line(b, "File: ", s.File)
	line(b, "Promise: ", s.Promise)
	for _, g := range groups {
		open := false
		for _, e := range s.Entries {
			if e.Kind != g.kind {
				continue
			}
			if !open {
				b.WriteString("\n### " + g.heading + "\n")
				open = true
			}
			writeEntry(b, e)
		}
	}
}

func writeEntry(b *strings.Builder, e Entry) {
	b.WriteString("\n#### ")
	b.WriteString(e.Heading)
	b.WriteString("\n")
	if writeMeta(b, e) {
		b.WriteString("\n")
	}
	line(b, "", e.Body)
	line(b, "Because: ", e.Because)
	if len(e.Rejected) > 0 {
		b.WriteString("Rejected:\n")
		for _, r := range e.Rejected {
			line(b, "- ", r)
		}
	}
}

// line writes prefix, text and a newline, or nothing when text is empty.
func line(b *strings.Builder, prefix, text string) {
	if text == "" {
		return
	}
	b.WriteString(prefix)
	b.WriteString(text)
	b.WriteString("\n")
}

func metaLine(e Entry) string {
	var b strings.Builder
	writeMeta(&b, e)
	return b.String()
}

// writeMeta writes an entry's rebuilt meta line, without its newline: slug,
// person, date, anchors. Consecutive PR and commit anchors share a segment;
// each from-anchor has its own. It reports whether it wrote anything.
func writeMeta(b *strings.Builder, e Entry) bool {
	n := b.Len()
	seg := func(parts ...string) {
		if b.Len() > n {
			b.WriteString(" · ")
		}
		for _, p := range parts {
			b.WriteString(p)
		}
	}
	if e.Slug != "" {
		seg("`", e.Slug, "`")
	}
	if e.Person != "" {
		seg(e.Role, " by ", e.Person)
		if e.How != "" {
			b.WriteString(", ")
			b.WriteString(e.How)
		}
	}
	if e.Date != "" {
		seg(e.Date)
	}
	inToks := false
	for _, an := range e.Anchors {
		doc := strings.HasPrefix(an, "from ")
		if inToks && !doc {
			b.WriteString(" ")
			b.WriteString(an)
			continue
		}
		seg(an)
		inToks = !doc
	}
	return b.Len() > n
}

// plural returns n with its noun in singular or plural form.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
