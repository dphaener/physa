// Package mapfile reads a product's map from its .physa directory: the
// product's folder, each capability's folder and the glossary. A folder is
// read whole: index.md for the promise, stewards, title and overview, then
// rules.md, decisions.md and questions.md for its entries. It turns them into
// entries (rules, decisions and questions), anchors and terms, describes each
// file it read so the checks can judge the folder's format, and reports every
// spot where the map is malformed, with its file and line. It judges form
// only: what an entry of each kind must carry, whether anchors point
// anywhere, and whether a folder's contents match its entries are questions
// for the checks built on top of it.
package mapfile

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/mapfolder"
)

// Kind says which kind an entry is; the file it sits in decides it.
type Kind = mapfolder.Kind

// The entry kinds, one per kind file of a folder.
const (
	Rule     = mapfolder.Rule
	Decision = mapfolder.Decision
	Question = mapfolder.Question
)

// AnchorKind says what an anchor points at.
type AnchorKind int

// The anchor kinds: a pull request, a commit, or a document in the repo.
const (
	PR AnchorKind = iota + 1
	Commit
	Doc
)

// Anchor links an entry to the change or document that made it real.
type Anchor struct {
	Kind  AnchorKind
	Value string // "1102", "a1b2c3d" or "docs/vision.md"
}

// Meta says which parts of an entry's meta line were present.
type Meta struct {
	Slug, Person, Date, Anchors bool
}

// Entry is one rule, decision or question.
type Entry struct {
	Kind     Kind
	Heading  string
	Path     string // the kind file it sits in, e.g. ".physa/capabilities/init/rules.md"
	Line     int    // line of the heading in that file
	Slug     string
	Role     string // "decided" or "raised", empty without a person
	Person   string
	How      string // text after the person's comma, e.g. "accepting a proposal"
	Date     string
	Anchors  []Anchor
	Body     string // prose between the meta line and Because:/Rejected:, blank lines dropped
	Because  string
	Rejected []string
	MetaHas  Meta

	rejectedLine int
	bodyDone     bool
}

// File is the product or one capability: its folder, read whole.
type File struct {
	Path         string // the folder, slash-separated, relative to the root
	Name         string // the folder's name, the capability's slug; "product" for the product
	Lines        int    // lines of index.md, as wc -l counts them
	Promise      string
	Stewards     []string
	Title        string
	Overview     string  // index.md's overview paragraph, its lines joined by "\n"
	OverviewLine int     // line of the overview's first line in index.md; 0 without one
	Entries      []Entry // rules, then decisions, then questions, each in file order
	Parts        []Part  // index.md first, then each kind file the folder has
}

// Part is one file of a folder as it was read, for the checks to judge its
// format without reading it again.
type Part struct {
	Path      string // slash-separated, relative to the root
	Kind      Kind   // 0 for index.md
	Lines     int    // as wc -l counts them
	Title     string // the text after "# ", empty without a title
	TitleLine int
	// Contents are the "- " lines under ## Contents in a kind file, or the
	// link lines of index.md, as written.
	Contents []Line
	// Headings are every heading outside a fenced block, "#" marks included.
	Headings []Line
	// OutsideEntries are the lines of "###" headings outside ## Entries; in
	// index.md, every "###" heading.
	OutsideEntries []int
	// Text is index.md's text as read, for the checks to judge its grammar;
	// empty in a kind file.
	Text string
}

// Line is one line of a file and its line number.
type Line struct {
	No   int
	Text string
}

// Term is one glossary term.
type Term struct {
	Name       string
	Definition string // text after "**Name**: "
	Avoid      []string
	Line       int
}

// Map is everything read from a product's .physa directory.
type Map struct {
	Product      *File   // nil when the product's folder is missing
	Capabilities []*File // sorted by Name
	Glossary     []Term
	HasReadme    bool
}

// Problem is one malformed spot in the map.
type Problem struct {
	Path    string // slash-separated, relative to the root
	Line    int    // 0 when the whole file is at fault
	Message string
}

func (p Problem) String() string {
	if p.Line == 0 {
		return p.Path + ": " + p.Message
	}
	return fmt.Sprintf("%s:%d: %s", p.Path, p.Line, p.Message)
}

