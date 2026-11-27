package walk

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/sourceset"
)

// Area is what one walk session is given: an area's name and promise, the
// area it was split from when it is a part, and its source files.
type Area struct {
	Name, Promise, Parent string
	Files                 []string
}

// Footprint is a capability with the source files it owns and the tests and
// docs that bear on it, every folder expanded to files and sorted.
type Footprint struct {
	Name, Promise        string
	Sources, Tests, Docs []string
}

// Footprints are an area's capabilities, and its shared files with reasons.
type Footprints struct {
	Capabilities []Footprint
	Shared       []Shared
}

// Problems are what a check found wrong with an answer. Each list is empty
// when it found nothing of that kind.
type Problems struct {
	// Split is set when the answer checked was a split.
	Split bool
	// Unclaimed are source files owned by none and not shared.
	Unclaimed []string
	// Twice maps a source file to the owners that both claim it.
	Twice map[string][]string
	// Outside are "owner: path" for source or shared paths matching no file
	// of the area.
	Outside []string
	// NotTest and NotDoc are "capability: path" for tests and docs paths
	// matching no file the split named a test or a doc.
	NotTest, NotDoc []string
	// NoSource are the capabilities (or parts) owning no source file.
	NoSource []string
	// Names are the other faults: a repeated, malformed or empty name, an
	// empty promise, too few parts.
	Names []string
}

// Empty reports whether nothing was found.
func (p Problems) Empty() bool {
	return len(p.Unclaimed)+len(p.Twice)+len(p.Outside)+len(p.NotTest)+len(p.NotDoc)+len(p.NoSource)+len(p.Names) == 0
}

var kebabName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// owner is something that claims files: a capability, a part or a shared
// entry, with the patterns it claims.
type owner struct {
	label    string
	patterns []string
}

// resolve claims the area's files for owners, and reports what no owner
// claims, what two claim and the patterns that match no file of the area.
func resolve(a Area, owners []owner, p *Problems) map[string][]string {
	cs := make([]sourceset.Claim, len(owners))
	for i, o := range owners {
		cs[i] = sourceset.Claim{Owner: fmt.Sprint(i), Patterns: o.patterns}
		for _, pat := range o.patterns {
			if !slices.ContainsFunc(a.Files, func(f string) bool { return sourceset.Match(pat, f) }) {
				p.Outside = append(p.Outside, o.label+": "+draft.OneLine(pat))
			}
		}
	}
	where, missing, twice := sourceset.Claims(a.Files, cs)
	p.Unclaimed = missing
	for f, ids := range twice {
		if p.Twice == nil {
			p.Twice = map[string][]string{}
		}
		for _, id := range ids {
			var i int
			fmt.Sscan(id, &i)
			p.Twice[f] = append(p.Twice[f], owners[i].label)
		}
	}
	owned := map[string][]string{}
	for f, id := range where {
		owned[id] = append(owned[id], f)
	}
	for _, fs := range owned {
		sort.Strings(fs)
	}
	return owned
}

// named adds to p a fault for each bad or repeated name and each empty promise.
func named(p *Problems, kind string, names, promises []string) {
	seen := map[string]bool{}
	for i, n := range names {
		switch {
		case !kebabName.MatchString(n):
			p.Names = append(p.Names, fmt.Sprintf("%s name %q is not kebab-case", kind, n))
		case seen[n]:
			p.Names = append(p.Names, fmt.Sprintf("two %ss are named %q", kind, n))
		}
		seen[n] = true
		if strings.TrimSpace(promises[i]) == "" {
			p.Names = append(p.Names, fmt.Sprintf("%s %q has an empty promise", kind, n))
		}
	}
}

// expand lists the files of kind (from the split) that patterns name; a
// pattern naming none is reported with label as bad.
func expand(patterns []string, files []string, label string, bad *[]string) []string {
	var out []string
	for _, pat := range patterns {
		for _, f := range files {
			if sourceset.Match(pat, f) && !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
		if !slices.ContainsFunc(files, func(f string) bool { return sourceset.Match(pat, f) }) {
			*bad = append(*bad, label+": "+draft.OneLine(pat))
		}
	}
	sort.Strings(out)
	return out
}

// Check judges a capabilities answer for area a against the split of the
// repo's files, and returns the footprints it resolves to and what is wrong.
// A source or shared path is a file or a folder ending with a slash; the
// most specific claim wins and a tie claims the file twice.
func Check(a Area, ans Answer, split sourceset.Division) (Footprints, Problems) {
	var p Problems
	owners := make([]owner, 0, len(ans.Capabilities)+len(ans.Shared))
	var names, promises []string
	for _, c := range ans.Capabilities {
		owners = append(owners, owner{c.Name, c.Sources})
		names, promises = append(names, c.Name), append(promises, c.Promise)
	}
	for _, s := range ans.Shared {
		owners = append(owners, owner{"shared", []string{s.Path}})
	}
	owned := resolve(a, owners, &p)
	named(&p, "capability", names, promises)
	var fp Footprints
	for i, c := range ans.Capabilities {
		f := Footprint{Name: c.Name, Promise: c.Promise, Sources: owned[fmt.Sprint(i)]}
		f.Tests = expand(c.Tests, split.Files[sourceset.Test], c.Name, &p.NotTest)
		f.Docs = expand(c.Docs, split.Files[sourceset.Doc], c.Name, &p.NotDoc)
		if len(f.Sources) == 0 {
			p.NoSource = append(p.NoSource, c.Name)
		}
		fp.Capabilities = append(fp.Capabilities, f)
	}
	for i, s := range ans.Shared {
		for _, f := range owned[fmt.Sprint(len(ans.Capabilities)+i)] {
			fp.Shared = append(fp.Shared, Shared{Path: f, Reason: draft.OneLine(s.Reason)})
		}
	}
	sort.Slice(fp.Shared, func(i, j int) bool { return fp.Shared[i].Path < fp.Shared[j].Path })
	return fp, p
}

// CheckSplit judges a split answer for area a: two or more parts with
// unique names, every source file in exactly one. It returns the parts as
// areas, each with a as its Parent.
func CheckSplit(a Area, ans Answer) ([]Area, Problems) {
	p := Problems{Split: true}
	owners := make([]owner, len(ans.Split))
	var names, promises []string
	for i, s := range ans.Split {
		owners[i] = owner{s.Name, s.Sources}
		names, promises = append(names, s.Name), append(promises, s.Promise)
	}
	if len(ans.Split) < 2 {
		p.Names = append(p.Names, "a split names at least two parts")
	}
	owned := resolve(a, owners, &p)
	named(&p, "part", names, promises)
	var parts []Area
	for i, s := range ans.Split {
		files := owned[fmt.Sprint(i)]
		if len(files) == 0 {
			p.NoSource = append(p.NoSource, s.Name)
		}
		parts = append(parts, Area{Name: s.Name, Promise: draft.OneLine(s.Promise), Parent: a.Name, Files: files})
	}
	return parts, p
}
