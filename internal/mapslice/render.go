package mapslice

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteMD writes s as markdown, its sections in a fixed order: the header,
// the product, each capability, the terms, Reading further (the slice skill)
// and how to anchor, a blank line between sections. Empty sections are
// omitted. The same slice always gives the same bytes.
func WriteMD(w io.Writer, s *Slice) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Slice: %s\n", strings.Join(s.Capabilities, ", "))
	if s.Product != nil {
		heading := "Product"
		if s.Product.Title != "" {
			heading += ": " + s.Product.Title
		}
		b.WriteString("\n")
		writeSection(&b, heading, s.Product)
	}
	for i := range s.Sections {
		heading := s.Sections[i].Title
		if heading == "" {
			heading = s.Sections[i].Name
		}
		b.WriteString("\n")
		writeSection(&b, heading, &s.Sections[i])
	}
	if len(s.Terms) > 0 {
		b.WriteString("\n## Terms\n")
		for _, t := range s.Terms {
			b.WriteString("\n**" + t.Name + "**: " + t.Definition + "\n")
			if len(t.Avoid) > 0 {
				b.WriteString("_Avoid_: " + strings.Join(t.Avoid, ", ") + "\n")
			}
		}
	}
	if s.ReadingFurther != "" {
		b.WriteString("\n## Reading further\n\n" + strings.TrimRight(s.ReadingFurther, "\n") + "\n")
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
	return writeJSON(w, s)
}

func writeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

var groups = []struct{ kind, heading string }{
	{"rule", "Rules"}, {"decision", "Decisions"}, {"question", "Questions"},
}

// writeSection writes the product or a capability: its file, promise and
// overview, the kinds it holds in full, then a group of heading lines for
// each kind shown by heading only, with how to take them.
func writeSection(b *strings.Builder, heading string, s *Section) {
	line(b, "## ", heading)
	line(b, "File: ", s.File)
	line(b, "Promise: ", s.Promise)
	if s.Overview != "" {
		b.WriteString("\n" + s.Overview + "\n")
	}
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
			b.WriteString("\n")
			writeEntry(b, "####", e)
		}
	}
	for _, g := range groups[1:] {
		open := false
		for _, h := range s.Contents {
			if h.Kind != g.kind {
				continue
			}
			if !open {
				b.WriteString("\n" + g.heading + " (take with physa map slice --entry <slug>...)\n")
				open = true
			}
			b.WriteString("- " + h.Slug + ": " + h.Heading + "\n")
		}
	}
}

// writeEntry writes e as the map writes it, under a heading of the given
// marks: the heading, the rebuilt meta line, the body, Because and Rejected.
func writeEntry(b *strings.Builder, marks string, e Entry) {
	b.WriteString(marks + " " + e.Heading + "\n")
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
// person, date, anchors. Adjacent PR and commit anchors share a segment;
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