const dir = mapfolder.Dir

// OldLayout is the message for each file of a map still in the single-file
// layout.
const OldLayout = "the map is in the old single-file format; convert it to folders (docs/architecture/09-capability-folders.md)"

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSlug says whether s is a slug: lowercase words of letters and digits
// joined by single hyphens, as a capability folder's name or an entry's slug
// must be.
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

// Read reads the map in root/.physa. The error is for I/O failure, such as a
// missing .physa directory; every fault in the format is a Problem, and
// reading goes on past each one. A missing capabilities directory means the
// map has no capabilities yet and is not a Problem; a capabilities path that
// is not a directory is one. A map in the old single-file layout gives one
// Problem per old file, saying to convert it.
func Read(root string) (*Map, []Problem, error) {
	info, err := os.Stat(path.Join(root, dir))
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, errors.New(dir + " is not a directory")
	}
	r := &reader{root: root}
	m := &Map{}
	r.product(m)
	r.capabilities(m)
	r.glossary(m)
	if _, err := os.Stat(path.Join(root, dir, "README.md")); err == nil {
		m.HasReadme = true
	} else {
		r.add(dir+"/README.md", 0, "missing file")
	}
	sort.SliceStable(r.problems, func(i, j int) bool {
		a, b := r.problems[i], r.problems[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
	return m, r.problems, nil
}

type reader struct {
	root     string
	problems []Problem
}

func (r *reader) add(p string, line int, format string, args ...any) {
	r.problems = append(r.problems, Problem{p, line, fmt.Sprintf(format, args...)})
}

func (r *reader) content(p string) ([]string, int, bool) {
	_, lines, n, ok := r.text(p)
	return lines, n, ok
}

// text reads the file at p: its text as read, its lines, and how many lines
// wc -l counts.
func (r *reader) text(p string) (string, []string, int, bool) {
	b, err := os.ReadFile(path.Join(r.root, p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.add(p, 0, "missing file")
		} else {
			r.add(p, 0, "cannot read file: %v", err)
		}
		return "", nil, 0, false
	}
	raw := string(b)
	n := strings.Count(raw, "\n")
	s := strings.TrimSuffix(raw, "\n")
	return raw, strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), n, true
}

// product reads .physa/product. An old .physa/product.md is reported as the
// old layout, and then a missing product folder is not reported again.
func (r *reader) product(m *Map) {
	old := dir + "/product.md"
	_, err := os.Stat(path.Join(r.root, old))
	isOld := err == nil
	if isOld {
		r.add(old, 0, OldLayout)
	}
	info, err := os.Stat(path.Join(r.root, mapfolder.ProductDir))
	switch {
	case errors.Is(err, os.ErrNotExist):
		if !isOld {
			r.add(mapfolder.ProductDir+"/"+mapfolder.IndexFile, 0, "missing file")
		}
	case err != nil:
		r.add(mapfolder.ProductDir, 0, "cannot read folder: %v", err)
	case !info.IsDir():
		r.add(mapfolder.ProductDir, 0, "not a folder: want the product's index.md and kind files in it")
	default:
		m.Product = r.folder(mapfolder.ProductDir, "product")
	}
}

// capabilities reads the folders in .physa/capabilities. A missing directory
// is no capabilities, not a problem: git keeps no empty directories, and a
// new codebase's map has none yet. A path that exists but is not a directory
// is a problem, as is anything in it but a folder named by a slug; a
// <name>.md file in it is the old layout.
func (r *reader) capabilities(m *Map) {
	cdir := mapfolder.CapabilitiesDir
	info, err := os.Stat(path.Join(r.root, cdir))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		r.add(cdir, 0, "cannot read directory: %v", err)
		return
	}
	if !info.IsDir() {
		r.add(cdir, 0, "not a directory: want capabilities as a directory of capability folders")
		return
	}
	ents, err := os.ReadDir(path.Join(r.root, cdir))
	if err != nil {
		r.add(cdir, 0, "cannot read directory: %v", err)
		return
	}
	for _, e := range ents {
		p := cdir + "/" + e.Name()
		switch {
		case !e.IsDir() && strings.HasSuffix(e.Name(), ".md"):
			r.add(p, 0, OldLayout)
		case !e.IsDir() || !ValidSlug(e.Name()):
			r.add(p, 0, "not a capability folder: want a folder named by the capability's slug, lowercase words joined by hyphens")
		default:
			if f := r.folder(p, e.Name()); f != nil {
				m.Capabilities = append(m.Capabilities, f)
			}
		}
	}
	sort.Slice(m.Capabilities, func(i, j int) bool { return m.Capabilities[i].Name < m.Capabilities[j].Name })
}

