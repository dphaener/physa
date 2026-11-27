package recoverinput

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
)

// listStore holds a walk and a placement, each when set.
type listStore struct {
	walk  *initrecord.Walk
	place bool
}

func (s listStore) Walk(ctx context.Context) (initrecord.Walk, bool, error) {
	if s.walk == nil {
		return initrecord.Walk{}, false, nil
	}
	return *s.walk, true, nil
}

func (s listStore) Place(ctx context.Context) (initrecord.Place, bool, error) {
	return initrecord.Place{}, s.place, nil
}

func storedWalk(t *testing.T, at time.Time) *initrecord.Walk {
	t.Helper()
	b, err := json.Marshal(testWalk())
	if err != nil {
		t.Fatal(err)
	}
	return &initrecord.Walk{At: at, Body: b}
}

func TestListedTakesTheWalkWhenBothAreStoredElseTheProposal(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	prop := mapping.Proposal{Capabilities: []mapping.Capability{{Name: "delivery", Promise: "Ships."}}}

	src, ok, err := Listed(listStore{storedWalk(t, at), true}, prop, true)
	if err != nil || !ok || !src.FromWalk || len(src.Capabilities) != len(testWalk().Capabilities) || src.Capabilities[0].Name != testWalk().Capabilities[0].Name {
		t.Errorf("both: %+v, %v, %v", src, ok, err)
	}
	if !src.Holds(at) || src.Holds(at.Add(-time.Hour)) {
		t.Error("a walk's source holds only the rows recovered from that walk")
	}

	for name, st := range map[string]listStore{"walk only": {storedWalk(t, at), false}, "neither": {}} {
		src, ok, err := Listed(st, prop, true)
		if err != nil || !ok || src.FromWalk || len(src.Capabilities) != 1 || !src.Holds(at.Add(-time.Hour)) {
			t.Errorf("%s: %+v, %v, %v", name, src, ok, err)
		}
	}
	if _, ok, err := Listed(listStore{storedWalk(t, at), false}, mapping.Proposal{}, false); ok || err != nil {
		t.Errorf("a walk without placements and no proposal: %v, %v", ok, err)
	}
}

func TestListedWalkOnlyNeedsNoProposal(t *testing.T) {
	src, ok, err := Listed(listStore{storedWalk(t, time.Now()), true}, mapping.Proposal{}, false)
	if err != nil || !ok || !src.FromWalk {
		t.Errorf("%+v, %v, %v", src, ok, err)
	}
}
