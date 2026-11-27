package recoverinput

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/walk"
)

// Capability is one capability of the walk as its recovery is listed: its
// name and promise.
type Capability struct {
	Name, Promise string
}

// Source is the capabilities of the walk a stored recovery came from, in
// the order the map lists them.
type Source struct {
	// At is the walk's time; a stored recovery counts only when its
	// ProposalAt equals it.
	At           time.Time
	Capabilities []Capability
}

// Holds reports whether a stored recovery made at proposalAt is of this
// source: only when the row was recovered from this walk, so a row from an
// earlier source (an earlier walk, or a proposal an earlier physa stored)
// stays stored but is not listed.
func (s Source) Holds(proposalAt time.Time) bool {
	return proposalAt.Equal(s.At)
}

// Listed is the source of the stored recovery: the walk's capabilities and
// promises in walk order, ok only when st holds both the walk and the
// placements.
func Listed(ctx context.Context, st interface {
	Walk(ctx context.Context) (initrecord.Walk, bool, error)
	Place(ctx context.Context) (initrecord.Place, bool, error)
}) (src Source, ok bool, err error) {
	w, walked, err := st.Walk(ctx)
	if err != nil {
		return src, false, err
	}
	_, placed, err := st.Place(ctx)
	if err != nil || !walked || !placed {
		return src, false, err
	}
	var wr walk.Record
	if err := json.Unmarshal(w.Body, &wr); err != nil {
		return src, false, fmt.Errorf("the stored walk cannot be read: %w", err)
	}
	src = Source{At: w.At}
	for _, c := range wr.Capabilities {
		src.Capabilities = append(src.Capabilities, Capability{Name: c.Name, Promise: c.Promise})
	}
	return src, true, nil
}
