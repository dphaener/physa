package initrecord

import (
	"reflect"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/store"
)

func samplePlace() Place {
	t0 := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	return Place{WalkAt: t0, At: t0.Add(time.Hour), Body: []byte(`{"prs":[]}`)}
}

func TestPlaceRoundTripAndReplace(t *testing.T) {
	s, _ := open(t)
	want := samplePlace()
	if err := s.PutPlace(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Place(t.Context())
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Place = %+v, %v, %v; want %+v", got, ok, err, want)
	}
	second := samplePlace()
	second.Body = []byte(`{"prs":[1]}`)
	if err := s.PutPlace(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Place(t.Context())
	if string(got.Body) != `{"prs":[1]}` {
		t.Errorf("body = %s; want the second", got.Body)
	}
	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM init_place").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1", n, err)
	}
}

func TestPlaceEmptyStoreHasNone(t *testing.T) {
	s, _ := open(t)
	if _, ok, err := s.Place(t.Context()); ok || err != nil {
		t.Fatalf("fresh Place = %v, %v; want false, nil", ok, err)
	}
}

func TestPutGatherClearsPlace(t *testing.T) {
	s, _ := open(t)
	if err := s.PutPlace(t.Context(), samplePlace()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGather(t.Context(), store.Gather{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Place(t.Context()); ok || err != nil {
		t.Errorf("Place after a new gather = %v, %v; want false, nil", ok, err)
	}
}

func TestPutAreasClearsPlace(t *testing.T) {
	s, _ := open(t)
	if err := s.PutPlace(t.Context(), samplePlace()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutAreas(t.Context(), sampleAreas()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Place(t.Context()); ok || err != nil {
		t.Errorf("Place after new areas = %v, %v; want false, nil", ok, err)
	}
}

func TestPutWalkClearsPlace(t *testing.T) {
	s, _ := open(t)
	if err := s.PutPlace(t.Context(), samplePlace()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutWalk(t.Context(), sampleWalk()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Place(t.Context()); ok || err != nil {
		t.Errorf("Place after a new walk = %v, %v; want false, nil", ok, err)
	}
	if _, ok, _ := s.Walk(t.Context()); !ok {
		t.Error("the new walk was not stored")
	}
}
