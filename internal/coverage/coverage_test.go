package coverage

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/store"
)

// change is a gathered change with id, subject and files.
func change(id, subject string, files ...string) store.GatheredChange {
	return store.GatheredChange{ID: id + strings.Repeat("0", 40-len(id)), Short: id, Subject: subject, Files: files}
}

// garden is a made-up gather: notes and billing each change together often
// enough to cluster, and a reports area no capability cites.
func garden() store.Gather {
	var g store.Gather
	for i, area := range []string{"notes", "notes", "notes", "billing", "billing", "billing", "reports", "reports", "reports"} {
		id := string(rune('a'+i)) + "1"
		g.Changes = append(g.Changes, change(id, "Touch "+area+" ("+"#"+string(rune('1'+i))+")", "app/"+area+"/model.rb", "app/"+area+"/view.rb"))
	}
	g.Changes = append(g.Changes, change("j1", "Tidy notes", "app/notes/helper.rb"), change("k1", "Bump the build", "Makefile"))
	for n := 1; n <= 9; n++ {
		g.PRs = append(g.PRs, store.GatheredPR{Number: n})
	}
	g.PRs = append(g.PRs, store.GatheredPR{Number: 40}) // no change names it
	return g
}

func caps() []mapping.Capability {
	return []mapping.Capability{
		{Name: "notes", ShownBy: []string{"#1"}},
		{Name: "billing", ShownBy: []string{"d1"}},
	}
}

func TestPROf(t *testing.T) {
	for subject, want := range map[string]int{
		"Add export (#12)":                   12,
		"Merge pull request #7 from a/b":     7,
		"Refer to #3 in passing":             0,
		"Add export (#12) and then trailing": 0,
	} {
		if got := PROf(store.GatheredChange{Subject: subject}); got != want {
			t.Errorf("PROf(%q) = %d, want %d", subject, got, want)
		}
	}
}

func TestPlaceByFilesThenDirectories(t *testing.T) {
	pl, rep := Place(garden(), caps())
	if got, want := pl.PRs, map[string][]int{"notes": {2, 3}, "billing": {4, 5, 6}}; !reflect.DeepEqual(got, want) {
		t.Errorf("PRs placed %v, want %v", got, want)
	}
	// #4 is d1's PR, so billing gets it. j1 touches no cited file but
	// shares notes' directory; k1 and the reports changes reach no one; d1
	// is cited, so it is not placed.
	ids := func(name string) []string {
		var out []string
		for _, id := range pl.Changes[name] {
			out = append(out, id[:2])
		}
		return out
	}
	if got := ids("notes"); !reflect.DeepEqual(got, []string{"a1", "b1", "c1", "j1"}) {
		t.Errorf("notes' changes %v", got)
	}
	if got := ids("billing"); !reflect.DeepEqual(got, []string{"e1", "f1"}) {
		t.Errorf("billing's changes %v", got)
	}
	want := Report{PRs: 10, Cited: 1, Placed: 5, Unplaced: 4, Unfiled: 1, Clusters: 3, Low: true}
	if len(rep.Uncovered) != 1 || rep.Uncovered[0].Paths[0] != "app/reports/model.rb" {
		t.Fatalf("uncovered %+v", rep.Uncovered)
	}
	rep.Uncovered = nil
	if !reflect.DeepEqual(rep, want) {
		t.Errorf("report %+v, want %+v", rep, want)
	}
}

func TestPlaceWithFullCoverageIsNotLow(t *testing.T) {
	g := garden()
	g.Changes, g.PRs = g.Changes[:6], g.PRs[:6]
	if _, rep := Place(g, caps()); rep.Low || rep.Unplaced != 0 || len(rep.Uncovered) != 0 {
		t.Errorf("report %+v", rep)
	}
}

func TestTextAndWarnSayWhenCoverageIsLow(t *testing.T) {
	_, rep := Place(garden(), caps())
	b := bytes.NewBufferString(Text(rep))
	for _, want := range []string{
		"Coverage: the capabilities cite 1 of 10 gathered PRs; 5 more are placed with the nearest by their files, so recovery reads them, and 4 reach none (1 with no change naming them, so no files to place them by).",
		"Clusters: 1 of 3 touch no capability.\n  app/reports/model.rb, app/reports/view.rb (3 changes)\n",
		"Coverage is low:",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, b.String())
		}
	}
	var w bytes.Buffer
	if Warn(&w, rep); !strings.HasPrefix(w.String(), "physa init: warning: coverage is low: 4 of 10 gathered PRs and 1 of 3 clusters") {
		t.Errorf("Warn wrote %q", w.String())
	}
	w.Reset()
	if Warn(&w, Report{PRs: 1, Cited: 1}); w.Len() != 0 {
		t.Errorf("Warn on good coverage wrote %q", w.String())
	}
}
