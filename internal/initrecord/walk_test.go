package initrecord

import (
	"reflect"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/store"
)

func sampleWalk() Walk {
	t0 := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	return Walk{AreasAt: t0, At: t0.Add(time.Hour), Model: "m", Input: 10, CachedInput: 2, Output: 3, Body: []byte(`{"areas":[]}`)}
}

func TestWalkRoundTripAndReplace(t *testing.T) {
	s, _ := open(t)
	want := sampleWalk()
	if err := s.PutWalk(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Walk()
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Walk = %+v, %v, %v; want %+v", got, ok, err, want)
	}
	second := sampleWalk()
	second.Body = []byte(`{"areas":[1]}`)
	if err := s.PutWalk(second); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Walk()
	if string(got.Body) != `{"areas":[1]}` {
		t.Errorf("body = %s; want the second", got.Body)
	}
	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM init_walk").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1", n, err)
	}
}

func TestWalkEmptyStoreHasNone(t *testing.T) {
	s, _ := open(t)
	if _, ok, err := s.Walk(); ok || err != nil {
		t.Fatalf("fresh Walk = %v, %v; want false, nil", ok, err)
	}
}

func TestPutGatherClearsWalk(t *testing.T) {
	s, _ := open(t)
	if err := s.PutWalk(sampleWalk()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGather(store.Gather{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Walk(); ok || err != nil {
		t.Errorf("Walk after a new gather = %v, %v; want false, nil", ok, err)
	}
}

func TestPutAreasClearsWalk(t *testing.T) {
	s, _ := open(t)
	if err := s.PutWalk(sampleWalk()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutAreas(sampleAreas()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Walk(); ok || err != nil {
		t.Errorf("Walk after new areas = %v, %v; want false, nil", ok, err)
	}
	if _, ok, _ := s.Areas(); !ok {
		t.Error("the new areas were not stored")
	}
}

func TestPutProposalKeepsWalk(t *testing.T) {
	s, _ := open(t)
	if err := s.PutWalk(sampleWalk()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProposal(Proposal{Body: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Walk(); !ok || err != nil {
		t.Errorf("Walk after a proposal = %v, %v; want kept", ok, err)
	}
}

func TestWalkSpendingOrder(t *testing.T) {
	s, _ := open(t)
	for _, ph := range []string{"propose", "walk", "recover", "areas", "discover"} {
		if err := s.AddSpent(Spent{Phase: ph, Count: spend.Count{Raw: adapter.Tokens{Input: 1}, Spend: spend.Spend{Tokens: 1}}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Spending()
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, p := range got {
		order = append(order, p.Phase)
	}
	if want := []string{"discover", "areas", "walk", "propose", "recover"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v; want %v", order, want)
	}
}
