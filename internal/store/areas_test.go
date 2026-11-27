package store

import (
	"reflect"
	"testing"
	"time"
)

func sampleAreas() Areas {
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	return Areas{GatherAt: t0, At: t0.Add(time.Hour), Model: "m", Input: 10, CachedInput: 2, Output: 3, Body: []byte(`{"areas":[]}`)}
}

func TestAreasRoundTripAndReplace(t *testing.T) {
	s, _ := open(t)
	if _, ok, err := s.Areas(); ok || err != nil {
		t.Fatalf("fresh Areas = %v, %v; want false, nil", ok, err)
	}
	want := sampleAreas()
	if err := s.PutAreas(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Areas()
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Areas = %+v, %v, %v; want %+v", got, ok, err, want)
	}
	second := sampleAreas()
	second.Body = []byte(`{"areas":[1]}`)
	if err := s.PutAreas(second); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Areas()
	if string(got.Body) != `{"areas":[1]}` {
		t.Errorf("body = %s; want the second", got.Body)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM init_areas").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1", n, err)
	}
}

func TestPutGatherClearsAreas(t *testing.T) {
	s, _ := open(t)
	if err := s.PutAreas(sampleAreas()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGather(Gather{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Areas(); ok || err != nil {
		t.Errorf("Areas after a new gather = %v, %v; want false, nil", ok, err)
	}
}

func TestPutProposalKeepsAreas(t *testing.T) {
	s, _ := open(t)
	if err := s.PutAreas(sampleAreas()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProposal(sampleProposal()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Areas(); !ok || err != nil {
		t.Errorf("Areas after a proposal = %v, %v; want kept", ok, err)
	}
}

func TestSpendingOrdersAreasBetweenDiscoverAndPropose(t *testing.T) {
	s, _ := open(t)
	for _, p := range []string{"recover", "propose", "areas", "other", "discover"} {
		if err := s.AddSpent(Spent{Phase: p}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Spending()
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, g := range got {
		order = append(order, g.Phase)
	}
	if want := []string{"discover", "areas", "propose", "recover", "other"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v; want %v", order, want)
	}
}
