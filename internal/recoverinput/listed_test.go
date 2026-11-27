package recoverinput

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/initrecord"
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

func TestListedTakesTheWalkOnlyWhenTheWalkAndPlacementsAreStored(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	src, ok, err := Listed(t.Context(), listStore{storedWalk(t, at), true})
	want := testWalk().Capabilities
	if err != nil || !ok || !src.At.Equal(at) || len(src.Capabilities) != len(want) ||
		src.Capabilities[0] != (Capability{Name: want[0].Name, Promise: want[0].Promise}) {
		t.Errorf("both: %+v, %v, %v", src, ok, err)
	}
	if !src.Holds(at) || src.Holds(at.Add(-time.Hour)) {
		t.Error("a walk's source holds only the rows recovered from that walk")
	}
	for name, st := range map[string]listStore{"walk only": {storedWalk(t, at), false}, "placements only": {nil, true}, "neither": {}} {
		if src, ok, err := Listed(t.Context(), st); ok || err != nil || len(src.Capabilities) != 0 {
			t.Errorf("%s: %+v, %v, %v", name, src, ok, err)
		}
	}
}

func TestListedWalkThatCannotBeReadIsAnError(t *testing.T) {
	st := listStore{&initrecord.Walk{Body: []byte("not json")}, true}
	if _, ok, err := Listed(t.Context(), st); ok || err == nil {
		t.Errorf("%v, %v", ok, err)
	}
}
