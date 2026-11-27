package initrecord

import (
	"reflect"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/store"
)

func sampleConsolidated() Consolidated {
	t0 := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	return Consolidated{WalkAt: t0, At: t0.Add(time.Hour), Model: "m", Input: 10, CachedInput: 4, Output: 2, Body: []byte(`{"merged":[]}`)}
}

func TestConsolidatedRoundTripAndReplace(t *testing.T) {
	s, _ := open(t)
	want := sampleConsolidated()
	if err := s.PutConsolidated(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Consolidated()
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Consolidated = %+v, %v, %v; want %+v", got, ok, err, want)
	}
	second := sampleConsolidated()
	second.Body = []byte(`{"merged":[1]}`)
	if err := s.PutConsolidated(second); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Consolidated()
	if string(got.Body) != `{"merged":[1]}` {
		t.Errorf("body = %s; want the second", got.Body)
	}
	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM init_consolidate").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1", n, err)
	}
}

func TestConsolidatedEmptyStoreHasNone(t *testing.T) {
	s, _ := open(t)
	if _, ok, err := s.Consolidated(); ok || err != nil {
		t.Fatalf("fresh Consolidated = %v, %v; want false, nil", ok, err)
	}
}

func TestConsolidatedClearedByGatherAreasWalk(t *testing.T) {
	cases := map[string]func(*Store) error{
		"a new gather": func(s *Store) error { return s.PutGather(store.Gather{}) },
		"new areas":    func(s *Store) error { return s.PutAreas(sampleAreas()) },
		"a new walk":   func(s *Store) error { return s.PutWalk(sampleWalk()) },
	}
	for name, act := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := open(t)
			if err := s.PutConsolidated(sampleConsolidated()); err != nil {
				t.Fatal(err)
			}
			if err := act(s); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := s.Consolidated(); ok || err != nil {
				t.Errorf("Consolidated after %s = %v, %v; want false, nil", name, ok, err)
			}
		})
	}
}

func TestPutRecoveredKeepsConsolidated(t *testing.T) {
	s, _ := open(t)
	if err := s.PutConsolidated(sampleConsolidated()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRecovered([]Recovered{sampleRecovered("a")}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Consolidated(); !ok || err != nil {
		t.Errorf("Consolidated after recovery = %v, %v; want kept", ok, err)
	}
}
