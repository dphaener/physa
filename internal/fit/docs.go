package fit

import (
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/store"
)

// kindRank orders docs: agent docs, contributing guides, readmes, decisions,
// then anything else; within a kind, by path.
var kindRank = map[string]int{"agent": 0, "contributing": 1, "readme": 2, "decisions": 3}

func rank(kind string) int {
	if r, ok := kindRank[kind]; ok {
		return r
	}
	return len(kindRank)
}

// fitDocs keeps docs within share by water-filling: docs that fit an even
// split are kept whole, and the rest of the share is split evenly among the
// larger ones, each cut by section. A doc whose headings alone do not fit
// its allowance is left out.
func fitDocs(gathered []store.Doc, share int) ([]Doc, SourceReport) {
	r := SourceReport{Name: "docs", Items: len(gathered)}
	docs := append([]store.Doc(nil), gathered...)
	sort.SliceStable(docs, func(i, j int) bool {
		if ri, rj := rank(docs[i].Kind), rank(docs[j].Kind); ri != rj {
			return ri < rj
		}
		return docs[i].Path < docs[j].Path
	})
	bySize := make([]int, len(docs))
	for i, d := range docs {
		bySize[i] = i
		r.Tokens += Tokens(d.Body)
	}
	sort.SliceStable(bySize, func(a, b int) bool {
		return Tokens(docs[bySize[a]].Body) < Tokens(docs[bySize[b]].Body)
	})

	allow := make([]int, len(docs))
	whole := make([]bool, len(docs))
	rem := max(share, 0)
	for k, i := range bySize {
		even := rem / (len(docs) - k)
		if n := Tokens(docs[i].Body); n <= even {
			whole[i] = true
			rem -= n
			continue
		}
		for _, j := range bySize[k:] {
			allow[j] = even
		}
		break
	}

	var kept []Doc
	for i, d := range docs {
		if whole[i] {
			kept = append(kept, Doc{Path: d.Path, Kind: d.Kind, Body: d.Body})
			r.Kept++
			r.KeptTokens += Tokens(d.Body)
			continue
		}
		body, ok := cutSections(d.Body, allow[i])
		if !ok {
			continue
		}
		kept = append(kept, Doc{Path: d.Path, Kind: d.Kind, Body: body, Cut: true})
		r.Kept++
		r.Partial++
		r.KeptTokens += Tokens(body)
	}
	return kept, r
}

// section is a heading line (empty for text before the first heading) and
// the body lines under it.
type section struct {
	heading string
	body    string
}

// sections splits a markdown doc on heading lines, # to ######, outside
// fenced code blocks.
func sections(doc string) []section {
	var out []section
	cur := section{}
	var body strings.Builder
	fence := ""
	flush := func() {
		cur.body = body.String()
		if cur.heading != "" || cur.body != "" {
			out = append(out, cur)
		}
		body.Reset()
	}
	for rest := doc; rest != ""; {
		line := rest
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line = rest[:i+1]
		}
		rest = rest[len(line):]
		bare := strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimLeft(bare, " ")
		if len(bare)-len(trimmed) <= 3 {
			if f := fenceOf(trimmed); f != "" {
				switch {
				case fence == "":
					fence = f
				case strings.HasPrefix(f, fence) && strings.TrimSpace(strings.TrimLeft(trimmed, f[:1])) == "":
					fence = ""
				}
			} else if fence == "" && isHeading(trimmed) {
				flush()
				cur = section{heading: line}
				continue
			}
		}
		body.WriteString(line)
	}
	flush()
	return out
}

// fenceOf returns the run of ``` or ~~~ that opens s, or "".
func fenceOf(s string) string {
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(s) && s[n] == c {
			n++
		}
		if n >= 3 {
			return s[:n]
		}
	}
	return ""
}

func isHeading(s string) bool {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && (n == len(s) || s[n] == ' ' || s[n] == '\t')
}

// cutSections cuts doc to tokens, keeping every heading line and trimming
// each section's body evenly, at line boundaries, by water-filling. A section
// that loses text ends with the marker. ok is false when the headings and
// markers alone do not fit.
func cutSections(doc string, tokens int) (string, bool) {
	secs := sections(doc)
	limit := 4 * tokens
	mark := markerBytes(doc)
	need := 0
	for _, s := range secs {
		need += len(s.heading)
		if s.body != "" {
			need += mark
		}
	}
	if tokens <= 0 || need > limit {
		return "", false
	}
	rem := limit
	for _, s := range secs {
		rem -= len(s.heading)
	}
	bySize := make([]int, len(secs))
	for i := range secs {
		bySize[i] = i
	}
	sort.SliceStable(bySize, func(a, b int) bool { return len(secs[bySize[a]].body) < len(secs[bySize[b]].body) })
	room := make([]int, len(secs))
	for i := range room {
		room[i] = -1 // kept whole
	}
	for k, i := range bySize {
		even := rem / (len(secs) - k)
		if b := len(secs[i].body); b <= even {
			rem -= b
			continue
		}
		for _, j := range bySize[k:] {
			room[j] = even
		}
		break
	}
	var out strings.Builder
	for i, s := range secs {
		out.WriteString(s.heading)
		if room[i] < 0 {
			out.WriteString(s.body)
			continue
		}
		cut, _ := cutLines(s.body, room[i])
		out.WriteString(cut)
	}
	return out.String(), true
}
