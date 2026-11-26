package quiet

import (
	"reflect"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/store"
)

var day = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

// fakeForge gives change "cN" PR N, and fails once asked for stop.
type fakeForge struct {
	stop  string
	asked []string
}

func (f *fakeForge) Name() string               { return "fake" }
func (f *fakeForge) PRExists(int) (bool, error) { return true, nil }
func (f *fakeForge) ChangePR(id string) (forge.PR, bool, error) {
	f.asked = append(f.asked, id)
	if id == f.stop {
		return forge.PR{}, false, &forge.Error{Class: forge.ErrUnavailable, Reason: "rate limited"}
	}
	n := 0
	for _, r := range id[1:] {
		n = n*10 + int(r-'0')
	}
	return forge.PR{Number: n, Comments: []forge.Comment{{Author: "kim", Body: "Keep the ledger whole."}}}, true, nil
}

func ch(id string, days int, files ...string) store.GatheredChange {
	return store.GatheredChange{ID: id, Short: id, Subject: "Change " + id + " (#" + id[1:] + ")", At: day.AddDate(0, 0, days), Files: files}
}

// older is newest first: a ledger area changed five times and then left,
// and a notes area that recent work still touches.
func older() []store.GatheredChange {
	var out []store.GatheredChange
	for i := 9; i >= 5; i-- {
		out = append(out, ch("c"+string(rune('0'+i)), i, "app/ledger/entry.rb", "app/ledger/book.rb"))
	}
	for i := 4; i >= 2; i-- {
		out = append(out, ch("c"+string(rune('0'+i)), i, "app/notes/model.rb", "app/notes/view.rb"))
	}
	return out
}

func TestReachGathersTheNewestPRsOfQuietClusters(t *testing.T) {
	recent := []store.GatheredChange{ch("c40", 40, "app/notes/model.rb")}
	f := &fakeForge{}
	prs, changes, sum := Reach(recent, older(), f, map[int]bool{8: true})
	var got []int
	for _, p := range prs {
		got = append(got, p.Number)
	}
	if !reflect.DeepEqual(got, []int{9, 7, 6}) || len(changes) != 3 || changes[0].ID != "c9" {
		t.Fatalf("PRs %v, changes %+v", got, changes)
	}
	if p := prs[0]; p.Title != "Change c9" || !p.MergedAt.Equal(day.AddDate(0, 0, 9)) || p.Comments[0].Body != "Keep the ledger whole." {
		t.Errorf("PR %+v", p)
	}
	if sum != (Summary{Clusters: 1, PRs: 3}) {
		t.Errorf("summary %+v", sum)
	}
}

func TestReachStopsWhenTheForgeStopsAnswering(t *testing.T) {
	prs, _, sum := Reach(nil, older(), &fakeForge{stop: "c7"}, nil)
	if len(prs) != 2 || sum.Why != "rate limited" || sum.PRs != 2 {
		t.Errorf("PRs %+v, summary %+v", prs, sum)
	}
	if _, _, sum := Reach(nil, older(), nil, nil); sum != (Summary{}) {
		t.Errorf("no forge: %+v", sum)
	}
}

func TestAddKeepsRecentChangesAndAddsWhatReachFinds(t *testing.T) {
	g := store.Gather{Changes: append([]store.GatheredChange{ch("c40", 40, "app/notes/model.rb")}, older()...), PRs: []store.GatheredPR{{Number: 40}}}
	sum := Add(&g, day.AddDate(0, 0, 30), &fakeForge{})
	if len(g.Changes) != 4 || g.Changes[0].ID != "c40" || len(g.PRs) != 4 || sum.PRs != 3 {
		t.Errorf("changes %d, PRs %d, summary %+v", len(g.Changes), len(g.PRs), sum)
	}
}
