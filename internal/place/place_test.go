package place

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

// fake is a store holding what Run reads, counting what it writes.
type fake struct {
	gather      *store.Gather
	areas, walk *initrecord.Areas
	walkAt      time.Time
	put         []initrecord.Place
}

func (f *fake) Gathered() (store.Gather, bool, error) {
	if f.gather == nil {
		return store.Gather{}, false, nil
	}
	return *f.gather, true, nil
}

func (f *fake) Areas() (initrecord.Areas, bool, error) {
	if f.areas == nil {
		return initrecord.Areas{}, false, nil
	}
	return *f.areas, true, nil
}

func (f *fake) Walk() (initrecord.Walk, bool, error) {
	if f.walk == nil {
		return initrecord.Walk{}, false, nil
	}
	return initrecord.Walk{At: f.walkAt, Body: f.walk.Body}, true, nil
}

func (f *fake) PutPlace(p initrecord.Place) error {
	f.put = append(f.put, p)
	return nil
}

func body(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func change(id, subject string, files ...string) store.GatheredChange {
	return store.GatheredChange{ID: id, Short: id, Subject: subject, Files: files}
}

// sample is a walk of three capabilities and a gather that meets every
// outcome: PRs placed, a PR with no change, a PR no capability owns, held
// changes following each, and free changes placed, unowned and without files.
func sample() ([]walk.CapabilityRecord, store.Gather) {
	caps := []walk.CapabilityRecord{
		withTests(capOf("reading", "read/a.go", "read/b.go"), "read/a_test.go"),
		{Capability: walk.Capability{Name: "writing", Sources: []string{"write/a.go"}}, Area: "disk", Parent: "core"},
		capOf("idle", "idle/a.go"),
	}
	g := store.Gather{
		PRs: []store.GatheredPR{{Number: 12, Title: "Read faster"}, {Number: 13, Title: "No change names it"}, {Number: 14, Title: "Tidy CI"}, {Number: 15, Title: "Both"}},
		Changes: []store.GatheredChange{
			change("c1", "Read faster (#12)", "read/a.go", "read/a_test.go"),
			change("c2", "Merge pull request #12 from x/y", "read/b.go", "README.md"),
			change("c3", "Tidy CI (#14)", ".github/ci.yml"),
			change("c4", "Write straight to disk", "write/a.go"),
			change("c5", "Fix a typo", "README.md"),
			change("c6", "An empty change"),
			change("c7", "Backport (#99)", "write/a.go", "read/a.go"),
			change("c8", "Both (#15)", "write/a.go", "read/a.go"),
		},
	}
	return caps, g
}

func TestPlaceAll(t *testing.T) {
	caps, g := sample()
	rec, res := placeAll(g, splitOf([]string{"read/a_test.go"}, []string{"README.md"}, nil), caps)
	wantRes := Result{
		Capabilities: 3, PRsPlaced: 2,
		PRsNowhere:    []ReasonCount{{NoFiles, 1}, {NoOwner, 1}},
		ChangesPlaced: 2, ChangesWithPR: 3,
		ChangesNowhere: []ReasonCount{{NoFiles, 1}, {NoOwner, 2}},
		Without:        Without{1, []string{"idle"}},
		Broad:          []BroadEntry{}, WalkEntries: true, FilesWeighed: 5,
	}
	if !reflect.DeepEqual(res, wantRes) {
		t.Errorf("result = %+v\nwant     %+v", res, wantRes)
	}
	reading, tie := []Placement{{"reading", 3, 1}}, []Placement{{"reading", 1, 0.5}}
	wantRec := Record{
		PRs: []PRPlaced{{12, reading}, {15, tie}},
		Changes: []ChangePlaced{
			{"c1", 12, reading}, {"c2", 12, reading}, {"c4", 0, []Placement{{"writing", 1, 1}}}, {"c7", 0, tie}, {"c8", 15, tie},
		},
		Nowhere: Nowhere{
			PRs:     []PRNowhere{{13, NoFiles}, {14, NoOwner}},
			Changes: []ChangeNowhere{{"c3", 14, NoOwner}, {"c5", 0, NoOwner}, {"c6", 0, NoFiles}},
		},
		ByCapability: []CapabilityPlaced{
			{"reading", "core", "", []int{12, 15}, []string{"c7"}},
			{"writing", "disk", "core", []int{}, []string{"c4"}},
			{"idle", "core", "", []int{}, []string{}},
		},
		Broad: []BroadEntry{}, WalkEntries: true, FilesWeighed: 5,
	}
	if !reflect.DeepEqual(rec, wantRec) {
		t.Errorf("record = %+v\nwant     %+v", rec, wantRec)
	}
}

func TestNowhere(t *testing.T) {
	caps := []walk.CapabilityRecord{capOf("reading", "read/a.go")}
	cases := []struct {
		name    string
		g       store.Gather
		reason  string
		prs     int
		changes int
	}{
		{"a PR whose files no capability owns", store.Gather{PRs: []store.GatheredPR{{Number: 1}}, Changes: []store.GatheredChange{change("c1", "Tidy (#1)", "ci.yml")}}, NoOwner, 1, 1},
		{"a PR with no changes", store.Gather{PRs: []store.GatheredPR{{Number: 1}}}, NoFiles, 1, 0},
		{"a PR whose change names no file", store.Gather{PRs: []store.GatheredPR{{Number: 1}}, Changes: []store.GatheredChange{change("c1", "Empty (#1)")}}, NoFiles, 1, 1},
		{"a free change no capability owns", store.Gather{Changes: []store.GatheredChange{change("c1", "Tidy", "ci.yml")}}, NoOwner, 0, 1},
		{"a free change with no files", store.Gather{Changes: []store.GatheredChange{change("c1", "Empty")}}, NoFiles, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, res := placeAll(c.g, splitOf(nil, nil, nil), caps)
			for _, n := range append(append([]ReasonCount{}, res.PRsNowhere...), res.ChangesNowhere...) {
				if n.Reason != c.reason && n.Count != 0 {
					t.Errorf("%d counted under %q; want only %q", n.Count, n.Reason, c.reason)
				}
			}
			var prs, changes int
			for _, n := range res.PRsNowhere {
				prs += n.Count
			}
			for _, n := range res.ChangesNowhere {
				changes += n.Count
			}
			if prs != c.prs || changes != c.changes || res.PRsPlaced+res.ChangesPlaced+res.ChangesWithPR != 0 {
				t.Errorf("nowhere = %d PRs, %d changes, result %+v; want %d and %d, none placed", prs, changes, res, c.prs, c.changes)
			}
			for _, p := range rec.Nowhere.PRs {
				if p.Reason != c.reason {
					t.Errorf("PR #%d reason = %q; want %q", p.Number, p.Reason, c.reason)
				}
			}
			for _, ch := range rec.Nowhere.Changes {
				if ch.Reason != c.reason {
					t.Errorf("change %s reason = %q; want %q", ch.ID, ch.Reason, c.reason)
				}
			}
			if len(rec.Nowhere.PRs) != c.prs || len(rec.Nowhere.Changes) != c.changes || res.Without.Count != 1 {
				t.Errorf("record nowhere = %+v, without %+v", rec.Nowhere, res.Without)
			}
		})
	}
}

