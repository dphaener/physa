// Package mapfile reads a product's map from its .physa directory: product.md,
// the capability files and the glossary. It turns them into entries (rules,
// decisions and questions), anchors and terms, and reports every spot where
// the map is malformed, with its file and line. It judges form only: what an
// entry of each kind must carry, and whether anchors point anywhere, are
// questions for the checks built on top of it.
package mapfile

import (
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Kind says which section an entry sits in.
type Kind int

// The entry kinds, one per section of a map file.
const (
	Rule Kind = iota + 1
	Decision
	Question
)

func (k Kind) String() string {
	switch k {
	case Rule:
		return "rule"
	case Decision:
		return "decision"
	case Question:
		return "question"
	}
	return "unknown"
}

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
	Line     int // line of the heading
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

// File is product.md or one capability file.
type File struct {
	Path     string // slash-separated, relative to the root
	Name     string // file stem; "product" for product.md
	Lines    int    // total lines, as wc -l counts them
	Promise  string
	Stewards []string
	Title    string
	Entries  []Entry
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
	Product      *File   // nil when product.md is missing
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

const dir = ".physa"

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSlug says whether s is a slug: lowercase words of letters and digits
// joined by single hyphens, as a capability file name or an entry's slug
// must be.
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

// Read reads the map in root/.physa. The error is for I/O failure, such as a
// missing .physa directory; every fault in the format is a Problem, and
// reading goes on past each one.
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
	if f := r.file(dir+"/product.md", "product"); f != nil {
		m.Product = f
	}
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
	b, err := os.ReadFile(path.Join(r.root, p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.add(p, 0, "missing file")
		} else {
			r.add(p, 0, "cannot read file: %v", err)
		}
		return nil, 0, false
	}
	s := string(b)
	n := strings.Count(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), n, true
}

func (r *reader) capabilities(m *Map) {
	cdir := dir + "/capabilities"
	ents, err := os.ReadDir(path.Join(r.root, cdir))
	if err != nil {
		r.add(cdir, 0, "missing directory")
		return
	}
	for _, e := range ents {
		p := cdir + "/" + e.Name()
		stem := strings.TrimSuffix(e.Name(), ".md")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || !ValidSlug(stem) {
			r.add(p, 0, "not a capability file: want <name>.md with a lowercase, hyphenated name")
			continue
		}
		if f := r.file(p, stem); f != nil {
			m.Capabilities = append(m.Capabilities, f)
		}
	}
	sort.Slice(m.Capabilities, func(i, j int) bool { return m.Capabilities[i].Name < m.Capabilities[j].Name })
}

// file reads product.md or a capability file.
func (r *reader) file(p, name string) *File {
	lines, n, ok := r.content(p)
	if !ok {
		return nil
	}
	f := &File{Path: p, Name: name, Lines: n}
	body := r.frontMatter(f, lines)
	var (
		section  Kind
		seen     = map[Kind]bool{}
		fenced   bool
		cur      *Entry
		inReject bool
		titled   bool
	)
	for i := body; i < len(lines); i++ {
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
			if section == 0 {
				r.add(p, no, "entry outside a Rules, Decisions or Questions section")
				cur = nil
				continue
			}
			f.Entries = append(f.Entries, Entry{Kind: section, Heading: strings.TrimSpace(line[4:]), Line: no})
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
		case strings.HasPrefix(line, "## "):
			cur = nil
			switch strings.TrimSpace(line[3:]) {
			case "Rules":
				section = Rule
			case "Decisions":
				section = Decision
			case "Questions":
				section = Question
			default:
				section = 0
				r.add(p, no, "unknown section %q: want Rules, Decisions or Questions", strings.TrimSpace(line[3:]))
				continue
			}
			if seen[section] {
				r.add(p, no, "section %q appears more than once", strings.TrimSpace(line[3:]))
			}
			seen[section] = true
		case strings.HasPrefix(line, "# "):
			cur, section = nil, 0
			if titled {
				r.add(p, no, "more than one title")
			}
			titled = true
			f.Title = strings.TrimSpace(line[2:])
		case cur != nil:
			inReject = r.body(p, no, line, cur, inReject)
		case section != 0 && strings.TrimSpace(line) != "":
			r.add(p, no, "text before the first entry of the section")
		}
	}
	if !titled {
		r.add(p, 0, "missing title: want one line starting with \"# \"")
	}
	for _, e := range f.Entries {
		if e.Rejected != nil && len(e.Rejected) == 0 {
			r.add(p, e.rejectedLine, "Rejected: has no items")
		}
	}
	return f
}

// frontMatter reads the leading --- block and returns the index of the first
// line after it.
func (r *reader) frontMatter(f *File, lines []string) int {
	if len(lines) == 0 || lines[0] != "---" {
		r.add(f.Path, 1, "missing front matter: the file must start with ---")
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
		r.add(f.Path, 1, "front matter is not closed by a --- line")
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
			r.add(f.Path, i+1, "front matter line is not key: value")
		case key == "promise":
			f.Promise = val
		case key == "stewards":
			if !strings.HasPrefix(val, "[") || !strings.HasSuffix(val, "]") {
				r.add(f.Path, i+1, "stewards must be a list like [a, b]")
				break
			}
			for _, s := range strings.Split(val[1:len(val)-1], ",") {
				if s = strings.TrimSpace(s); s != "" {
					f.Stewards = append(f.Stewards, s)
				}
			}
		default:
			r.add(f.Path, i+1, "unknown front matter key %q: want promise or stewards", key)
		}
	}
	if f.Promise == "" {
		r.add(f.Path, 1, "front matter needs a non-empty promise")
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
