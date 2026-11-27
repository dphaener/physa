package place

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/walk"
)

// capOf is a capability owning sources, in the area "core".
func capOf(name string, sources ...string) walk.CapabilityRecord {
	return walk.CapabilityRecord{Capability: walk.Capability{Name: name, Sources: sources}, Area: "core"}
}

// withTests gives c test entries as a session wrote them.
func withTests(c walk.CapabilityRecord, entries ...string) walk.CapabilityRecord {
	c.Entries.Tests = entries
	return c
}

// splitOf is a split holding tests, docs and other files.
func splitOf(tests, docs, other []string) sourceset.Division {
	return sourceset.Division{Files: map[sourceset.Kind][]string{sourceset.Test: tests, sourceset.Doc: docs, sourceset.Other: other}}
}

// numbered is n file paths under dir.
func numbered(dir string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s/f%02d_test.go", dir, i)
	}
	return out
}

// weightOf is file's weight for each capability, as floats.
func weightOf(w weights, file string) map[string]float64 {
	out := map[string]float64{}
	for name, x := range w.of[file] {
		out[name], _ = x.Float64()
	}
	return out
}

func TestWeigh(t *testing.T) {
	both := []string{"a/a_test.go", "t/shared_test.go"}
	cases := []struct {
		name  string
		caps  []walk.CapabilityRecord
		split sourceset.Division
		file  string
		want  map[string]float64
	}{
		{"a source weighs 1 for its owner alone",
			[]walk.CapabilityRecord{capOf("a", "a/a.go"), capOf("b", "b/b.go")}, splitOf(nil, nil, nil),
			"a/a.go", map[string]float64{"a": 1}},
		{"a test shared by two weighs a half for each",
			[]walk.CapabilityRecord{withTests(capOf("a", "a/a.go"), "t/shared_test.go"), withTests(capOf("b", "b/b.go"), "t/")}, splitOf(both, nil, nil),
			"t/shared_test.go", map[string]float64{"a": 0.5, "b": 0.5}},
		{"a test only one names weighs 1 for it",
			[]walk.CapabilityRecord{withTests(capOf("a", "a/a.go"), "a/a_test.go"), withTests(capOf("b", "b/b.go"), "t/")}, splitOf(both, nil, nil),
			"a/a_test.go", map[string]float64{"a": 1}},
		{"a test that is also someone's source is not shared",
			[]walk.CapabilityRecord{capOf("a", "a/a.go", "t/shared_test.go"), withTests(capOf("b", "b/b.go"), "t/shared_test.go"), withTests(capOf("c", "c/c.go"), "t/")}, splitOf(both, nil, nil),
			"t/shared_test.go", map[string]float64{"a": 1}},
		{"a file named by two entries of one capability is claimed once",
			[]walk.CapabilityRecord{withTests(capOf("a", "a/a.go"), "t/", "t/shared_test.go"), withTests(capOf("b", "b/b.go"), "t/shared_test.go")}, splitOf(both, nil, nil),
			"t/shared_test.go", map[string]float64{"a": 0.5, "b": 0.5}},
		{"a file no footprint names weighs nothing",
			[]walk.CapabilityRecord{capOf("a", "a/a.go")}, splitOf(both, nil, nil),
			"t/shared_test.go", map[string]float64{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := weigh(c.caps, c.split)
			if got := weightOf(w, c.file); !reflect.DeepEqual(got, c.want) {
				t.Errorf("weight of %s = %v; want %v", c.file, got, c.want)
			}
			if !w.entries && len(c.caps[0].Entries.Tests) > 0 {
				t.Error("entries = false for a walk that kept them")
			}
		})
	}
}

