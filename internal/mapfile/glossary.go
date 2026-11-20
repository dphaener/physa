package mapfile

import (
	"regexp"
	"strings"
)

var (
	termRe    = regexp.MustCompile(`^\*\*([^*]+)\*\*: (\S.*)$`)
	emptyTerm = regexp.MustCompile(`^\*\*[^*]+\*\*:\s*$`)
)

// glossary reads glossary.md: headings and prose are free; a term is a line
// **Name**: definition, optionally followed by an _Avoid_: list.
func (r *reader) glossary(m *Map) {
	p := dir + "/glossary.md"
	lines, _, ok := r.content(p)
	if !ok {
		return
	}
	var cur *Term
	for i, line := range lines {
		no := i + 1
		switch {
		case strings.TrimSpace(line) == "", strings.HasPrefix(line, "#"):
			cur = nil
		case emptyTerm.MatchString(line):
			r.add(p, no, "term has no definition")
			cur = nil
		case termRe.MatchString(line):
			t := termRe.FindStringSubmatch(line)
			m.Glossary = append(m.Glossary, Term{Name: strings.TrimSpace(t[1]), Line: no})
			cur = &m.Glossary[len(m.Glossary)-1]
		case strings.HasPrefix(line, "_Avoid_:"):
			if cur == nil {
				r.add(p, no, "_Avoid_ must directly follow a term")
				continue
			}
			var words []string
			for _, w := range strings.Split(strings.TrimPrefix(line, "_Avoid_:"), ",") {
				if w = strings.TrimSpace(w); w != "" {
					words = append(words, w)
				}
			}
			if len(words) == 0 {
				r.add(p, no, "_Avoid_ lists no words")
			}
			cur.Avoid = append(cur.Avoid, words...)
		}
	}
}
