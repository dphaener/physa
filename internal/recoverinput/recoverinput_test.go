package recoverinput

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

const (
	idA = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	idB = "b1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	idC = "c1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
)

var gatherAt = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

func testGather() store.Gather {
	return store.Gather{
		Run:  store.GatherRun{At: gatherAt},
		Docs: []store.Doc{{Path: "README.md", Kind: "readme", Body: "# Notes\n\nPeople keep their notes.\n"}},
		Changes: []store.GatheredChange{
			{ID: idA, Short: "a1b2c3d", Subject: "feat(export): Export every note", At: gatherAt.Add(-3 * time.Hour)},
			{ID: idB, Short: "b1b2c3d", Subject: "Revert \"feat(export): Export every note\"", At: gatherAt.Add(-2 * time.Hour), Reverts: idA},
			{ID: idC, Short: "c1b2c3d", Subject: "feat(search): Rank by date", At: gatherAt.Add(-time.Hour)},
		},
		PRs: []store.GatheredPR{
			{Number: 12, Title: "Export every note", MergedAt: gatherAt.Add(-3 * time.Hour)},
			{Number: 7, Title: "Search by word", MergedAt: gatherAt.Add(-5 * time.Hour)},
		},
	}
}

func changeIDs(g store.Gather) []string {
	var ids []string
	for _, c := range g.Changes {
		ids = append(ids, c.ID)
	}
	return ids
}

// TestPlacedSourcesHoldTheChangesARevertLinks checks that a capability's
// sources hold, beside the PRs and changes placed with it, the change a
// placed change reverts and the change that reverts a placed one, and
// nothing placed with another capability.
func TestPlacedSourcesHoldTheChangesARevertLinks(t *testing.T) {
	g := testGather()
	got := placed(g, place.CapabilityPlaced{Capability: "export", Changes: []string{"b1b2c3d"}})
	// b1b2c3d is placed; a1b2c3d comes with it as the change it reverts.
	if ids := changeIDs(got); !slices.Equal(ids, []string{idA, idB}) || len(got.PRs) != 0 {
		t.Errorf("changes %v, PRs %+v", ids, got.PRs)
	}
	// And the other way: the revert comes with the change it reverts.
	got = placed(g, place.CapabilityPlaced{Capability: "export", PRs: []int{12}, Changes: []string{idA}})
	if ids := changeIDs(got); !slices.Equal(ids, []string{idA, idB}) || len(got.PRs) != 1 || got.PRs[0].Number != 12 {
		t.Errorf("changes %v, PRs %+v", ids, got.PRs)
	}
	if len(got.Docs) != 0 || !got.Run.At.Equal(gatherAt) {
		t.Errorf("docs %v, run %+v", got.Docs, got.Run)
	}
}

// writeRepo makes a repo root holding files, each with the given number of
// lines.
func writeRepo(t *testing.T, files map[string]int) string {
	t.Helper()
	root := t.TempDir()
	for p, n := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		body := ""
		for i := 0; i < n; i++ {
			body += fmt.Sprintf("line %d\n", i)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testWalk() walk.Record {
	return walk.Record{Capabilities: []walk.CapabilityRecord{
		{Capability: walk.Capability{Name: "export", Promise: "Notes leave the product.",
			Sources: []string{"internal/export/export.go", "internal/export/zip.go"},
			Tests:   []string{"internal/export/export_test.go", "internal/export/zip_test.go"},
			Docs:    []string{"docs/export.md"}, Other: []string{"testdata/export/a.json"}},
			Area: "export", Entries: walk.Entries{Tests: []string{"internal/export/"}, Docs: []string{"docs/export.md"}, Other: []string{"testdata/export/"}}},
		{Capability: walk.Capability{Name: "search", Promise: "A note is found by any word.",
			Sources: []string{"internal/search/search.go"}, Tests: []string{"internal/search/search_test.go"}},
			Area: "search"},
	}}
}

func TestWalkedCarriesFootprintAndPlacedSources(t *testing.T) {
	root := writeRepo(t, map[string]int{"internal/export/export.go": 40, "internal/export/zip.go": 1, "internal/search/search.go": 12})
	p := place.Record{ByCapability: []place.CapabilityPlaced{
		{Capability: "export", Area: "export", PRs: []int{12}, Changes: []string{idA}},
		{Capability: "search", Area: "search", PRs: []int{7}, Changes: []string{"c1b2c3d"}},
	}}
	got := Walk(root, testWalk(), p, testGather())
	if len(got) != 2 || got[0].Name != "export" || got[1].Name != "search" {
		t.Fatalf("walked %+v", got)
	}
	ex := got[0]
	want := recovering.Footprint{
		Sources: []recovering.SourceFile{{Path: "internal/export/export.go", Lines: 40}, {Path: "internal/export/zip.go", Lines: 1}},
		Tests:   []string{"internal/export/"}, Docs: []string{"docs/export.md"}, Other: []string{"testdata/export/"},
	}
	if !reflect.DeepEqual(ex.Footprint, want) || ex.Area != "export" || ex.Promise != "Notes leave the product." {
		t.Errorf("export = %+v", ex)
	}
	if len(ex.Sources.PRs) != 1 || ex.Sources.PRs[0].Number != 12 {
		t.Errorf("export PRs %+v", ex.Sources.PRs)
	}
	// idA is placed by its full id; idB, its revert, comes with it.
	if ids := changeIDs(ex.Sources); !slices.Equal(ids, []string{idA, idB}) {
		t.Errorf("export changes %v", ids)
	}
	if len(ex.Sources.Docs) != 0 || !ex.Sources.Run.At.Equal(gatherAt) {
		t.Errorf("export docs %v, run %+v", ex.Sources.Docs, ex.Sources.Run)
	}
	se := got[1]
	// A walk without entries gives its expanded lists; idC is placed by its short id.
	if !slices.Equal(se.Footprint.Tests, []string{"internal/search/search_test.go"}) || se.Footprint.Sources[0].Lines != 12 {
		t.Errorf("search footprint %+v", se.Footprint)
	}
	if ids := changeIDs(se.Sources); len(se.Sources.PRs) != 1 || se.Sources.PRs[0].Number != 7 || !slices.Equal(ids, []string{idC}) {
		t.Errorf("search PRs %+v, changes %v", se.Sources.PRs, ids)
	}
}

// TestWalkedWithoutPlacementOrCount checks that a capability with nothing
// placed and a source file with no count still give an input.
func TestWalkedWithoutPlacementOrCount(t *testing.T) {
	root := writeRepo(t, map[string]int{"internal/export/export.go": 40})
	// zip.go and search.go are not in the repo, so they have no count.
	got := Walk(root, testWalk(), place.Record{}, testGather())
	if len(got) != 2 {
		t.Fatalf("walked %+v", got)
	}
	for _, w := range got {
		if len(w.Sources.PRs) != 0 || len(w.Sources.Changes) != 0 || len(w.Sources.Docs) != 0 {
			t.Errorf("%s: sources %+v, want none", w.Name, w.Sources)
		}
	}
	src := got[0].Footprint.Sources
	if len(src) != 2 || src[0].Lines != 40 || src[1] != (recovering.SourceFile{Path: "internal/export/zip.go"}) {
		t.Errorf("export sources %+v", src)
	}
	if s := got[1].Footprint.Sources; len(s) != 1 || s[0].Lines != 0 {
		t.Errorf("search sources %+v", s)
	}
	t.Logf("no placement: %d inputs; export sources %+v; search sources %+v", len(got), src, got[1].Footprint.Sources)
}

func TestSeedIsTheGathersTime(t *testing.T) {
	g := testGather()
	if Seed(g) != uint64(gatherAt.UnixNano()) || Seed(g) != Seed(testGather()) {
		t.Errorf("seed %d", Seed(g))
	}
	g.Run.At = gatherAt.Add(time.Second)
	if Seed(g) == Seed(testGather()) {
		t.Error("a new gather gave the same seed")
	}
}

// manyCaps is n capabilities in each of the areas named, one of them a
// part split from another area.
func manyCaps(areas []string, n int) []Walked {
	var caps []Walked
	for _, a := range areas {
		for i := 0; i < n; i++ {
			w := Walked{Name: fmt.Sprintf("%s-%d", a, i), Area: a}
			if a == "billing" {
				w.Parent = "accounts"
			}
			caps = append(caps, w)
		}
	}
	return caps
}

func names(ws []Walked) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Name)
	}
	return out
}

