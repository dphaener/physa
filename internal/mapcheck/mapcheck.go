// Package mapcheck checks a product's map against its format and size
// limits: the rules `physa check map` enforces. It builds on the reader's
// problems and adds what each kind of entry must carry, that every folder
// keeps the folder format (index.md's grammar and links, each kind file's
// title, sections, headings, and contents that match its entries), that
// slugs and terms are unique, that document anchors name files in the repo,
// that commit anchors name changes in the repo's history, read through
// version control, and that each file stays within its limit.
package mapcheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/vcs"
)

const (
	glossaryPath = ".physa/glossary.md"
)

// errNoVCS is why commit anchors are unresolved when no OpenRepo is set.
var errNoVCS = errors.New("no version control")

var hows = []string{"in their words", "choosing an option", "accepting a proposal"}

// Result is what a check found.
type Result struct {
	Problems     []mapfile.Problem // sorted by path, then line, then message
	Capabilities int
	Entries      int // entries in the product and in every capability

	// PRAnchors says how #N anchors were checked: PRAnchorsGitHub when every
	// one was resolved, PRAnchorsFormOnly when only their form was.
	PRAnchors string
	// PRNote is why PR anchors were checked for form only after the resolver
	// failed; empty otherwise (the caller explains a missing resolver).
	PRNote string
}

// The values of Result.PRAnchors.
const (
	PRAnchorsGitHub   = "github"
	PRAnchorsFormOnly = "form-only"
)

// Options carries what the check cannot learn from the repo's files.
type Options struct {
	// PRExists tells whether a PR with this number exists. Nil checks #N
	// anchors for form only. An error stops resolving for the rest of the
	// run: PR anchors are then checked for form only.
	PRExists func(ctx context.Context, n int) (bool, error)
	// OpenRepo opens the version control holding root. It is called once,
	// when the first commit anchor is met. Nil leaves commit anchors
	// unresolved: each is a problem.
	OpenRepo func(ctx context.Context, root string) (vcs.Repo, error)
	// DocRoot is where `from <path>` anchors resolve; empty means root. A
	// scratch copy of the map uses it to check against the real repo.
	DocRoot string
	// FormOnly checks every anchor for its form alone: a commit anchor is
	// not resolved, so it is no problem, and a #N anchor is not resolved
	// whatever PRExists says. History and the forge are never read. Every
	// other check runs as it does without the option: format, contents,
	// slugs, terms, size limits, and `from <path>` anchors naming a file.
	FormOnly bool
}

// Check reads the map under root (the repo root) and checks it, with no
// resolver for #N anchors and no version control for commit anchors. The
// error is only for a map that cannot be read at all.
func Check(ctx context.Context, root string) (Result, error) {
	return CheckWith(ctx, root, Options{})
}

// CheckWith is Check with options: it resolves #N anchors through
// o.PRExists and commit anchors through o.OpenRepo, when each is set.
func CheckWith(ctx context.Context, root string, o Options) (Result, error) {
	_, res, err := CheckRead(ctx, root, o)
	return res, err
}

// CheckRead is CheckWith that also returns the map it read, so a caller
// that needs the map reads it once. The map is nil only with an error.
func CheckRead(ctx context.Context, root string, o Options) (*mapfile.Map, Result, error) {
	m, problems, err := mapfile.Read(root)
	if err != nil {
		return nil, Result{}, err
	}
	docRoot := o.DocRoot
	if docRoot == "" {
		docRoot = root
	}
	c := &checker{root: root, docRoot: docRoot, problems: problems, prExists: o.PRExists, prKnown: map[int]bool{}, openRepo: o.OpenRepo, formOnly: o.FormOnly}
	if o.FormOnly {
		c.prExists = nil
	}
	files := m.Capabilities
	if m.Product != nil {
		files = append([]*mapfile.File{m.Product}, files...)
	}
	known := map[string]bool{}
	for _, p := range problems {
		known[at(p.Path, p.Line)] = true
	}
	seen := map[string]string{}
	res := Result{Capabilities: len(m.Capabilities)}
	for _, f := range files {
		res.Entries += len(f.Entries)
		for i := range f.Entries {
			c.entry(ctx, &f.Entries[i], seen)
		}
		c.folder(f, known)
	}
	c.glossary(m.Glossary)
	slices.SortStableFunc(c.problems, func(a, b mapfile.Problem) int {
		if x := strings.Compare(a.Path, b.Path); x != 0 {
			return x
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Message, b.Message)
	})
	res.Problems = c.problems
	res.PRAnchors, res.PRNote = PRAnchorsFormOnly, c.prNote
	if c.prExists != nil && c.prNote == "" {
		res.PRAnchors = PRAnchorsGitHub
	}
	return m, res, nil
}

type checker struct {
	root     string
	formOnly bool   // commit anchors are not resolved
	docRoot  string // where doc anchors resolve
	problems []mapfile.Problem

	prExists func(ctx context.Context, n int) (bool, error) // nil once resolving is off
	prKnown  map[int]bool                                   // resolved numbers
	prNote   string

	openRepo func(ctx context.Context, root string) (vcs.Repo, error)
	repo     vcs.Repo // opened when the first commit anchor is met
	repoErr  error
	repoOpen bool
}

func (c *checker) add(p string, line int, format string, args ...any) {
	c.problems = append(c.problems, mapfile.Problem{Path: p, Line: line, Message: fmt.Sprintf(format, args...)})
}

