package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func sampleProposal() Proposal {
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	return Proposal{GatherAt: t0, At: t0.Add(time.Hour), SessionID: "sess-1", Model: "m",
		Appetite: 200000, Input: 1000, CachedInput: 200, Output: 300, Body: []byte(`{"promise":"p"}`)}
}

func TestProposalRoundTrip(t *testing.T) {
	s, _ := open(t)
	want := sampleProposal()
	if err := s.PutProposal(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Proposed()
	if err != nil || !ok {
		t.Fatalf("Proposed = %v, %v", ok, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestProposalReplaces(t *testing.T) {
	s, _ := open(t)
	first := sampleProposal()
	if err := s.PutProposal(first); err != nil {
		t.Fatal(err)
	}
	second := sampleProposal()
	second.SessionID, second.Body = "sess-2", []byte(`{"promise":"q"}`)
	if err := s.PutProposal(second); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Proposed()
	if err != nil || !ok || got.SessionID != "sess-2" || string(got.Body) != `{"promise":"q"}` {
		t.Errorf("got %+v, %v, %v; want the second proposal", got, ok, err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM init_proposal").Scan(&n); err != nil || n != 1 {
		t.Errorf("rows = %d, %v; want 1", n, err)
	}
}

func TestProposedFreshStoreIsFalse(t *testing.T) {
	s, _ := open(t)
	if _, ok, err := s.Proposed(); ok || err != nil {
		t.Errorf("Proposed = %v, %v; want false, nil", ok, err)
	}
	if rs, err := s.Recovered(); len(rs) != 0 || err != nil {
		t.Errorf("Recovered = %d rows, %v; want none", len(rs), err)
	}
}

func TestPutGatherClearsProposal(t *testing.T) {
	s, _ := open(t)
	if err := s.PutGather(sampleGather()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProposal(sampleProposal()); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGather(sampleGather()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Proposed(); ok || err != nil {
		t.Errorf("Proposed after a new gather = %v, %v; want false, nil", ok, err)
	}
}

func TestMigrationFiveUpgradesVersionFourStore(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "physa.db"))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.n > 4 {
			break
		}
		body, err := migrationFiles.ReadFile("migrations/" + m.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 4"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gather_runs VALUES (1, '2026-11-21T09:00:00Z', '2025-11-21T09:00:00Z', 10, 0, '', 'none')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := version(t, s); got != 6 {
		t.Errorf("user_version = %d, want 6", got)
	}
	g, ok, err := s.Gathered()
	if err != nil || !ok || g.Run.MaxPRs != 10 || g.Run.ForgeReason != "none" {
		t.Errorf("gather after upgrade = %+v, %v, %v; want it intact", g.Run, ok, err)
	}
	if _, ok, err := s.Proposed(); ok || err != nil {
		t.Errorf("Proposed = %v, %v; want false, nil", ok, err)
	}
}
