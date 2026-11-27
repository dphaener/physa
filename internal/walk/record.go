package walk

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/initrecord"
)

// Record is the body init's record keeps for the walk, read back with Load.
// Its file paths are init's own and never reach the map.
type Record struct {
	Capabilities []CapabilityRecord `json:"capabilities"`
	Shared       []SharedRecord     `json:"shared"`
	Splits       []SplitRecord      `json:"splits"`
	Renamed      []Rename           `json:"renamed"`
	Sessions     []string           `json:"sessions"`
}

// CapabilityRecord is a capability with its footprint, its area and, when
// that area is a part, the area it was split from.
type CapabilityRecord struct {
	Capability
	Area   string `json:"area"`
	Parent string `json:"parent"`
	// Entries are the test, doc and other paths as the session wrote them,
	// folders keeping their trailing slash; empty in an older stored walk.
	Entries Entries `json:"entries,omitempty"`
}

// Entries are the paths a footprint names for one kind of file, as written.
type Entries struct {
	Tests []string `json:"tests,omitempty"`
	Docs  []string `json:"docs,omitempty"`
	Other []string `json:"other,omitempty"`
}

// SharedRecord is a source file no capability owns, with its area.
type SharedRecord struct {
	Shared
	Area string `json:"area"`
}

// SplitRecord is an area a session split, with the parts walked in its place.
type SplitRecord struct {
	Area  string             `json:"area"`
	Parts []areas.AreaRecord `json:"parts"`
}

// Rename is a name an earlier area took: Area's From is kept as To.
type Rename struct {
	Area string `json:"area"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Load reads the stored walk and decodes its body, false when none exists.
func Load(ctx context.Context, st *initrecord.Store) (Record, bool, error) {
	var r Record
	w, ok, err := st.Walk(ctx)
	if err == nil && ok {
		err = json.Unmarshal(w.Body, &r)
	}
	return r, ok && err == nil, err
}

// collect reads the jobs in stored order, a split area's parts right after
// it and never in the order sessions ended, into res and the record. A name
// an earlier area took gets the next free number. failed names what failed.
func collect(jobs []*job, res *Result) (rec Record, failed []string) {
	rec = Record{Capabilities: []CapabilityRecord{}, Shared: []SharedRecord{}, Splits: []SplitRecord{}, Renamed: []Rename{}}
	taken := map[string]bool{}
	var all []*job
	for _, j := range jobs {
		all = append(append(all, j), j.parts...)
	}
	for _, j := range all {
		a := j.area
		ar := AreaResult{Name: a.Name, Parent: a.Parent, Capabilities: []string{}, Parts: []string{}}
		res.FixRounds, res.Unclaimed = res.FixRounds+j.rounds, res.Unclaimed+j.unclaimed
		if j.err != nil {
			failed, j.fp = append(failed, fmt.Sprintf("%s: %v", a.Name, j.err)), Footprints{}
		}
		var parts []areas.AreaRecord
		for _, p := range j.parts {
			ar.Parts = append(ar.Parts, p.area.Name)
			parts = append(parts, areas.AreaRecord{Name: p.area.Name, Promise: p.area.Promise, Files: p.area.Files})
		}
		if len(parts) > 0 {
			rec.Splits = append(rec.Splits, SplitRecord{a.Name, parts})
		}
		for _, c := range j.fp.Capabilities {
			kept := Capability{Name: c.Name, Promise: c.Promise, Sources: c.Sources, Tests: append([]string{}, c.Tests...), Docs: append([]string{}, c.Docs...), Other: append([]string{}, c.Other...)}
			ent := Entries{slices.Clone(c.Entries.Tests), slices.Clone(c.Entries.Docs), slices.Clone(c.Entries.Other)}
			for n := 2; taken[kept.Name]; n++ {
				kept.Name = fmt.Sprintf("%s-%d", c.Name, n)
			}
			if taken[kept.Name] = true; kept.Name != c.Name {
				rec.Renamed = append(rec.Renamed, Rename{Area: a.Name, From: c.Name, To: kept.Name})
			}
			rec.Capabilities = append(rec.Capabilities, CapabilityRecord{kept, a.Name, a.Parent, ent})
			ar.Capabilities, ar.Owned = append(ar.Capabilities, kept.Name), ar.Owned+len(c.Sources)
		}
		for _, s := range j.fp.Shared {
			rec.Shared, ar.Shared = append(rec.Shared, SharedRecord{s, a.Name}), ar.Shared+1
		}
		res.Areas = append(res.Areas, ar)
		res.Capabilities, res.Owned, res.Shared = res.Capabilities+len(ar.Capabilities), res.Owned+ar.Owned, res.Shared+ar.Shared
	}
	res.Renamed = rec.Renamed
	return rec, failed
}