// folder reads the product's or a capability's folder: index.md, which must
// be there, then each kind file it has, in the order rules, decisions,
// questions. A kind with no file has no entries.
func (r *reader) folder(d, name string) *File {
	ents, err := os.ReadDir(path.Join(r.root, d))
	if err != nil {
		r.add(d, 0, "cannot read folder: %v", err)
		return nil
	}
	has := map[string]bool{}
	for _, e := range ents {
		_, kind := mapfolder.KindOfFile(e.Name())
		if e.IsDir() || (!kind && e.Name() != mapfolder.IndexFile) {
			r.add(d+"/"+e.Name(), 0, "not a file of a folder: want only index.md, rules.md, decisions.md and questions.md")
			continue
		}
		has[e.Name()] = true
	}
	f := &File{Path: d, Name: name}
	r.index(f, d+"/"+mapfolder.IndexFile)
	for _, k := range mapfolder.Kinds {
		if has[k.File()] {
			r.kindFile(f, k, d+"/"+k.File())
		}
	}
	return f
}

// index reads index.md: front matter, the title, the overview (the first
// paragraph after the title) and the link lines. Whether it keeps to
// index.md's grammar is for the checks; this records what it holds.
func (r *reader) index(f *File, p string) {
	raw, lines, n, ok := r.text(p)
	if !ok {
		return
	}
	f.Lines = n
	part := Part{Path: p, Lines: n, Text: raw}
	var overview []string
	ended := false // the overview's paragraph has ended
	for i := r.frontMatter(f, p, lines); i < len(lines); i++ {
		line, no := lines[i], i+1
		switch {
		case heading(line):
			part.Headings = append(part.Headings, Line{no, line})
			ended = ended || len(overview) > 0
			if strings.HasPrefix(line, "### ") {
				part.OutsideEntries = append(part.OutsideEntries, no)
			}
			if strings.HasPrefix(line, "# ") {
				r.title(&part, no, line)
				f.Title = part.Title
			}
		case strings.HasPrefix(line, "- "):
			part.Contents = append(part.Contents, Line{no, line})
			ended = ended || len(overview) > 0
		case strings.TrimSpace(line) == "":
			ended = ended || len(overview) > 0
		case part.Title != "" && !ended && len(part.Contents) == 0:
			if len(overview) == 0 {
				f.OverviewLine = no
			}
			overview = append(overview, strings.TrimSpace(line))
		}
	}
	f.Overview = strings.Join(overview, "\n")
	if part.Title == "" {
		r.add(p, 0, "missing title: want one line starting with \"# \"")
	}
	f.Parts = append(f.Parts, part)
}

// heading says whether line is a markdown heading of one to three levels.
func heading(line string) bool {
	return strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ")
}

func (r *reader) title(part *Part, no int, line string) {
	if part.Title != "" {
		r.add(part.Path, no, "more than one title")
		return
	}
	part.Title, part.TitleLine = strings.TrimSpace(line[2:]), no
}

