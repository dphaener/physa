// Package mapcheck checks a product's map against its format and size
// budgets: the rules `physa check` enforces. It builds on the reader's
// problems and adds what each kind of entry must carry, that slugs and terms
// are unique, that document anchors name files in the repo, that commit
// anchors name commits in the repo's git history, and that the map stays
// within its budgets.
package mapcheck

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/mapfile"
)

// The size budgets of the map. Going over one fails the check.
const (
	MaxCapabilityLines = 150
	MaxProductLines    = 100
)

const (
	glossaryPath = ".physa/glossary.md"
)

var hows = []string{"in their words", "choosing an option", "accepting a proposal"}

// Result is what a check found.
type Result struct {
	Problems     []mapfile.Problem // sorted by path, then line, then message
	Capabilities int
	Entries      int // entries in product.md and in every capability file

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
	PRExists func(n int) (bool, error)
}

// Check reads the map under root (the repo root) and checks it. The error is
// only for a map that cannot be read at all.
func Check(root string) (Result, error) {
	return CheckWith(root, Options{})
}

// CheckWith is Check with options: it resolves #N anchors through
// o.PRExists when it is set.
func CheckWith(root string, o Options) (Result, error) {
	m, problems, err := mapfile.Read(root)
	if err != nil {
		return Result{}, err
	}
	c := &checker{root: root, problems: problems, prExists: o.PRExists, prKnown: map[int]bool{}}
	files := m.Capabilities
	if m.Product != nil {
		files = append([]*mapfile.File{m.Product}, files...)
	}
	seen := map[string]string{}
	res := Result{Capabilities: len(m.Capabilities)}
	for _, f := range files {
		res.Entries += len(f.Entries)
		for i := range f.Entries {
			c.entry(f, &f.Entries[i], seen)
		}
	}
	c.glossary(m.Glossary)
	c.budgets(m)
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
	return res, nil
}

type checker struct {
	root     string
	problems []mapfile.Problem

	prExists func(n int) (bool, error) // nil once resolving is off
	prKnown  map[int]bool              // resolved numbers
	prNote   string

	repo     *history.Repo // opened when the first commit anchor is met
	repoErr  error
	repoOpen bool
}

func (c *checker) add(p string, line int, format string, args ...any) {
	c.problems = append(c.problems, mapfile.Problem{Path: p, Line: line, Message: fmt.Sprintf(format, args...)})
}

// entry checks one entry. Problems about the meta line are reported on it,
// which sits right under the heading; the rest on the heading.
func (c *checker) entry(f *mapfile.File, e *mapfile.Entry, seen map[string]string) {
	meta := e.Line + 1
	at := func(line int, format string, args ...any) { c.add(f.Path, line, format, args...) }
	if e.MetaHas.Slug {
		where := fmt.Sprintf("%s:%d", f.Path, e.Line)
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
		c.needAnchors(f, e, meta)
		c.needBecause(f, e)
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
		c.needAnchors(f, e, meta)
		c.needBecause(f, e)
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
			c.docAnchor(f, meta, a.Value)
		case mapfile.Commit:
			c.commitAnchor(f, meta, a.Value)
		case mapfile.PR:
			c.prAnchor(f, meta, a.Value)
		}
	}
}

func (c *checker) needAnchors(f *mapfile.File, e *mapfile.Entry, meta int) {
	if len(e.Anchors) == 0 {
		c.add(f.Path, meta, "a %s needs at least one anchor", e.Kind)
	}
}

func (c *checker) needBecause(f *mapfile.File, e *mapfile.Entry) {
	if e.Because == "" {
		c.add(f.Path, e.Line, "a %s needs a `Because:` line", e.Kind)
	}
}

// docAnchor checks that a `from <path>` anchor names a regular file inside
// the repo.
func (c *checker) docAnchor(f *mapfile.File, line int, p string) {
	clean := path.Clean(p)
	if path.IsAbs(p) || filepath.IsAbs(p) || clean == ".." || strings.HasPrefix(clean, "../") {
		c.add(f.Path, line, "anchor %q must stay inside the repo", p)
		return
	}
	info, err := os.Stat(filepath.Join(c.root, filepath.FromSlash(clean)))
	if err != nil || !info.Mode().IsRegular() {
		c.add(f.Path, line, "anchor %q does not name a file in the repo", p)
	}
}

// commitAnchor checks that a commit-hash anchor names a commit in the repo.
// Git runs only when the first commit anchor is met.
func (c *checker) commitAnchor(f *mapfile.File, line int, hash string) {
	if !c.repoOpen {
		c.repoOpen = true
		c.repo, c.repoErr = history.Open(c.root)
	}
	switch {
	case errors.Is(c.repoErr, history.ErrNotRepo):
		c.add(f.Path, line, "anchor %q cannot resolve: not a git repository", hash)
	case c.repoErr != nil:
		c.add(f.Path, line, "anchor %q cannot resolve: %v", hash, c.repoErr)
	case !c.repo.HasCommit(hash):
		hint := ""
		if c.repo.Shallow() {
			hint = " (the clone is shallow; fetch its full history)"
		}
		c.add(f.Path, line, "anchor %q does not name a commit in this repo%s", hash, hint)
	}
}

// prAnchor checks that a #N anchor names a PR, when a resolver is set. Each
// number is resolved once; the first resolver error turns resolving off.
func (c *checker) prAnchor(f *mapfile.File, line int, value string) {
	n, err := strconv.Atoi(value)
	if c.prExists == nil || err != nil {
		return // form only; the reader has checked the form
	}
	exists, seen := c.prKnown[n]
	if !seen {
		exists, err = c.prExists(n)
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
		c.add(f.Path, line, "anchor #%d names no PR in this repo", n)
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

func (c *checker) budgets(m *mapfile.Map) {
	if m.Product != nil && m.Product.Lines > MaxProductLines {
		c.over(m.Product.Path, m.Product.Lines, MaxProductLines)
	}
	for _, f := range m.Capabilities {
		if f.Lines > MaxCapabilityLines {
			c.over(f.Path, f.Lines, MaxCapabilityLines)
		}
	}
}

func (c *checker) over(p string, lines, budget int) {
	c.add(p, 0, "%d lines, budget is %d: propose a merge or a prune", lines, budget)
}
