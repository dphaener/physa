package areas

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/store"
)

// Record is the body init's record keeps for the areas, which the walk reads
// with Load: the split as code expanded it, the files left out by reason
// (sourceset's Left constants), the areas, and how they were made.
type Record struct {
	Split    sourceset.Division   `json:"split"`
	Left     map[string]LeftFiles `json:"left"`
	Areas    []AreaRecord         `json:"areas"`
	Parts    int                  `json:"parts"`
	Merges   []Joint              `json:"merges"`
	Sessions []string             `json:"sessions"`
}

// LeftFiles is the files left out for one reason.
type LeftFiles struct {
	Count int      `json:"count"`
	Paths []string `json:"paths"`
}

// AreaRecord is one stored area: its name, its promise and its files, sorted.
type AreaRecord struct {
	Name    string   `json:"name"`
	Promise string   `json:"promise"`
	Files   []string `json:"files"`
}

// Loader is the store as Load reads it.
type Loader interface {
	Areas() (store.Areas, bool, error)
}

// Load reads the stored areas and decodes their body, false when none are
// stored.
func Load(st Loader) (Record, bool, error) {
	a, ok, err := st.Areas()
	if err != nil || !ok {
		return Record{}, false, err
	}
	var r Record
	if err := json.Unmarshal(a.Body, &r); err != nil {
		return Record{}, false, fmt.Errorf("the stored areas do not decode: %w", err)
	}
	return r, true, nil
}

// record builds the body from the listing, the split and the areas.
func record(l sourceset.Listing, d sourceset.Division, areas []*Area, parts int, merges []Joint, ids []string) Record {
	r := Record{Split: d, Left: map[string]LeftFiles{}, Areas: []AreaRecord{}, Parts: parts, Merges: merges, Sessions: ids}
	for reason, paths := range l.Left {
		r.Left[reason] = LeftFiles{Count: len(paths), Paths: paths}
	}
	for _, a := range areas {
		files := append([]string{}, a.Files...)
		sort.Strings(files)
		r.Areas = append(r.Areas, AreaRecord{Name: a.Name, Promise: a.Promise, Files: files})
	}
	return r
}