// kindFile reads rules.md, decisions.md or questions.md: the title, the
// contents lines under ## Contents, and the entries. Every "###" heading
// outside a fenced block starts an entry of the file's kind.
func (r *reader) kindFile(f *File, k Kind, p string) {
	lines, n, ok := r.content(p)
	if !ok {
		return
	}
	part := Part{Path: p, Kind: k, Lines: n}
	start := 0
	if len(lines) > 0 && lines[0] == "---" {
		r.add(p, 1, "front matter belongs only in index.md")
		start = 1
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				start = i + 1
				break
			}
		}
	}
	var (
		section  string
		fenced   bool
		cur      *Entry
		inReject bool
		first    = len(f.Entries)
	)
	for i := start; i < len(lines); i++ {
		line, no := lines[i], i+1
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		switch {
		case strings.HasPrefix(line, "### "):
			part.Headings = append(part.Headings, Line{no, line})
			if section != "Entries" {
				part.OutsideEntries = append(part.OutsideEntries, no)
			}
			f.Entries = append(f.Entries, Entry{Kind: k, Heading: strings.TrimSpace(line[4:]), Path: p, Line: no})
			cur = &f.Entries[len(f.Entries)-1]
			inReject = false
			if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" && !strings.HasPrefix(lines[i+1], "#") {
				i++
				r.meta(p, i+1, lines[i], cur)
			} else {
				at := no
				if i+1 < len(lines) {
					at = no + 1
				}
				r.add(p, at, "missing meta line after heading %q", cur.Heading)
			}
		case strings.HasPrefix(line, "## "), strings.HasPrefix(line, "# "):
			cur = nil
			part.Headings = append(part.Headings, Line{no, line})
			if strings.HasPrefix(line, "# ") {
				section = ""
				r.title(&part, no, line)
			} else {
				section = strings.TrimSpace(line[3:])
			}
		case cur != nil:
			inReject = r.body(p, no, line, cur, inReject)
		case strings.TrimSpace(line) == "":
		case section == "Contents" && strings.HasPrefix(line, "- "):
			part.Contents = append(part.Contents, Line{no, line})
		case section == "Entries":
			r.add(p, no, "text before the first entry")
		default:
			r.add(p, no, "text outside the contents and the entries")
		}
	}
	if part.Title == "" {
		r.add(p, 0, "missing title: want one line starting with \"# \"")
	}
	for _, e := range f.Entries[first:] {
		if e.Rejected != nil && len(e.Rejected) == 0 {
			r.add(p, e.rejectedLine, "Rejected: has no items")
		}
	}
	f.Parts = append(f.Parts, part)
}

// frontMatter reads index.md's leading --- block into f and returns the
// index of the first line after it.
func (r *reader) frontMatter(f *File, p string, lines []string) int {
	if len(lines) == 0 || lines[0] != "---" {
		r.add(p, 1, "missing front matter: index.md must start with ---")
		return 0
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		r.add(p, 1, "front matter is not closed by a --- line")
		return 1
	}
	for i := 1; i < end; i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch {
		case !ok:
			r.add(p, i+1, "front matter line is not key: value")
		case key == "promise":
			f.Promise = val
		case key == "stewards":
			if !strings.HasPrefix(val, "[") || !strings.HasSuffix(val, "]") {
				r.add(p, i+1, "stewards must be a list like [a, b]")
				break
			}
			for _, s := range strings.Split(val[1:len(val)-1], ",") {
				if s = strings.TrimSpace(s); s != "" {
					f.Stewards = append(f.Stewards, s)
				}
			}
		default:
			r.add(p, i+1, "unknown front matter key %q: want promise or stewards", key)
		}
	}
	if f.Promise == "" {
		r.add(p, 1, "front matter needs a non-empty promise")
	}
	return end + 1
}

// body handles one line of an entry after its meta line and reports whether a
// Rejected list is open.
func (r *reader) body(p string, no int, line string, e *Entry, inReject bool) bool {
	switch {
	case strings.HasPrefix(line, "Because:"):
		e.bodyDone = true
		e.Because = strings.TrimSpace(strings.TrimPrefix(line, "Because:"))
		if e.Because == "" {
			r.add(p, no, "Because: has no text")
		}
		return false
	case strings.TrimSpace(line) == "Rejected:":
		e.bodyDone = true
		e.Rejected, e.rejectedLine = []string{}, no
		return true
	case inReject && strings.HasPrefix(line, "- "):
		e.Rejected = append(e.Rejected, strings.TrimSpace(line[2:]))
		return true
	case inReject && strings.TrimSpace(line) == "":
		return true
	case !e.bodyDone && strings.TrimSpace(line) != "":
		if e.Body != "" {
			e.Body += "\n"
		}
		e.Body += line
	}
	return false
}
