package store

import (
	"reflect"
	"testing"
	"time"
)

func sampleGather() Gather {
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	return Gather{
		Run:  GatherRun{At: t0, Since: t0.AddDate(-1, 0, 0), MaxPRs: 1000, PRsLimited: true, Forge: "github", ForgeReason: ""},
		Docs: []Doc{{Path: "AGENTS.md", Kind: "agents", Body: "rules"}, {Path: "README.md", Kind: "readme", Body: "hi"}},
		Changes: []GatheredChange{
			{ID: "aaa111", Short: "aaa", Subject: "feat: x", Message: "feat: x\n\nbody", Author: "a", At: t0,
				Trailers: []Trailer{{Key: "Session", Value: "s1"}}, Files: []string{"a.go", "b.go"}},
			{ID: "bbb222", Short: "bbb", Subject: "Revert x", Message: "Revert x", Author: "b", At: t0.Add(time.Hour),
				Trailers: []Trailer{}, Reverts: "aaa111", Files: []string{}},
		},
		PRs: []GatheredPR{
			{Number: 7, Title: "t", Body: "b", Author: "a", MergedAt: t0, Issues: []string{"#3"},
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
	if err := s.PutGather(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Gathered()
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	want.PRs[1].Comments = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestGatheredFreshStoreIsFalse(t *testing.T) {
	s, _ := open(t)
	if _, ok, err := s.Gathered(); ok || err != nil {
		t.Errorf("Gathered = %v, %v; want false, nil", ok, err)
	}
}

func TestPutGatherReplaces(t *testing.T) {
	s, _ := open(t)
	if err := s.PutGather(sampleGather()); err != nil {
		t.Fatal(err)
	}
	next := Gather{Run: GatherRun{At: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), MaxPRs: 5, Forge: "", ForgeReason: "no forge"},
		Docs: []Doc{{Path: "CLAUDE.md", Kind: "agents", Body: "x"}}}
	if err := s.PutGather(next); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Gathered()
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
	if err := s.PutGather(first); err != nil {
		t.Fatal(err)
	}
	bad := sampleGather()
	bad.Docs = []Doc{{Path: "x", Kind: "k"}, {Path: "x", Kind: "k"}} // duplicate primary key
	bad.Changes = nil
	if err := s.PutGather(bad); err == nil {
		t.Fatal("PutGather with a duplicate doc path succeeded")
	}
	got, ok, err := s.Gathered()
	if err != nil || !ok {
		t.Fatalf("Gathered = %v, %v", ok, err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Errorf("earlier gather changed: %+v", got)
	}
}
