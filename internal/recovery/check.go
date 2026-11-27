package recovery

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/pathlint"
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

// answer is one answer of a session as the check read it: the capability's
// single text with its front matter, the terms, and the problems found in
// the two.
type answer struct {
	file, terms string
	problems    []mapfile.Problem
}

// checker checks one capability at a time, as `physa check map` does, in a
// scratch map of its own: product is the proposal's product promise and
// root the repo's root.
type checker struct {
	product, root string
	prExists      func(context.Context, int) (bool, error)
	openRepo      func(context.Context, string) (vcs.Repo, error)
}

// scratch writes a map holding one capability, its folder split from its
// single text file, and its terms in a fresh temp directory and returns the
// directory, which the caller removes. A file Split rejects is left out of
// the map and returned as a problem at the capability's folder, on the line
// of file at fault, for the session to put right.
func (c checker) scratch(name, file, terms string) (string, []mapfile.Problem, error) {
	dir, err := os.MkdirTemp("", "physa-check-")
	if err != nil {
		return "", nil, err
	}
	product := draft.OneLine(c.product)
	if product == "" {
		product = noPromise
	}
	var split []mapfile.Problem
	folder, err := mapfolder.Split(file)
	if e := (*mapfolder.SplitError)(nil); errors.As(err, &e) {
		split, err = []mapfile.Problem{{Path: mapfolder.CapabilityDir(name), Line: e.Line, Message: e.Message}}, nil
	} else if err == nil {
		err = folder.Write(dir, mapfolder.CapabilityDir(name))
	}
	if err == nil {
		index := mapfolder.Index("---\npromise: "+product+"\n---", "Product", product, nil)
		err = mapfolder.Folder{Files: map[string]string{mapfolder.IndexFile: index}}.Write(dir, mapfolder.ProductDir)
	}
	for _, f := range [][2]string{{".physa/README.md", readme}, {recovering.GlossaryPath, recovering.Glossary(terms)}} {
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, filepath.FromSlash(f[0])), []byte(f[1]), 0o644)
		}
	}
	if err != nil {
		return "", nil, errors.Join(err, os.RemoveAll(dir))
	}
	return dir, split, nil
}

// run returns the problems the map check finds in name's folder and its
// terms, any Split found in file, and each file path a file of the folder
// names outside its anchors (pathlint), on that file and line; a problem in
// the scratch map's own files would be Physa's bug, and is left out. The
// error is for a scratch map that could not be written or read.
func (c checker) run(ctx context.Context, name, file, terms string) ([]mapfile.Problem, error) {
	dir, out, err := c.scratch(name, file, terms)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	o := mapcheck.Options{DocRoot: c.root, PRExists: c.prExists}
	if c.openRepo != nil {
		o.OpenRepo = func(ctx context.Context, _ string) (vcs.Repo, error) { return c.openRepo(ctx, c.root) }
	}
	res, err := mapcheck.CheckWith(ctx, dir, o)
	for _, p := range res.Problems {
		if strings.HasPrefix(p.Path, mapfolder.CapabilityDir(name)+"/") || p.Path == recovering.GlossaryPath {
			out = append(out, p)
		}
	}
	if folder, serr := mapfolder.Split(file); serr == nil {
		out = append(out, pathsNamed(name, folder)...)
	}
	return out, err
}

// pathsNamed is a problem for each file path a file of name's folder f
// names outside its anchors, by file and line. The lines Split generated
// (the contents, the index's links to the folder's files) are Physa's, and
// are left out.
func pathsNamed(name string, f mapfolder.Folder) []mapfile.Problem {
	var out []mapfile.Problem
	for _, n := range slices.Sorted(maps.Keys(f.Files)) {
		for _, h := range pathlint.Find(f.Files[n]) {
			if src := f.Source[n]; h.Line <= len(src) && src[h.Line-1] > 0 {
				out = append(out, mapfile.Problem{Path: mapfolder.CapabilityDir(name) + "/" + n, Line: h.Line, Message: pathlint.Message(h.Path)})
			}
		}
	}
	return out
}

// read returns the entries of name's folder and the terms, as the map
// reader reads them, with the paths and lines Drop and DropTerms take
// problems at.
func (c checker) read(name, file, terms string) ([]mapfile.Entry, []mapfile.Term, error) {
	dir, _, err := c.scratch(name, file, terms)
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

// describe words a's problems for the session and the person.
func describe(a answer) []string { return recovering.Describe(a.file, a.terms, a.problems) }

// settle ends a capability on a, its last checked answer: every entry,
// term and overview holding a problem is dropped and listed (the promise
// takes a dropped overview's place), and what is left is checked again,
// until it checks clean (recovered) or problems remain that none of them
// holds (failed, with those problems, and nothing kept). Problems in the
// glossary outside any term drop every term: the capability is worth
// keeping without them.
func (c checker) settle(ctx context.Context, name string, a answer, out *Outcome) error {
	path := mapfolder.CapabilityDir(name)
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
		if a.problems, err = c.run(ctx, name, a.file, a.terms); err != nil {
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
				problems = append(problems, mapfile.Problem{Path: e.Path, Line: e.Line + 1,
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
		file, gone, rest := recovering.Drop(mapfolder.CapabilityDir(o.Name), o.File, problems)
		terms2, goneTerms, _ := recovering.DropTerms(o.Terms, rest)
		o.File, o.Terms = file, terms2
		o.Dropped = append(append(o.Dropped, gone...), goneTerms...)
		o.Counts = recovering.Count(o.File, o.Terms)
	}
	return nil
}