func TestWeighKinds(t *testing.T) {
	a := capOf("a", "a/a.go")
	a.Entries = walk.Entries{Tests: []string{"a/a_test.go"}, Docs: []string{"docs/"}, Other: []string{"a/data.json"}}
	b := capOf("b", "b/b.go")
	b.Entries = walk.Entries{Docs: []string{"docs/guide.md"}, Other: []string{"a/data.json"}}
	// An entry is matched against the files of its own kind only: the doc
	// entry "docs/" never claims the test under docs.
	w := weigh([]walk.CapabilityRecord{a, b}, splitOf([]string{"a/a_test.go", "docs/doc_test.go"}, []string{"docs/guide.md", "docs/more.md"}, []string{"a/data.json"}))
	want := map[string]map[string]float64{
		"a/a_test.go": {"a": 1}, "docs/guide.md": {"a": 0.5, "b": 0.5}, "docs/more.md": {"a": 1},
		"a/data.json": {"a": 0.5, "b": 0.5}, "docs/doc_test.go": {},
	}
	for f, x := range want {
		if got := weightOf(w, f); !reflect.DeepEqual(got, x) {
			t.Errorf("weight of %s = %v; want %v", f, got, x)
		}
	}
	if len(w.of) != 6 {
		t.Errorf("files weighed = %d; want 6 (two sources, a test, two docs, one other)", len(w.of))
	}
}

func TestWeighBroad(t *testing.T) {
	wide, edge := numbered("wide", Broad+1), numbered("edge", Broad)
	caps := []walk.CapabilityRecord{withTests(capOf("a", "a/a.go"), "wide/", "edge/"), withTests(capOf("b", "b/b.go"), "wide/f00_test.go")}
	w := weigh(caps, splitOf(append(append([]string{}, wide...), edge...), nil, nil))
	if want := []BroadEntry{{"a", "test", "wide/", Broad + 1}}; !reflect.DeepEqual(w.broad, want) {
		t.Errorf("broad = %+v; want %+v", w.broad, want)
	}
	if got := weightOf(w, "wide/f01_test.go"); len(got) != 0 {
		t.Errorf("a file only the broad entry names weighs %v; want nothing", got)
	}
	if got := weightOf(w, "wide/f00_test.go"); !reflect.DeepEqual(got, map[string]float64{"b": 1}) {
		t.Errorf("the broad entry shared b's own test: %v; want b alone", got)
	}
	for _, f := range edge {
		if got := weightOf(w, f); !reflect.DeepEqual(got, map[string]float64{"a": 1}) {
			t.Fatalf("an entry naming %d files was not used: %s weighs %v", Broad, f, got)
		}
	}
}

func TestWeighOldWalkWithoutEntries(t *testing.T) {
	a, b := capOf("a", "a/a.go"), capOf("b", "b/b.go")
	// An old walk kept only the expanded files. "README.md" as a pattern
	// would name every README; as a stored file it is that file alone.
	a.Tests, a.Docs = numbered("wide", Broad+1), []string{"README.md"}
	b.Tests = []string{"wide/f00_test.go"}
	w := weigh([]walk.CapabilityRecord{a, b}, splitOf(a.Tests, []string{"README.md", "sub/README.md"}, nil))
	if w.entries {
		t.Error("entries = true for a walk stored without them")
	}
	if len(w.broad) != 0 {
		t.Errorf("broad = %+v; want none, a stored file is its own entry", w.broad)
	}
	if got := weightOf(w, "wide/f00_test.go"); !reflect.DeepEqual(got, map[string]float64{"a": 0.5, "b": 0.5}) {
		t.Errorf("shared stored test weighs %v; want a half each", got)
	}
	if got := weightOf(w, "wide/f40_test.go"); !reflect.DeepEqual(got, map[string]float64{"a": 1}) {
		t.Errorf("stored test weighs %v; want a alone", got)
	}
	if got := weightOf(w, "sub/README.md"); len(got) != 0 {
		t.Errorf("sub/README.md weighs %v; want nothing", got)
	}
	if none := weigh([]walk.CapabilityRecord{capOf("a", "a/a.go")}, splitOf(nil, nil, nil)); !none.entries {
		t.Error("entries = false for a walk that names no test, doc or other file at all")
	}
}