var areaNames = []string{"export", "search", "sharing", "sync", "billing", "tags"}

func TestOrderIsTheSameForTheSameSeed(t *testing.T) {
	caps := manyCaps(areaNames, 5)
	first := names(Order(caps, 42))
	if again := names(Order(caps, 42)); !slices.Equal(first, again) {
		t.Errorf("same seed, another order:\n%v\n%v", first, again)
	}
	rev := slices.Clone(caps)
	slices.Reverse(rev)
	if got := names(Order(rev, 42)); !slices.Equal(first, got) {
		t.Errorf("same seed, input reversed, another order:\n%v\n%v", first, got)
	}
	if names(caps)[0] != "export-0" {
		t.Error("Order changed its input")
	}
	t.Logf("seed 42: %v", first)
}

func TestOrderDiffersForAnotherSeed(t *testing.T) {
	caps := manyCaps(areaNames, 5)
	a, b := names(Order(caps, 42)), names(Order(caps, 43))
	if slices.Equal(a, b) {
		t.Errorf("seeds 42 and 43 gave one order: %v", a)
	}
	t.Logf("seed 43: %v", b)
}

func TestOrderSpreadsAcrossAreas(t *testing.T) {
	// Uneven areas: the first round holds one of each, and the rest follow.
	var caps []Walked
	for i, a := range areaNames {
		caps = append(caps, manyCaps([]string{a}, i+1)...)
	}
	for _, seed := range []uint64{0, 1, 42, Seed(testGather()), 1<<64 - 1} {
		got := Order(caps, seed)
		if len(got) != len(caps) {
			t.Fatalf("seed %d: %d of %d", seed, len(got), len(caps))
		}
		seen := map[string]bool{}
		for _, w := range got {
			if seen[w.Name] {
				t.Errorf("seed %d: %s twice", seed, w.Name)
			}
			seen[w.Name] = true
		}
		first := map[string]bool{}
		for _, w := range got[:len(areaNames)] {
			first[w.Parent+"/"+w.Area] = true
		}
		if len(first) != len(areaNames) {
			t.Errorf("seed %d: the first %d come from %d areas: %v", seed, len(areaNames), len(first), names(got[:len(areaNames)]))
		}
	}
}

// TestOrderKeysAreasByTheirParent checks that two parts with one name, split
// from different areas, are different areas.
func TestOrderKeysAreasByTheirParent(t *testing.T) {
	caps := []Walked{
		{Name: "a", Area: "core", Parent: "server"}, {Name: "b", Area: "core", Parent: "server"},
		{Name: "c", Area: "core", Parent: "client"}, {Name: "d", Area: "core", Parent: "client"},
	}
	got := Order(caps, 7)
	if got[0].Parent == got[1].Parent || got[2].Parent == got[3].Parent {
		t.Errorf("order %+v does not alternate the two areas", got)
	}
}
