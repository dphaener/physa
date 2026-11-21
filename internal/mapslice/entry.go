package mapslice

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/mapfile"
)

// Taken is the entries an agent took by slug, in the order it named them.
type Taken struct {
	Entries []TakenEntry `json:"entries"`
}

// TakenEntry is one taken entry in full and where it lives in the map.
type TakenEntry struct {
	Capability string `json:"capability"` // the capability's name, or "product"
	Entry
}

// Take takes exactly the entries slugs name, in full, from the product or
// any capability, in the order asked; a slug named twice is taken once, at
// its first place. It fails, taking nothing, when any slug names no entry,
// and the error names every such slug.
func Take(m *mapfile.Map, slugs []string) (*Taken, error) {
	if m == nil {
		return nil, errors.New("no map to take entries from")
	}
	if len(slugs) == 0 {
		return nil, errors.New("no entries named; a slice lists the slugs of its capabilities' decisions and questions")
	}
	type place struct {
		capability string
		entry      *mapfile.Entry
	}
	bySlug := map[string]place{}
	files := m.Capabilities
	if m.Product != nil {
		files = append([]*mapfile.File{m.Product}, files...)
	}
	for _, f := range files {
		for i := range f.Entries {
			if e := &f.Entries[i]; e.Slug != "" {
				if _, dup := bySlug[e.Slug]; !dup {
					bySlug[e.Slug] = place{f.Name, e}
				}
			}
		}
	}
	t := &Taken{Entries: []TakenEntry{}}
	seen := map[string]bool{}
	var unknown []string
	for _, s := range slugs {
		if seen[s] {
			continue
		}
		seen[s] = true
		p, ok := bySlug[s]
		if !ok {
			unknown = append(unknown, fmt.Sprintf("%q", s))
			continue
		}
		t.Entries = append(t.Entries, TakenEntry{Capability: p.capability, Entry: toEntry(*p.entry)})
	}
	if len(unknown) > 0 {
		noun := "entry"
		if len(unknown) > 1 {
			noun = "entries"
		}
		return nil, fmt.Errorf("unknown %s %s; a slice lists the slugs of its capabilities' decisions and questions", noun, strings.Join(unknown, ", "))
	}
	return t, nil
}

// WriteTakenMD writes each taken entry as the map writes it, with one blank
// line between entries, and nothing else.
func WriteTakenMD(w io.Writer, t *Taken) error {
	var b strings.Builder
	for i, e := range t.Entries {
		if i > 0 {
			b.WriteString("\n")
		}
		writeEntry(&b, "###", e.Entry)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteTakenJSON writes t as one line of JSON, {"entries":[...]}, each entry
// with the capability it lives in.
func WriteTakenJSON(w io.Writer, t *Taken) error {
	return writeJSON(w, t)
}
