// Package mapfolder knows where each part of a capability lives in its
// folder and writes the contents that link to its entries: index.md with the
// promise, overview and links, and one file of entries per kind. It splits a
// capability's single text into its folder and formats a map's folders,
// never changing an entry's text.
package mapfolder

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Where the map lives, relative to the repo's root.
const (
	Dir             = ".physa"
	ProductDir      = Dir + "/product"
	CapabilitiesDir = Dir + "/capabilities"
)

// The files of a capability's folder.
const (
	IndexFile     = "index.md"
	RulesFile     = "rules.md"
	DecisionsFile = "decisions.md"
	QuestionsFile = "questions.md"
)

// CapabilityDir is the folder of the capability named slug.
func CapabilityDir(slug string) string { return CapabilitiesDir + "/" + slug }

// Kind says which kind of entry an entry is, and so which file holds it.
type Kind int

// The entry kinds, in the order their files are linked from index.md.
const (
	Rule Kind = iota + 1
	Decision
	Question
)

// Kinds lists every kind in index.md's order.
var Kinds = []Kind{Rule, Decision, Question}

var kindNames = [...][3]string{{"unknown", "", ""}, {"rule", RulesFile, "Rules"}, {"decision", DecisionsFile, "Decisions"}, {"question", QuestionsFile, "Questions"}}

func (k Kind) name(i int) string {
	if k < Rule || k > Question {
		return kindNames[0][i]
	}
	return kindNames[k][i]
}

func (k Kind) String() string { return k.name(0) }

// File is the file of a folder that holds entries of this kind.
func (k Kind) File() string { return k.name(1) }

// Word is the kind's word in titles, sections and index links: "Rules",
// "Decisions" or "Questions".
func (k Kind) Word() string { return k.name(2) }

// KindOfFile says which kind a file name holds; false for any other name.
func KindOfFile(name string) (Kind, bool) { return kindOf(name, 1) }

func kindOf(s string, i int) (Kind, bool) {
	for _, k := range Kinds {
		if k.name(i) == s {
			return k, true
		}
	}
	return 0, false
}

// Anchor is the link anchor GitHub makes from a heading: lowercase, every
// character other than a letter, a digit, a space, a hyphen or an underscore
// dropped, and each space turned into a hyphen. Repeated hyphens stay.
func Anchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-', r == '_', unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Contents is a kind file's contents: one line per entry, in entry order,
// each the heading verbatim linked to its anchor.
func Contents(headings []string) []string {
	out := make([]string, 0, len(headings))
	for _, h := range headings {
		out = append(out, "- ["+h+"](#"+Anchor(h)+")")
	}
	return out
}

// HeadingProblem says why a heading cannot make a stable contents line
// (brackets, angle brackets and asterisks break it), or is empty.
func HeadingProblem(heading string) string {
	if i := strings.IndexAny(heading, "[]<>*"); i >= 0 {
		return fmt.Sprintf("heading %q contains %q, which breaks its contents line: reword it without [, ], <, > or *", heading, heading[i:i+1])
	}
	return ""
}

// IndexLink is index.md's link line to the kind's file.
func IndexLink(k Kind) string { return "- [" + k.Word() + "](" + k.File() + ")" }

// KindFile renders a kind file: its title, the contents generated from the
// entries, and each entry's text (its heading line through its last body
// line) verbatim, one blank line apart.
func KindFile(title string, kind Kind, entries []string) string {
	var es [][]line
	for _, e := range entries {
		es = append(es, plain(e))
	}
	return renderKind(kindTitle(line{text: title}, kind), es).String()
}

// Index renders index.md: the front matter block with its --- lines, the
// title, the overview paragraph and a link to each of kinds' files, in the
// order rules, decisions, questions.
func Index(frontMatter, title, overview string, kinds []Kind) string {
	return renderIndex(plain(frontMatter), line{text: "# " + title}, plain(overview), kinds).String()
}

// LineError is a spot where a text is not in its file's shape: a
// SplitError for a single text, a FormatError for a folder file.
type LineError struct {
	Line    int // 1-based line of the text
	Message string
}

