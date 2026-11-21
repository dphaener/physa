package recovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/vcs"
)

// What the scratch map holds beside the capability: a README, which the
// check only asks to exist, and a stand-in for a product promise the
// proposal lacks, so that the scratch map's own files check clean.
const (
	readme    = "# The map\n\nA scratch copy of one recovered capability, made to check it.\n"
	noPromise = "Not yet stated."
)

// answer is one answer of a session as the check read it: the capability
// file with its front matter, the terms, and the problems found in the two.
type answer struct {
	file, terms string
	problems    []mapfile.Problem
}

// checker checks one capability file at a time, as `physa check map` does,
// in a scratch map of its own: product is the proposal's product promise and
// root the repo's root.
type checker struct {
	product, root string
	prExists      func(int) (bool, error)
	openRepo      func(string) (vcs.Repo, error)
}

// capabilityPath is where the check reports the problems of name's file.
func capabilityPath(name string) string { return ".physa/capabilities/" + name + ".md" }

// scratch writes a map holding one capability file and its terms in a fresh
// temp directory and returns the directory, which the caller removes.
func (c checker) scratch(name, file, terms string) (string, error) {
	dir, err := os.MkdirTemp("", "physa-check-")
	if err != nil {
		return "", err
	}
	product := draft.OneLine(c.product)
	if product == "" {
		product = noPromise
	}
	err = os.MkdirAll(filepath.Join(dir, ".physa", "capabilities"), 0o755)
	for _, f := range [][2]string{
		{".physa/README.md", readme},
		{".physa/product.md", "---\npromise: " + product + "\n---\n\n# Product\n"},
		{recovering.GlossaryPath, recovering.Glossary(terms)},
		{capabilityPath(name), file},
	} {
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, filepath.FromSlash(f[0])), []byte(f[1]), 0o644)
		}
	}
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// run returns the problems the map check finds in name's file and its
// terms; a problem in the scratch map's own files would be Physa's bug, and
// is left out. The error is for a scratch map that could not be written or
// read.
func (c checker) run(name, file, terms string) ([]mapfile.Problem, error) {
	dir, err := c.scratch(name, file, terms)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	o := mapcheck.Options{DocRoot: c.root, PRExists: c.prExists}
	if c.openRepo != nil {
		o.OpenRepo = func(string) (vcs.Repo, error) { return c.openRepo(c.root) }
	}
	res, err := mapcheck.CheckWith(dir, o)
	var out []mapfile.Problem
	for _, p := range res.Problems {
		if p.Path == capabilityPath(name) || p.Path == recovering.GlossaryPath {
			out = append(out, p)
		}
	}
	return out, err
}

// read returns the entries of name's file and the terms, as the map reader
// reads them, with the lines Drop and DropTerms take problems at.
func (c checker) read(name, file, terms string) ([]mapfile.Entry, []mapfile.Term, error) {
	dir, err := c.scratch(name, file, terms)
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	m, _, err := mapfile.Read(dir)
	if err != nil {
		return nil, nil, err
	}
	var entries []mapfile.Entry
	for _, f := range m.Capabilities {
		entries = append(entries, f.Entries...)
	}
	return entries, m.Glossary, nil
}

// describe words a's problems for the session and the person: each names
// the line at fault by its text, since the session never saw the front
// matter Physa adds and cannot count lines by it.
func describe(a answer) []string {
	var out []string
	for _, p := range a.problems {
		what, text := "the file", a.file
		if p.Path == recovering.GlossaryPath {
			what, text = "the glossary", recovering.Glossary(a.terms)
		}
		if lines := strings.Split(text, "\n"); p.Line > 0 && p.Line <= len(lines) {
			line := []rune(draft.OneLine(lines[p.Line-1]))
			if len(line) > 100 {
				line = append(line[:100], '…')
			}
			what += fmt.Sprintf(", at %q", string(line))
		}
		out = append(out, what+": "+p.Message)
	}
	return out
}

// settle ends a capability on a, its last checked answer: every entry and
// term holding a problem is dropped and listed, and what is left is checked
// again, until it checks clean (recovered) or problems remain that no entry
// or term holds (failed, with those problems, and nothing kept). Problems
// in the glossary outside any term drop every term: the file is worth
// keeping without them.
func (c checker) settle(name string, a answer, out *Outcome) error {
	path := capabilityPath(name)
	for len(a.problems) > 0 {
		file, entries, rest := recovering.Drop(path, a.file, a.problems)
		terms, gone, rest := recovering.DropTerms(a.terms, rest)
		var whole []mapfile.Problem
		for _, p := range rest {
			if p.Path == recovering.GlossaryPath && strings.TrimSpace(terms) != "" {
				whole = append(whole, p)
			}
		}
		if len(whole) > 0 {
			d := recovering.Dropped{Kind: recovering.KindTerm, Heading: "every term"}
			for _, p := range whole {
				d.Reasons = append(d.Reasons, p.Message)
			}
			terms, gone = "", append(gone, d)
		}
		if len(entries)+len(gone) == 0 {
			out.Status, out.Problems = StatusFailed, describe(a)
			return nil
		}
		out.Dropped = append(append(out.Dropped, entries...), gone...)
		a.file, a.terms = file, terms
		var err error
		if a.problems, err = c.run(name, a.file, a.terms); err != nil {
			return err
		}
	}
	out.Status, out.File, out.Terms = StatusRecovered, a.file, a.terms
	out.Counts = recovering.Count(a.file, a.terms)
	return nil
}

// dedupe drops, from every recovered capability after the first in run
// order, the entries whose slug and the terms whose name an earlier one
// already holds, and lists them: a slug and a term are unique in the whole
// map.
func dedupe(c checker, outcomes []Outcome) error {
	slugs, names := map[string]string{}, map[string]string{}
	for i := range outcomes {
		o := &outcomes[i]
		if o.Status != StatusRecovered {
			continue
		}
		entries, terms, err := c.read(o.Name, o.File, o.Terms)
		if err != nil {
			return err
		}
		var problems []mapfile.Problem
		for _, e := range entries {
			if first, dup := slugs[e.Slug]; dup && e.Slug != "" {
				problems = append(problems, mapfile.Problem{Path: capabilityPath(o.Name), Line: e.Line + 1,
					Message: fmt.Sprintf("slug %q is already used in the capability %q", e.Slug, first)})
			} else if e.Slug != "" {
				slugs[e.Slug] = o.Name
			}
		}
		for _, t := range terms {
			key := strings.ToLower(t.Name)
			if first, dup := names[key]; dup {
				problems = append(problems, mapfile.Problem{Path: recovering.GlossaryPath, Line: t.Line,
					Message: fmt.Sprintf("term %q is already defined in the capability %q", t.Name, first)})
			} else {
				names[key] = o.Name
			}
		}
		if len(problems) == 0 {
			continue
		}
		file, gone, rest := recovering.Drop(capabilityPath(o.Name), o.File, problems)
		terms2, goneTerms, _ := recovering.DropTerms(o.Terms, rest)
		o.File, o.Terms = file, terms2
		o.Dropped = append(append(o.Dropped, gone...), goneTerms...)
		o.Counts = recovering.Count(o.File, o.Terms)
	}
	return nil
}