func TestHeldAndFreeChanges(t *testing.T) {
	caps := []walk.CapabilityRecord{capOf("reading", "read/a.go"), capOf("writing", "write/a.go", "write/b.go")}
	g := store.Gather{
		PRs: []store.GatheredPR{{Number: 7}},
		Changes: []store.GatheredChange{
			// Alone this change would go to reading; its PR's files put it on writing.
			change("held", "Part one (#7)", "read/a.go"),
			change("rest", "Part two (#7)", "write/a.go", "write/b.go"),
			change("free", "On its own", "read/a.go"),
			change("outside", "From an older PR (#3)", "read/a.go"),
		},
	}
	rec, res := placeAll(g, splitOf(nil, nil, nil), caps)
	writing := []Placement{{"writing", 2, 0.667}}
	want := []ChangePlaced{{"held", 7, writing}, {"rest", 7, writing}, {"free", 0, []Placement{{"reading", 1, 1}}}, {"outside", 0, []Placement{{"reading", 1, 1}}}}
	if !reflect.DeepEqual(rec.Changes, want) {
		t.Errorf("changes = %+v\nwant      %+v", rec.Changes, want)
	}
	if res.ChangesWithPR != 2 || res.ChangesPlaced != 2 || res.PRsPlaced != 1 || res.Without.Count != 0 {
		t.Errorf("result = %+v; want 2 with their PR, 2 on their own, 1 PR", res)
	}
	if got := rec.ByCapability[0]; !reflect.DeepEqual(got.Changes, []string{"free", "outside"}) || len(got.PRs) != 0 {
		t.Errorf("reading holds %+v; want the free changes only", got)
	}
}

