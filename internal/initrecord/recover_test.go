package initrecord

import (
	"math"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/store"
)

func TestRecoverSeedEmptyStoreHasNone(t *testing.T) {
	s, _ := open(t)
	if _, ok, err := s.RecoverSeed(t.Context()); ok || err != nil {
		t.Fatalf("fresh RecoverSeed = %v, %v; want false, nil", ok, err)
	}
}

func TestRecoverSeedRoundTrips(t *testing.T) {
	for _, seed := range []uint64{0, 1, 20261126, math.MaxInt64, math.MaxInt64 + 1, math.MaxUint64} {
		s, _ := open(t)
		at := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
		if err := s.PutRecoverSeed(t.Context(), seed, at); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.RecoverSeed(t.Context())
		if err != nil || !ok || got != seed {
			t.Errorf("RecoverSeed after %d = %d, %v, %v; want %d, true, nil", seed, got, ok, err, seed)
		}
	}
}

func TestRecoverSeedReplaces(t *testing.T) {
	s, _ := open(t)
	at := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	if err := s.PutRecoverSeed(t.Context(), 7, at); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRecoverSeed(t.Context(), math.MaxUint64, at); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.RecoverSeed(t.Context())
	if err != nil || !ok || got != math.MaxUint64 {
		t.Errorf("RecoverSeed = %d, %v, %v; want the second seed", got, ok, err)
	}
	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM init_recover").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1", n, err)
	}
}

func TestPutGatherClearsRecoverSeed(t *testing.T) {
	s, _ := open(t)
	if err := s.PutRecoverSeed(t.Context(), 42, time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGather(t.Context(), store.Gather{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.RecoverSeed(t.Context()); ok || err != nil {
		t.Errorf("RecoverSeed after a new gather = %v, %v; want false, nil", ok, err)
	}
}
