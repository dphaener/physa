package recoverinput

import (
	"slices"

	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/walk"
)

// Walked is a capability found in the code, with what its recovery reads:
// its area and, when that area is a part, the area it was split from, its
// footprint, and the PRs and changes placed with it.
type Walked struct {
	Name, Promise string
	Area, Parent  string
	Footprint     recovering.Footprint
	// Sources are the PRs and changes placed with the capability, and the
	// changes those revert or are reverted by, in the gather's order, whole
	// and with no docs: the session reads the footprint's docs itself.
	Sources store.Gather
}

// Walk is one Walked for each capability of the stored walk w, in walk
// order. Its source files carry their line counts as root holds them now, a
// file that cannot be read as text showing none (0), and its sources are
// what the stored placement p places with it in the gather g. A capability
// p places nothing with has empty sources and is still recovered.
func Walk(root string, w walk.Record, p place.Record, g store.Gather) []Walked {
	byName := map[string]place.CapabilityPlaced{}
	for _, c := range p.ByCapability {
		byName[c.Capability] = c
	}
	out := make([]Walked, 0, len(w.Capabilities))
	for _, c := range w.Capabilities {
		out = append(out, Walked{
			Name: c.Name, Promise: c.Promise, Area: c.Area, Parent: c.Parent,
			Footprint: footprint(root, c),
			Sources:   placed(g, byName[c.Name]),
		})
	}
	return out
}

// footprint is c's source files with their line counts, and its tests,
// docs and other files. These are the entries as the walk's session wrote
// them, folders keeping their trailing slash, since that is the form the
// walk's own message named the repo's files in and a session with Glob
// opens a folder itself; a walk stored before entries were kept gives its
// expanded lists instead.
func footprint(root string, c walk.CapabilityRecord) recovering.Footprint {
	tracked := make([]vcs.TrackedFile, len(c.Sources))
	for i, f := range c.Sources {
		tracked[i] = vcs.TrackedFile{Path: f}
	}
	lines := map[string]int{}
	if l, err := sourceset.List(root, tracked); err == nil {
		for _, f := range l.Files {
			lines[f.Path] = f.Lines
		}
	}
	fp := recovering.Footprint{Sources: make([]recovering.SourceFile, len(c.Sources))}
	for i, f := range c.Sources {
		fp.Sources[i] = recovering.SourceFile{Path: f, Lines: lines[f]}
	}
	e := c.Entries
	if len(e.Tests)+len(e.Docs)+len(e.Other) == 0 {
		e = walk.Entries{Tests: c.Tests, Docs: c.Docs, Other: c.Other}
	}
	fp.Tests, fp.Docs, fp.Other = slices.Clone(e.Tests), slices.Clone(e.Docs), slices.Clone(e.Other)
	return fp
}

// placed is the part of g that c's placement holds: its PRs, the changes
// placed there on their own, each matched by its full or short id, and the
// changes those revert or are reverted by.
func placed(g store.Gather, c place.CapabilityPlaced) store.Gather {
	prs := map[int]bool{}
	for _, n := range c.PRs {
		prs[n] = true
	}
	ids := map[string]bool{}
	for _, id := range c.Changes {
		if id != "" {
			ids[id] = true
		}
	}
	return pick(g,
		func(p store.GatheredPR) bool { return prs[p.Number] },
		func(ch store.GatheredChange) bool { return ids[ch.ID] || (ch.Short != "" && ids[ch.Short]) })
}