// entry checks one entry. Problems are reported in the entry's own file:
// those about the meta line on it, which sits right under the heading; the
// rest on the heading.
func (c *checker) entry(ctx context.Context, e *mapfile.Entry, seen map[string]string) {
	meta := e.Line + 1
	at := func(line int, format string, args ...any) { c.add(e.Path, line, format, args...) }
	if e.MetaHas.Slug {
		where := fmt.Sprintf("%s:%d", e.Path, e.Line)
		if first, dup := seen[e.Slug]; dup {
			at(meta, "slug %q is already used at %s", e.Slug, first)
		} else {
			seen[e.Slug] = where
		}
	}
	if e.Role == "raised" && e.Kind != mapfile.Question {
		at(meta, "a %s is not raised by anyone: raised by belongs on questions", e.Kind)
	}
	switch e.Kind {
	case mapfile.Rule:
		if e.MetaHas.Person {
			at(meta, "a rule carries no person: people and dates are only on decisions and questions")
		}
		if e.MetaHas.Date {
			at(meta, "a rule carries no date: people and dates are only on decisions and questions")
		}
		c.needAnchors(e, meta)
		c.needBecause(e)
	case mapfile.Decision:
		switch {
		case e.Role != "decided" && !e.MetaHas.Person:
			at(meta, "a decision needs `decided by <person>, <how>`")
		case e.Role == "decided" && e.Person == "":
			at(meta, "a decision needs `decided by <person>, <how>`")
		case e.Role == "decided" && !slices.Contains(hows, e.How):
			at(meta, "decided by %q needs how: one of %s", e.Person, strings.Join(hows, ", "))
		}
		if !e.MetaHas.Date {
			at(meta, "a decision needs a date")
		}
		c.needAnchors(e, meta)
		c.needBecause(e)
	case mapfile.Question:
		if e.Role == "decided" {
			at(meta, "a question is not decided: decided by belongs on decisions")
		} else if e.Role != "raised" {
			at(meta, "a question needs `raised by <person>`")
		}
		if !e.MetaHas.Date {
			at(meta, "a question needs a date")
		}
	}
	for _, a := range e.Anchors {
		switch a.Kind {
		case mapfile.Doc:
			c.docAnchor(e.Path, meta, a.Value)
		case mapfile.Commit:
			c.commitAnchor(ctx, e.Path, meta, a.Value)
		case mapfile.PR:
			c.prAnchor(ctx, e.Path, meta, a.Value)
		}
	}
}

func (c *checker) needAnchors(e *mapfile.Entry, meta int) {
	if len(e.Anchors) == 0 {
		c.add(e.Path, meta, "a %s needs at least one anchor", e.Kind)
	}
}

func (c *checker) needBecause(e *mapfile.Entry) {
	if e.Because == "" {
		c.add(e.Path, e.Line, "a %s needs a `Because:` line", e.Kind)
	}
}

// docAnchor checks that a `from <path>` anchor names a regular file inside
// the doc root (the repo unless Options.DocRoot is set).
func (c *checker) docAnchor(file string, line int, p string) {
	clean := path.Clean(p)
	if path.IsAbs(p) || filepath.IsAbs(p) || clean == ".." || strings.HasPrefix(clean, "../") {
		c.add(file, line, "anchor %q must stay inside the repo", p)
		return
	}
	info, err := os.Stat(filepath.Join(c.docRoot, filepath.FromSlash(clean)))
	if err != nil || !info.Mode().IsRegular() {
		c.add(file, line, "anchor %q does not name a file in the repo", p)
	}
}

// commitAnchor checks that a commit-hash anchor names a change in the repo.
// Version control is opened only when the first commit anchor is met.
func (c *checker) commitAnchor(ctx context.Context, file string, line int, hash string) {
	if c.formOnly {
		return // the reader has checked the form
	}
	if !c.repoOpen {
		c.repoOpen = true
		if c.openRepo == nil {
			c.repoErr = errNoVCS
		} else {
			c.repo, c.repoErr = c.openRepo(ctx, c.root)
		}
	}
	switch {
	case c.repoErr != nil:
		// vcs.ErrNotRepo reads "not a repository under version control".
		c.add(file, line, "anchor %q cannot resolve: %v", hash, c.repoErr)
	case !c.repo.HasChange(ctx, hash):
		hint := ""
		if c.repo.Partial(ctx) {
			hint = " (the clone is shallow; fetch its full history)"
		}
		c.add(file, line, "anchor %q does not name a commit in this repo%s", hash, hint)
	}
}

// prAnchor checks that a #N anchor names a PR, when a resolver is set. Each
// number is resolved once; the first resolver error turns resolving off.
func (c *checker) prAnchor(ctx context.Context, file string, line int, value string) {
	n, err := strconv.Atoi(value)
	if c.prExists == nil || err != nil {
		return // form only; the reader has checked the form
	}
	exists, seen := c.prKnown[n]
	if !seen {
		exists, err = c.prExists(ctx, n)
		if err != nil {
			c.prNote = err.Error()
			if c.prNote == "" {
				c.prNote = "the resolver failed"
			}
			c.prExists = nil
			return
		}
		c.prKnown[n] = exists
	}
	if !exists {
		c.add(file, line, "anchor #%d names no PR in this repo", n)
	}
}

func (c *checker) glossary(terms []mapfile.Term) {
	first := map[string]int{}
	for _, t := range terms {
		key := strings.ToLower(t.Name)
		if line, dup := first[key]; dup {
			c.add(glossaryPath, t.Line, "term %q is already defined at line %d", t.Name, line)
			continue
		}
		first[key] = t.Line
	}
}