func TestOldWalkPlacesByStoredFiles(t *testing.T) {
	old := capOf("reading", "read/a.go")
	old.Tests = []string{"read/a_test.go"}
	g := store.Gather{Changes: []store.GatheredChange{change("c1", "Test it", "read/a_test.go")}}
	rec, res := placeAll(g, splitOf(old.Tests, nil, nil), []walk.CapabilityRecord{old})
	if res.WalkEntries || rec.WalkEntries || res.ChangesPlaced != 1 || res.FilesWeighed != 2 {
		t.Errorf("result = %+v; want the change placed by the stored test, entries false", res)
	}
	var out bytes.Buffer
	if err := Text(&out, res); err != nil || !strings.Contains(out.String(), "kept no footprint entries") {
		t.Errorf("text does not say the walk kept no entries (%v):\n%s", err, out.String())
	}
}

// stored is a fake store holding the sample as the phases store it.
func stored(t *testing.T) *fake {
	t.Helper()
	caps, g := sample()
	split := splitOf([]string{"read/a_test.go"}, []string{"README.md"}, nil)
	return &fake{
		gather: &g,
		areas:  &initrecord.Areas{Body: body(t, areas.Record{Split: split})},
		walk:   &initrecord.Areas{Body: body(t, walk.Record{Capabilities: caps})},
		walkAt: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC),
	}
}

func TestRunStoresThePlacement(t *testing.T) {
	st := stored(t)
	res, err := Run(st)
	if err != nil {
		t.Fatal(err)
	}
	if res.PRsPlaced != 2 || res.ChangesPlaced != 2 || res.ChangesWithPR != 3 {
		t.Errorf("result = %+v", res)
	}
	if len(st.put) != 1 || !st.put[0].WalkAt.Equal(st.walkAt) || st.put[0].At.Before(st.walkAt) {
		t.Fatalf("stored = %+v; want one placement from the walk at %v", st.put, st.walkAt)
	}
	var rec Record
	if err := json.Unmarshal(st.put[0].Body, &rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.PRs) != 2 || rec.PRs[0].Number != 12 || len(rec.Nowhere.PRs) != 2 || len(rec.ByCapability) != 3 {
		t.Errorf("stored record = %+v", rec)
	}
	for _, key := range []string{`"prs_placed":2`, `"prs_nowhere":[{"reason":"no files known from the gather","count":1}`, `"changes_placed_with_pr":3`, `"capabilities_without":{"count":1,"names":["idle"]}`, `"broad_entries":[]`, `"files_weighed":5`} {
		if got := string(body(t, res)); !strings.Contains(got, key) {
			t.Errorf("result JSON lacks %s:\n%s", key, got)
		}
	}
}

func TestRunNeedsTheEarlierPhases(t *testing.T) {
	cases := []struct {
		name string
		drop func(*fake)
		want string
	}{
		{"no walk", func(f *fake) { f.walk = nil }, "no walk stored yet; run physa init --walk first"},
		{"no walk and no gather", func(f *fake) { f.walk, f.gather = nil, nil }, "no walk stored yet; run physa init --walk first"},
		{"no gather", func(f *fake) { f.gather = nil }, "no gather stored yet; run physa init --gather first"},
		{"no areas", func(f *fake) { f.areas = nil }, "no areas stored yet; run physa init --areas first"},
		{"a walk that does not decode", func(f *fake) { f.walk = &initrecord.Areas{Body: []byte("{")} }, "the stored walk does not decode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := stored(t)
			c.drop(st)
			_, err := Run(st)
			if err == nil || !strings.HasPrefix(err.Error(), c.want) {
				t.Errorf("err = %v; want %q", err, c.want)
			}
			if len(st.put) != 0 {
				t.Errorf("stored %d placements; want none", len(st.put))
			}
		})
	}
}

