package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func sampleGather() Gather {
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	return Gather{
		Run:  GatherRun{At: t0, Since: t0.AddDate(-1, 0, 0), MaxPRs: 1000, PRsLimited: true, Forge: "github", ForgeReason: ""},
		Docs: []Doc{{Path: "AGENTS.md", Kind: "agents", Body: "rules", Reason: "the agent rules"}, {Path: "README.md", Kind: "readme", Body: "hi"}},
		Changes: []GatheredChange{
			{ID: "aaa111", Short: "aaa", Subject: "feat: x", Message: "feat: x\n\nbody", Author: "a", At: t0,
				Trailers: []Trailer{{Key: "Session", Value: "s1"}}, Files: []string{"a.go", "b.go"}},
			{ID: "bbb222", Short: "bbb", Subject: "Revert x", Message: "Revert x", Author: "b", At: t0.Add(time.Hour),
				Trailers: []Trailer{}, Reverts: "aaa111", Files: []string{}},
		},
		PRs: []GatheredPR{
			{Number: 7, Title: "t", Body: "b", Author: "a", MergedBy: "m", MergedAt: t0, Issues: []string{"#3"},
				Comments: []GatheredComment{
					{Author: "r", At: t0, Kind: "review", RequestsChanges: true, Body: "fix"},
					{Author: "r", At: t0.Add(time.Minute), Kind: "inline", Body: "ok"},
				}},
			{Number: 8, Title: "u", Author: "c", MergedAt: t0, Issues: []string{}, Comments: nil},
		},
	}
}

