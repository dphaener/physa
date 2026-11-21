package recovering

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapfile"
)

// GlossaryPath is where the map check reports a glossary's problems.
const GlossaryPath = ".physa/glossary.md"

// glossaryHead is what Glossary writes above the terms: two lines.
const glossaryHead = "# Glossary\n\n"

var (
	slugAt    = regexp.MustCompile("^`([^`]*)`")
	termStart = regexp.MustCompile(`^\*\*([^*]+)\*\*:`)
)

// File assembles the capability file: front matter holding promise, on one
// line, then body, the file the session wrote, with any front matter of its
// own removed. A body with no title gets one made from name, so a missing
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

// entries finds the entries of a capability file as the map reader reads
// them: after the front matter, outside fences, each from its "### "
// heading to the next heading of the same or a higher level.
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
		out     []span
		section string
		fence   bool
	)
	end := func(no int) {
		if n := len(out); n > 0 && out[n-1].to == 0 {
			out[n-1].to = no
		}
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
			out = append(out, s)
		case strings.HasPrefix(l, "## "):
			end(no)
			section = map[string]string{"Rules": "rule", "Decisions": "decision", "Questions": "question"}[strings.TrimSpace(l[3:])]
		case strings.HasPrefix(l, "# "):
			end(no)
			section = ""
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

// cut takes out of text every span a problem at path falls in, and returns
// what is left, what was taken with its reasons, and the problems that fell
// in no span or were not at path.
func cut(path, text string, spans []span, problems []mapfile.Problem) (string, []Dropped, []mapfile.Problem) {
	reasons := make([][]string, len(spans))
	var rest []mapfile.Problem
	for _, p := range problems {
		hit := -1
		if p.Path == path && p.Line > 0 {
			for i, s := range spans {
				if p.Line >= s.from && p.Line < s.to {
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

// Drop takes out of file, the capability file the map check read at path,
// every whole entry holding a problem on one of its lines, from its heading
// to the next heading of the same or a higher level. It returns the file
// that is left, the entries taken with their reasons, in the file's order,
// and the rest: problems on no entry's lines (the title, a section heading,
// the front matter, the whole file) and problems at other paths.
func Drop(path, file string, problems []mapfile.Problem) (kept string, dropped []Dropped, rest []mapfile.Problem) {
	return cut(path, file, entries(file), problems)
}

// DropTerms takes out of terms every term the map check found a problem in,
// reading problems at GlossaryPath by the lines of Glossary(terms). It
// returns the terms that are left, the terms taken with their reasons, and
// the rest, as Drop does.
func DropTerms(terms string, problems []mapfile.Problem) (kept string, dropped []Dropped, rest []mapfile.Problem) {
	g := Glossary(terms)
	left, dropped, rest := cut(GlossaryPath, g, termSpans(g), problems)
	kept = strings.TrimSpace(strings.TrimPrefix(left, glossaryHead))
	if kept != "" {
		kept += "\n"
	}
	return kept, dropped, rest
}

// Count says how many rules, decisions and questions file holds, and how
// many terms glossaryTerms holds.
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
