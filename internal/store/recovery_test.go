package store

import (
	"reflect"
	"testing"
	"time"
)

func sampleRecovered(name string) Recovered {
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	return Recovered{Capability: name, ProposalAt: t0, At: t0.Add(time.Hour), Status: "recovered",
		File: "# " + name + "\n", Terms: "**Term**: meaning\n", Details: []byte(`{"dropped":[]}`),
		SessionID: "sess-" + name, Model: "m", Attempts: 2, Appetite: 200000, Input: 1000, CachedInput: 200, Output: 300}
}

func TestRecoveredRoundTrip(t *testing.T) {
	s, _ := open(t)
	want := []Recovered{sampleRecovered("alpha"), sampleRecovered("beta")}
	if err := s.PutRecovered([]Recovered{want[1], want[0]}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recovered()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestRecoveredReplaceKeepsOthers(t *testing.T) {
	s, _ := open(t)
	if err := s.PutRecovered([]Recovered{sampleRecovered("alpha"), sampleRecovered("beta")}); err != nil {
		t.Fatal(err)
	}
	again := sampleRecovered("beta")
	again.Status, again.File = "failed", "# changed\n"
	if err := s.PutRecovered([]Recovered{again}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recovered()
	if err != nil || len(got) != 2 {
		t.Fatalf("Recovered = %d rows, %v; want 2", len(got), err)
	}
	if got[0].File != sampleRecovered("alpha").File {
		t.Errorf("alpha changed: %+v", got[0])
	}
	if got[1].Status != "failed" || got[1].File != "# changed\n" {
		t.Errorf("beta not replaced: %+v", got[1])
	}
}

func TestRecoveredClearedByNewGather(t *testing.T) {
	s, _ := open(t)
	if err := s.PutRecovered([]Recovered{sampleRecovered("alpha")}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGather(sampleGather()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recovered()
	if err != nil || len(got) != 0 {
		t.Errorf("Recovered after a new gather = %d rows, %v; want none", len(got), err)
	}
}

func TestRecoveredClearedByNewProposal(t *testing.T) {
	s, _ := open(t)
	if err := s.PutRecovered([]Recovered{sampleRecovered("alpha")}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProposal(sampleProposal()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recovered()
	if err != nil || len(got) != 0 {
		t.Errorf("Recovered after a new proposal = %d rows, %v; want none", len(got), err)
	}
}

func TestRecoveredEmptyStore(t *testing.T) {
	s, _ := open(t)
	got, err := s.Recovered()
	if err != nil || len(got) != 0 {
		t.Errorf("Recovered = %d rows, %v; want none", len(got), err)
	}
}