func TestGatherRoundTrip(t *testing.T) {
	s, _ := open(t)
	want := sampleGather()
	if err := s.PutGather(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Gathered(t.Context())
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	want.PRs[1].Comments = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestGatherDocReasonRoundTrip(t *testing.T) {
	s, _ := open(t)
	want := Gather{
		Run:  GatherRun{At: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC), Since: time.Date(2025, 11, 21, 9, 0, 0, 0, time.UTC)},
		Docs: []Doc{{Path: "docs/vision.md", Kind: "overview", Body: "v", Reason: "states what the product is"}},
	}
	if err := s.PutGather(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Gathered(t.Context())
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	if len(got.Docs) != 1 || got.Docs[0].Reason != "states what the product is" {
		t.Errorf("docs = %+v; want the reason back", got.Docs)
	}
}

func TestGatherDocReasonEmptyBeforeMigrationEleven(t *testing.T) {
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
		if m.n > 10 {
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
	for _, q := range []string{
		`INSERT INTO gather_runs (id, at, since, max_prs, prs_limited, forge, forge_reason) VALUES (1, '2026-11-21T09:00:00Z', '2025-11-21T09:00:00Z', 10, 0, '', '')`,
		`INSERT INTO gather_docs (path, kind, body) VALUES ('README.md', 'readme', 'hi')`,
		"PRAGMA user_version = 10",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.Gathered(t.Context())
	if err != nil || !ok || len(got.Docs) != 1 {
		t.Fatalf("Gathered = %v, %v, %+v", ok, err, got.Docs)
	}
	if got.Docs[0].Reason != "" || got.Docs[0].Body != "hi" {
		t.Errorf("doc = %+v; want the old body and an empty reason", got.Docs[0])
	}
}

func TestGatheredFreshStoreIsFalse(t *testing.T) {
	s, _ := open(t)
	if _, ok, err := s.Gathered(t.Context()); ok || err != nil {
		t.Errorf("Gathered = %v, %v; want false, nil", ok, err)
	}
}

func TestPutGatherReplaces(t *testing.T) {
	s, _ := open(t)
	if err := s.PutGather(t.Context(), sampleGather()); err != nil {
		t.Fatal(err)
	}
	next := Gather{Run: GatherRun{At: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), MaxPRs: 5, Forge: "", ForgeReason: "no forge"},
		Docs: []Doc{{Path: "CLAUDE.md", Kind: "agents", Body: "x"}}}
	if err := s.PutGather(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Gathered(t.Context())
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	if len(got.Docs) != 1 || got.Docs[0].Path != "CLAUDE.md" || len(got.Changes) != 0 || len(got.PRs) != 0 {
		t.Errorf("not replaced: %+v", got)
	}
	if got.Run.PRsLimited || got.Run.ForgeReason != "no forge" || got.Run.MaxPRs != 5 {
		t.Errorf("run = %+v", got.Run)
	}
	for _, tbl := range []string{"gather_runs", "gather_docs", "gather_changes", "gather_prs", "gather_comments"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM " + tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		want := map[string]int{"gather_runs": 1, "gather_docs": 1}[tbl]
		if n != want {
			t.Errorf("%s rows = %d, want %d", tbl, n, want)
		}
	}
}

func TestPutGatherFailureRollsBack(t *testing.T) {
	s, _ := open(t)
	first := sampleGather()
	if err := s.PutGather(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	bad := sampleGather()
	bad.Docs = []Doc{{Path: "x", Kind: "k"}, {Path: "x", Kind: "k"}} // duplicate primary key
	bad.Changes = nil
	if err := s.PutGather(t.Context(), bad); err == nil {
		t.Fatal("PutGather with a duplicate doc path succeeded")
	}
	got, ok, err := s.Gathered(t.Context())
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Errorf("earlier gather changed: %+v", got)
	}
}

func TestGatherStoppedRoundTrip(t *testing.T) {
	s, _ := open(t)
	want := sampleGather()
	want.Run.Stopped = true
	want.Run.Reached = time.Date(2026, 5, 2, 8, 30, 0, 0, time.UTC)
	want.Run.StopReason = "rate limit reached, resets in 40m"
	if err := s.PutGather(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Gathered(t.Context())
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	r := got.Run
	if !r.Stopped || !r.Reached.Equal(want.Run.Reached) || r.StopReason != want.Run.StopReason {
		t.Errorf("run = %+v; want the mark, date and reason back", r)
	}
}

func TestGatherCompleteReadsNotStopped(t *testing.T) {
	s, _ := open(t)
	if err := s.PutGather(t.Context(), sampleGather()); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Gathered(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if r := got.Run; r.Stopped || !r.Reached.IsZero() || r.StopReason != "" {
		t.Errorf("run = %+v; want not stopped, zero Reached, empty reason", r)
	}
}

func TestGatherNotStoppedBeforeMigrationFifteen(t *testing.T) {
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
		if m.n > 14 {
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
	for _, q := range []string{
		`INSERT INTO gather_runs (id, at, since, max_prs, prs_limited, forge, forge_reason) VALUES (1, '2026-11-21T09:00:00Z', '2025-11-21T09:00:00Z', 10, 0, '', '')`,
		"PRAGMA user_version = 14",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.Gathered(t.Context())
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	if r := got.Run; r.Stopped || !r.Reached.IsZero() || r.StopReason != "" {
		t.Errorf("run = %+v; want not stopped", r)
	}
}

func TestPutWithCancelledContextLeavesStoreUnchanged(t *testing.T) {
	s, _ := open(t)
	want := sampleGather()
	if err := s.PutGather(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	other := sampleGather()
	other.Run.MaxPRs = 5
	if err := s.PutGather(ctx, other); !errors.Is(err, context.Canceled) {
		t.Errorf("PutGather with a cancelled context = %v; want context.Canceled", err)
	}
	if err := s.PutSliceRecord(ctx, SliceRecord{At: time.Now(), Capabilities: []string{"map"}}); err == nil {
		t.Error("PutSliceRecord with a cancelled context = nil; want an error")
	}
	got, ok, err := s.Gathered(t.Context())
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	want.PRs[1].Comments = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the store changed: got %+v\nwant %+v", got, want)
	}
	recs, err := s.SliceRecords(t.Context())
	if err != nil || len(recs) != 0 {
		t.Errorf("slice records = %v, %v; want none", recs, err)
	}
	if _, _, err := s.Gathered(ctx); err == nil {
		t.Error("Gathered with a cancelled context = nil; want an error")
	}
}