// TestRunOnInitsRecord runs against the real store: the record the phases
// write is the record Run reads, and Load reads back what it stored.
func TestRunOnInitsRecord(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := initrecord.Of(db)
	if _, ok, err := Load(st); ok || err != nil {
		t.Fatalf("Load on a fresh store = %v, %v; want false, nil", ok, err)
	}
	f := stored(t)
	t0 := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	f.gather.Run = store.GatherRun{At: t0, Since: t0.AddDate(-1, 0, 0)}
	if err := st.PutGather(*f.gather); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(st); err == nil || !strings.Contains(err.Error(), "--walk first") {
		t.Fatalf("err = %v; want the walk asked for first", err)
	}
	if err := st.PutAreas(initrecord.Areas{GatherAt: t0, At: t0, Body: f.areas.Body}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutWalk(initrecord.Walk{AreasAt: t0, At: f.walkAt, Body: f.walk.Body}); err != nil {
		t.Fatal(err)
	}
	res, err := Run(st)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok, err := Load(st)
	if err != nil || !ok {
		t.Fatalf("Load = %v, %v", ok, err)
	}
	if len(rec.PRs) != res.PRsPlaced || len(rec.Changes) != res.ChangesPlaced+res.ChangesWithPR || rec.FilesWeighed != 5 {
		t.Errorf("record %+v does not match result %+v", rec, res)
	}
	p, _, _ := st.Place()
	if !p.WalkAt.Equal(f.walkAt) {
		t.Errorf("walk_at = %v; want %v", p.WalkAt, f.walkAt)
	}
}

func TestText(t *testing.T) {
	res := Result{
		Capabilities: 3, PRsPlaced: 2, PRsNowhere: []ReasonCount{{NoFiles, 1}, {NoOwner, 4}},
		ChangesPlaced: 5, ChangesWithPR: 6, ChangesNowhere: []ReasonCount{{NoFiles, 0}, {NoOwner, 7}},
		Without: Without{2, []string{"idle", "spare"}}, Broad: []BroadEntry{{"reading", "test", "test/", 41}},
		WalkEntries: true, FilesWeighed: 90,
	}
	var out bytes.Buffer
	if err := Text(&out, res); err != nil {
		t.Fatal(err)
	}
	want := `Placed the gathered PRs and changes on the capabilities (3) by their footprints, with no session.
PRs placed: 2
PRs placed nowhere, no files known from the gather: 1
PRs placed nowhere, none of its files belongs to a capability: 4
Changes placed on their own: 5
Changes placed with their PR: 6
Changes placed nowhere, no files known from the gather: 0
Changes placed nowhere, none of its files belongs to a capability: 7
Capabilities with no PR or change: 2 (idle, spare)
Broad entries not used to place (each names more than 40 files): 1
- reading: the test entry test/ names 41 files
Files weighed: 90
`
	if out.String() != want {
		t.Errorf("text =\n%s\nwant\n%s", out.String(), want)
	}
}

// TestNoAgentImport reads the imports of the package's own files: placement
// makes no agent call, so none may import the adapter. The packages it reads
// records from run sessions themselves, so only its own imports are checked.
func TestNoAgentImport(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no files to read: %v", err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, im := range f.Imports {
			path := strings.Trim(im.Path.Value, `"`)
			if strings.Contains(path, "internal/adapter") || path == "os/exec" {
				t.Errorf("%s imports %s; placement makes no agent call", name, path)
			}
		}
	}
	if checked < 4 {
		t.Errorf("read %d files; want the package's four", checked)
	}
}