// owning builds one capability per weight, named by names, each owning that
// many source files, and the files of an item touching them all.
func owning(names []string, counts []int) ([]walk.CapabilityRecord, []string) {
	var caps []walk.CapabilityRecord
	var files []string
	for i, name := range names {
		var own []string
		for j := 0; j < counts[i]; j++ {
			own = append(own, fmt.Sprintf("%s/%d.go", name, j))
		}
		caps, files = append(caps, capOf(name, own...)), append(files, own...)
	}
	return caps, files
}

func TestTake(t *testing.T) {
	cases := []struct {
		name   string
		names  []string
		counts []int
		want   []Placement
	}{
		{"the largest alone covers half", []string{"a", "b", "c"}, []int{6, 3, 1}, []Placement{{"a", 6, 0.6}}},
		{"two are needed to cover half", []string{"a", "b", "c"}, []int{4, 4, 2}, []Placement{{"a", 4, 0.4}, {"b", 4, 0.4}}},
		// 3 and 3 of 10 already cover half, so the rule stops at two.
		{"it stops once half is covered", []string{"a", "b", "c", "d"}, []int{3, 3, 3, 1}, []Placement{{"a", 3, 0.3}, {"b", 3, 0.3}}},
		{"three when two fall short of half", []string{"a", "b", "c", "d", "e"}, []int{2, 2, 2, 2, 2}, []Placement{{"a", 2, 0.2}, {"b", 2, 0.2}, {"c", 2, 0.2}}},
		// Three of seven is under half, and a fourth is still never taken.
		{"at most three", []string{"a", "b", "c", "d", "e", "f", "g"}, []int{1, 1, 1, 1, 1, 1, 1}, []Placement{{"a", 1, 0.143}, {"b", 1, 0.143}, {"c", 1, 0.143}}},
		{"a tie goes to the name", []string{"zed", "mid", "abe"}, []int{2, 2, 1}, []Placement{{"mid", 2, 0.4}, {"zed", 2, 0.4}}},
		{"exactly half is covered", []string{"b", "a"}, []int{2, 2}, []Placement{{"a", 2, 0.5}}},
		{"weight ranks before name", []string{"a", "z"}, []int{1, 3}, []Placement{{"z", 3, 0.75}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			caps, files := owning(c.names, c.counts)
			if got := take(files, weigh(caps, splitOf(nil, nil, nil))); !reflect.DeepEqual(got, c.want) {
				t.Errorf("take = %+v; want %+v", got, c.want)
			}
		})
	}
}

func TestTakeSharedWeightsAreExact(t *testing.T) {
	// Three tests each shared by a, b and c: a third each, three times, is 1
	// for each, a three-way tie with d's one source that names must settle.
	shared := []string{"t/1_test.go", "t/2_test.go", "t/3_test.go"}
	caps := []walk.CapabilityRecord{capOf("d", "d/d.go"), withTests(capOf("c", "c/c.go"), "t/"), withTests(capOf("b", "b/b.go"), "t/"), withTests(capOf("a", "a/a.go"), "t/")}
	w := weigh(caps, splitOf(shared, nil, nil))
	got := take(append([]string{"d/d.go", "t/1_test.go"}, shared...), w)
	want := []Placement{{"a", 1, 0.25}, {"b", 1, 0.25}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("take = %+v; want %+v (a file named twice counts once)", got, want)
	}
	if got := take([]string{"t/1_test.go"}, w); !reflect.DeepEqual(got, []Placement{{"a", 0.333, 0.333}, {"b", 0.333, 0.333}}) {
		t.Errorf("take = %+v; want a and b at a third each, rounded to three places", got)
	}
	if got := take([]string{"nobody.go"}, w); got != nil {
		t.Errorf("take = %+v; want nothing for a file no capability weighs", got)
	}
}
