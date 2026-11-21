package recovering

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
)

// GlossaryPath is where the map check reports a glossary's problems.
const GlossaryPath = ".physa/glossary.md"

// glossaryHead is what Glossary writes above the terms: two lines.
const glossaryHead = "# Glossary\n\n"

var (
	slugAt    = regexp.MustCompile("^`([^`]*)`")
	termStart = regexp.MustCompile(`^\*\*([^*]+)\*\*:`)
)

// File assembles the capability's single text, which mapfolder.Split makes
// its folder of: front matter holding promise, on one line, then body, the
// text the session wrote, with any front matter of its own removed. A body with no title gets one made from name, so a missing
// title line does not fail the whole file.
func File(name, promise, body string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n")), "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				lines = lines[i+1:]
				break
			}
		}
	}
	body = strings.TrimSpace(strings.Join(lines, "\n"))
	titled, fence := false, false
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "```") {
			fence = !fence
		} else if !fence && strings.HasPrefix(l, "# ") {
			titled = true
		}
	}
	if !titled {
		body = strings.TrimSpace("# " + title(name) + "\n\n" + body)
	}
	return "---\npromise: " + draft.OneLine(promise) + "\n---\n\n" + body + "\n"
}

// title makes a capability's name a title: "team-invites" is "Team invites".
func title(name string) string {
	s := strings.ReplaceAll(name, "-", " ")
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// Glossary is the glossary.md the map check reads for terms: a title, then
// the terms. DropTerms reads problems by its lines.
func Glossary(terms string) string {
	return glossaryHead + strings.TrimSpace(strings.ReplaceAll(terms, "\r\n", "\n")) + "\n"
}

// span is one entry or term: its kind, slug and heading, and its lines,
// from the first to before the last, counted from 1.
type span struct {
	kind, slug, heading string
	from, to            int
}

// lines splits text as the map reader does: by line, without the last line
// end.
func lines(text string) []string {
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
}

// entries finds the entries of a capability's single text as Split reads
// them: after the front matter, outside fences, each from its "### "
// heading to the next heading of the same or a higher level. The overview,
// the text between the title and the first heading below it, is a span of
// kind KindOverview.
func entries(file string) []span {
	ls := lines(file)
	start := 0
	if len(ls) > 0 && ls[0] == "---" {
		start = 1
		for i := 1; i < len(ls); i++ {
			if ls[i] == "---" {
				start = i + 1
				break
			}
		}
	}
	var (
		out            []span
		section        string
		fence, titled  bool
		overview, open bool
	)
	end := func(no int) {
		if n := len(out); n > 0 && out[n-1].to == 0 {
			out[n-1].to = no
		}
		open = false
	}
	for i := start; i < len(ls); i++ {
		l, no := ls[i], i+1
		switch {
		case strings.HasPrefix(l, "```"):
			fence = !fence
		case fence:
		case strings.HasPrefix(l, "### "):
			end(no)
			s := span{kind: section, heading: strings.TrimSpace(l[4:]), from: no}
			if s.kind == "" {
				s.kind = "entry"
			}
			if i+1 < len(ls) {
				if m := slugAt.FindStringSubmatch(ls[i+1]); m != nil {
					s.slug = m[1]
				}
			}
			out, open, titled = append(out, s), true, false
		case strings.HasPrefix(l, "## "):
			end(no)
			titled = false
			section = map[string]string{"Rules": "rule", "Decisions": "decision", "Questions": "question"}[strings.TrimSpace(l[3:])]
		case strings.HasPrefix(l, "# "):
			end(no)
			section, titled = "", true
		case titled && !open && !overview && strings.TrimSpace(l) != "":
			out = append(out, span{kind: KindOverview, heading: "the overview", from: no})
			open, overview = true, true
		}
	}
	end(len(ls) + 1)
	return out
}

// termSpans finds the terms of a glossary as the map reader reads them: a
// "**Name**:" line and the _Avoid_ lines right after it.
func termSpans(glossary string) []span {
	ls := lines(glossary)
	var out []span
	for i := 0; i < len(ls); i++ {
		m := termStart.FindStringSubmatch(ls[i])
		if m == nil {
			continue
		}
		s := span{kind: KindTerm, heading: strings.TrimSpace(m[1]), from: i + 1}
		for i+1 < len(ls) && strings.HasPrefix(ls[i+1], "_Avoid_:") {
			i++
		}
		s.to = i + 2
		out = append(out, s)
	}
	return out
}

// cut takes out of text every span a problem falls in, by the line of text
// at says it is on (0 for none), and returns what is left, what was taken
// with its reasons, and the problems that fell in no span.
func cut(text string, spans []span, problems []mapfile.Problem, at func(mapfile.Problem) int) (string, []Dropped, []mapfile.Problem) {
	reasons := make([][]string, len(spans))
	var rest []mapfile.Problem
	for _, p := range problems {
		hit := -1
		if no := at(p); no > 0 {
			for i, s := range spans {
				if no >= s.from && no < s.to {
					hit = i
					break
				}
			}
		}
		if hit < 0 {
			rest = append(rest, p)
			continue
		}
		reasons[hit] = append(reasons[hit], p.Message)
	}
	ls := lines(text)
	gone := make([]bool, len(ls))
	var dropped []Dropped
	for i, s := range spans {
		if reasons[i] == nil {
			continue
		}
		dropped = append(dropped, Dropped{Kind: s.kind, Slug: s.slug, Heading: s.heading, Reasons: reasons[i]})
		for no := s.from; no < s.to && no <= len(ls); no++ {
			gone[no-1] = true
		}
	}
	if dropped == nil {
		return text, nil, rest
	}
	var kept []string
	for i, l := range ls {
		if !gone[i] {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n") + "\n", dropped, rest
}

// Drop takes out of file, a capability's single text, every whole entry
// holding a problem on one of its lines, from its heading to the next
// heading of the same or a higher level, and the overview when a problem
// holds it. dir is the capability's folder the map check read, the folder
// mapfolder.Split makes of file: a problem at a file in dir is read at the
// line of file it came from, and one at dir itself (Split's own) at its
// line of file. A problem on the whole of index.md, its length, is the
// overview's: Split then takes the promise in its place. Drop returns the
// text that is left, the parts taken with their reasons, in the text's
// order, and the rest: problems on no entry's lines (the title, a section
// heading, the front matter, a whole kind file) and problems outside dir.
func Drop(dir, file string, problems []mapfile.Problem) (kept string, dropped []Dropped, rest []mapfile.Problem) {
	f, _ := mapfolder.Split(file) // a text Split rejects has only problems at dir itself
	spans := entries(file)
	overview := 0
	for _, s := range spans {
		if s.kind == KindOverview {
			overview = s.from
		}
	}
	return cut(file, spans, problems, func(p mapfile.Problem) int {
		if p.Path == dir {
			return p.Line
		}
		name, ok := strings.CutPrefix(p.Path, dir+"/")
		switch {
		case !ok:
			return 0
		case name == mapfolder.IndexFile && p.Line == 0:
			return overview
		}
		return f.Origin(name, p.Line)
	})
}

// DropTerms takes out of terms every term the map check found a problem in,
// reading problems at GlossaryPath by the lines of Glossary(terms). It
// returns the terms that are left, the terms taken with their reasons, and
// the rest, as Drop does.
func DropTerms(terms string, problems []mapfile.Problem) (kept string, dropped []Dropped, rest []mapfile.Problem) {
	g := Glossary(terms)
	left, dropped, rest := cut(g, termSpans(g), problems, func(p mapfile.Problem) int {
		if p.Path != GlossaryPath {
			return 0
		}
		return p.Line
	})
	kept = strings.TrimSpace(strings.TrimPrefix(left, glossaryHead))
	if kept != "" {
		kept += "\n"
	}
	return kept, dropped, rest
}

// Count says how many rules, decisions and questions file, a capability's
// single text, holds, and how many terms glossaryTerms holds.
func Count(file, glossaryTerms string) Counts {
	var c Counts
	for _, s := range entries(file) {
		switch s.kind {
		case "rule":
			c.Rules++
		case "decision":
			c.Decisions++
		case "question":
			c.Questions++
		}
	}
	c.Terms = len(termSpans(glossaryTerms))
	return c
}

// Describe words problems, found in the folder split from file, a
// capability's single text, and in the glossary of terms, for the session
// and the person: each names the part at fault and quotes its line, since
// the session wrote one text, never saw the front matter Physa adds or the
// folder it splits it into, and cannot count lines by them.
func Describe(file, terms string, problems []mapfile.Problem) []string {
	folder, _ := mapfolder.Split(file)
	var out []string
	for _, p := range problems {
		what, text := "the file", file
		if p.Path == GlossaryPath {
			what, text = "the glossary", Glossary(terms)
		} else if _, name, ok := strings.Cut(strings.TrimPrefix(p.Path, mapfolder.CapabilitiesDir+"/"), "/"); ok {
			what, text = "the title and overview", folder.Files[name]
			if k, ok := mapfolder.KindOfFile(name); ok {
				what = "the " + strings.ToLower(k.Word())
			}
		}
		if ls := strings.Split(text, "\n"); p.Line > 0 && p.Line <= len(ls) {
			line := []rune(draft.OneLine(ls[p.Line-1]))
			if len(line) > 100 {
				line = append(line[:100], '…')
			}
			what += fmt.Sprintf(", at %q", string(line))
		}
		out = append(out, what+": "+p.Message)
	}
	return out
}