func (e *LineError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

// SplitError and FormatError name LineError for Split and Format.
type SplitError = LineError
type FormatError = LineError

// lineErr makes a *LineError at the 0-based line i.
func lineErr(i int, format string, args ...any) error {
	return &LineError{Line: i + 1, Message: fmt.Sprintf(format, args...)}
}

// line is one line of a file and the line of the text it came from; 0 when
// it was generated.
type line struct {
	text string
	src  int
}

// doc is a file being built, line by line.
type doc []line

func (d doc) String() string {
	var b strings.Builder
	for _, l := range d {
		b.WriteString(l.text + "\n")
	}
	return b.String()
}

// put adds d to f as the file named name, with its lines' sources.
func (d doc) put(f Folder, name string) {
	f.Files[name], f.Source[name] = d.String(), make([]int, len(d))
	for i, l := range d {
		f.Source[name][i] = l.src
	}
}

func lines(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }

// plain is text as lines with no source and no blank lines around it.
func plain(text string) []line {
	var out []line
	for _, s := range lines(text) {
		out = append(out, line{text: s})
	}
	return trim(out)
}

// trim drops the blank lines at either end.
func trim(ls []line) []line {
	for len(ls) > 0 && blank(ls[0].text) {
		ls = ls[1:]
	}
	for len(ls) > 0 && blank(ls[len(ls)-1].text) {
		ls = ls[:len(ls)-1]
	}
	return ls
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func fence(s string) bool { return strings.HasPrefix(s, "```") }

// section says whether s is a title or section heading line.
func section(s string) bool { return strings.HasPrefix(s, "# ") || strings.HasPrefix(s, "## ") }

func kindTitle(title line, kind Kind) line {
	return line{text: "# " + title.text + " · " + kind.Word(), src: title.src}
}

// renderKind builds a kind file under its whole title line.
func renderKind(title line, entries [][]line) doc {
	d := doc{title, {}, {text: "## Contents"}, {}}
	var heads []string
	for _, e := range entries {
		heads = append(heads, strings.TrimSpace(strings.TrimPrefix(e[0].text, "### ")))
	}
	for _, c := range Contents(heads) {
		d = append(d, line{text: c})
	}
	if len(heads) > 0 {
		d = append(d, line{})
	}
	d = append(d, line{text: "## Entries"})
	for _, e := range entries {
		d = append(append(d, line{}), e...)
	}
	return d
}

// renderIndex builds index.md; title is the whole title line.
func renderIndex(fm []line, title line, overview []line, kinds []Kind) doc {
	d := append(append(doc{}, fm...), line{}, title)
	if len(overview) > 0 {
		d = append(append(d, line{}), overview...)
	}
	var links []line
	for _, k := range Kinds {
		if slices.Contains(kinds, k) {
			links = append(links, line{text: IndexLink(k)})
		}
	}
	if len(links) > 0 {
		d = append(append(d, line{}), links...)
	}
	return d
}

// frontMatter is the leading --- block's lines and the index of its closing
// line.
func frontMatter(src []string) ([]line, int, error) {
	if src[0] != "---" {
		return nil, 0, lineErr(0, "missing front matter: want --- on the first line")
	}
	for i := 1; i < len(src); i++ {
		if src[i] == "---" {
			fm := make([]line, i+1)
			for j := range fm {
				fm[j] = line{text: src[j], src: j + 1}
			}
			return fm, i, nil
		}
	}
	return nil, 0, lineErr(0, "front matter is not closed by a --- line")
}

// scanner gathers entries: each runs from its "### " heading to its last
// non-blank line, and a fenced block inside one is body.
type scanner struct {
	entries [][]line
	cur     []line
	fenced  bool
}

// take adds l to the open entry, or opens one at a "### " heading, and says
// whether it did. A title or section heading outside a fence ends the entry.
func (sc *scanner) take(l line) bool {
	switch {
	case sc.fenced:
		sc.fenced = !fence(l.text)
	case strings.HasPrefix(l.text, "### "):
		sc.close()
		sc.cur = []line{}
	case sc.cur == nil:
		return false
	case section(l.text):
		sc.close()
		return false
	default:
		sc.fenced = fence(l.text)
	}
	sc.cur = append(sc.cur, l)
	return true
}

func (sc *scanner) close() {
	if sc.cur != nil {
		sc.entries = append(sc.entries, trim(sc.cur))
	}
	sc.cur = nil
}

// flush closes the open entry and hands over every entry gathered.
func (sc *scanner) flush() [][]line {
	sc.close()
	es := sc.entries
	sc.entries = nil
	return es
}

// Folder is one capability's folder, split from its single text.
type Folder struct {
	Files  map[string]string // file name, such as "index.md", to its text
	Source map[string][]int  // file name to, per line, the line of the single text it came from; 0 when generated
}

// Split turns one capability written as a single text (front matter, a "# "
// title, an optional overview paragraph, then "## Rules", "## Decisions" and
// "## Questions" sections of "### " entries) into its folder: index.md, plus
// one kind file for each kind with entries. Entry text is kept byte for byte.
// Without an overview the promise is the overview (its Source the promise's
// line), so every index has one. A text not in that shape is a *SplitError.
func Split(text string) (Folder, error) {
	src := lines(text)
	ix, i, err := head(src, func(s string) bool { return strings.HasPrefix(s, "## ") || strings.HasPrefix(s, "### ") })
	if err != nil {
		return Folder{}, err
	}
	var (
		kind    Kind
		seen    = map[Kind]bool{}
		entries = map[Kind][][]line{}
		sc      scanner
	)
	for ; i < len(src); i++ {
		s := src[i]
		if !sc.fenced && strings.HasPrefix(s, "### ") && kind == 0 {
			return Folder{}, lineErr(i, "entry outside a Rules, Decisions or Questions section")
		}
		if sc.take(line{text: s, src: i + 1}) {
			continue
		}
		switch {
		case strings.HasPrefix(s, "## "):
			entries[kind] = append(entries[kind], sc.flush()...)
			word := strings.TrimSpace(s[3:])
			k, ok := kindOf(word, 2)
			if !ok {
				return Folder{}, lineErr(i, "unknown section %q: want Rules, Decisions or Questions", word)
			}
			if seen[k] {
				return Folder{}, lineErr(i, "section %q appears more than once", word)
			}
			seen[k], kind = true, k
		case strings.HasPrefix(s, "# "):
			return Folder{}, lineErr(i, "more than one title")
		case !blank(s):
			return Folder{}, lineErr(i, "text before the first entry of the section")
		}
	}
	entries[kind] = append(entries[kind], sc.flush()...)
	if len(ix.overview) == 0 {
		for _, l := range ix.fm {
			if key, val, ok := strings.Cut(l.text, ":"); ok && strings.TrimSpace(key) == "promise" && strings.TrimSpace(val) != "" {
				ix.overview = []line{{text: strings.TrimSpace(val), src: l.src}}
			}
		}
	}
	f := Folder{Files: map[string]string{}, Source: map[string][]int{}}
	name := line{text: strings.TrimSpace(ix.title.text[2:]), src: ix.title.src}
	for _, k := range Kinds {
		if len(entries[k]) > 0 {
			ix.kinds = append(ix.kinds, k)
			renderKind(kindTitle(name, k), entries[k]).put(f, k.File())
		}
	}
	renderIndex(ix.fm, ix.title, ix.overview, ix.kinds).put(f, IndexFile)
	return f, nil
}
