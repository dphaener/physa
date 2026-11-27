package initrecord

import (
	"context"
	"errors"
	"github.com/dphaener/physa/internal/store"
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
	if _, ok, err := s.Areas(t.Context()); ok || err != nil {
		t.Fatalf("fresh Areas = %v, %v; want false, nil", ok, err)
	}
	want := sampleAreas()
	if err := s.PutAreas(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Areas(t.Context())
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Areas = %+v, %v, %v; want %+v", got, ok, err, want)
	}
	second := sampleAreas()
	second.Body = []byte(`{"areas":[1]}`)
	if err := s.PutAreas(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Areas(t.Context())
	if string(got.Body) != `{"areas":[1]}` {
		t.Errorf("body = %s; want the second", got.Body)
	}
	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM init_areas").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1", n, err)
	}
}

func TestPutGatherClearsAreas(t *testing.T) {
	s, _ := open(t)
	if err := s.PutAreas(t.Context(), sampleAreas()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGather(t.Context(), store.Gather{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Areas(t.Context()); ok || err != nil {
		t.Errorf("Areas after a new gather = %v, %v; want false, nil", ok, err)
	}
}

func TestSpendingOrdersAreasBetweenDiscoverAndPropose(t *testing.T) {
	s, _ := open(t)
	for _, p := range []string{"recover", "propose", "areas", "other", "discover"} {
		if err := s.AddSpent(t.Context(), Spent{Phase: p}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Spending(t.Context())
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

func TestPutAreasWithCancelledContextDeletesNothing(t *testing.T) {
	s, _ := open(t)
	if err := s.PutWalk(t.Context(), sampleWalk()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.PutAreas(ctx, sampleAreas()); !errors.Is(err, context.Canceled) {
		t.Errorf("PutAreas with a cancelled context = %v; want context.Canceled", err)
	}
	if _, ok, err := s.Areas(t.Context()); ok || err != nil {
		t.Errorf("Areas = %v, %v; want none stored", ok, err)
	}
	if _, ok, err := s.Walk(t.Context()); !ok || err != nil {
		t.Errorf("Walk = %v, %v; want the walk kept", ok, err)
	}
}
