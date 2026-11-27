package recoverinput

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/walk"
)

// NoPromise is the product's promise when no proposal is stored to give
// one: the stand-in the check also uses.
const NoPromise = "Not yet stated."

// Source is the capabilities of the source a stored recovery came from, in
// the order the map lists them.
type Source struct {
	// FromWalk is true when they are the walk's, false when they are the
	// proposal's.
	FromWalk bool
	// At is the walk's time when FromWalk; a stored recovery counts only when
	// its ProposalAt equals it. Zero for the proposal, which keeps every
	// stored row as it always has.
	At           time.Time
	Capabilities []mapping.Capability
}

// Holds reports whether a stored recovery made at proposalAt is of this
// source: always for the proposal, and for the walk only when the row was
// recovered from this walk, so a row from an earlier source stays stored
// but is not listed.
func (s Source) Holds(proposalAt time.Time) bool {
	return !s.FromWalk || proposalAt.Equal(s.At)
}

// Listed is the source of the stored recovery: the walk's capabilities and
// promises in walk order when st holds both the walk and the placements,
// else the proposal's capabilities when proposed is true, else ok is false.
func Listed(ctx context.Context, st interface {
	Walk(ctx context.Context) (initrecord.Walk, bool, error)
	Place(ctx context.Context) (initrecord.Place, bool, error)
}, proposed mapping.Proposal, hasProposal bool) (src Source, ok bool, err error) {
	w, walked, err := st.Walk(ctx)
	if err != nil {
		return src, false, err
	}
	if _, placed, err := st.Place(ctx); err != nil {
		return src, false, err
	} else if walked && placed {
		var wr walk.Record
		if err := json.Unmarshal(w.Body, &wr); err != nil {
			return src, false, fmt.Errorf("the stored walk cannot be read: %w", err)
		}
		src = Source{FromWalk: true, At: w.At}
		for _, c := range wr.Capabilities {
			src.Capabilities = append(src.Capabilities, mapping.Capability{Name: c.Name, Promise: c.Promise})
		}
		return src, true, nil
	}
	if !hasProposal {
		return src, false, nil
	}
	return Source{Capabilities: proposed.Capabilities}, true, nil
}
