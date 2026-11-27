package consolidating

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/recovering"
)

// many is n rule texts, headings and slugs from prefix, each anchored to
// anchor.
func many(prefix string, n int, anchor string) []string {
	var out []string
	for i := range n {
		out = append(out, entry(fmt.Sprintf("Rule %s %d", prefix, i), fmt.Sprintf("%s-%d", prefix, i), anchor, fmt.Sprintf("Body %s %d.", prefix, i), "reason."))
	}
	return out
}

// pair is two capabilities in different areas, both about invites.
func pair(t *testing.T) (capabilitytext.Capability, capabilitytext.Capability) {
	a := parse(t, "invites", areaA, text("invites", "People invite their team.", "Invites grow a team.",
		[]string{entry("Invites expire", "invite-expiry", "#12", "An invite lasts a week.", "stale links leak access.")},
		[]string{"### We email invites\n`invite-email` · decided by dphaener, in their words · 2026-01-02 · #3\nInvites go by email.\nBecause: everyone has one.\n"},
		nil), "**Invite**: an offer to join.\n")
	a.Footprint = recovering.Footprint{Sources: []recovering.SourceFile{{Path: "app/invites.go", Lines: 10}}, Tests: []string{"app/invites_test.go"}}
	b := parse(t, "invites-2", areaBPart, text("invites-2", "A team grows by invitation.", "The second overview.",
		[]string{entry("Invite links are single use", "invite-once", "#13 abc1234", "A link works once.", "links get forwarded.")},
		nil,
		[]string{"### Should invites renew?\n`invite-renew` · raised by dphaener · 2026-01-02\nNobody has decided.\n"}), "**Seat**: a paid place.\n")
	b.Footprint = recovering.Footprint{Sources: []recovering.SourceFile{{Path: "app/invites.go", Lines: 10}, {Path: "app/links.go", Lines: 5}}, Docs: []string{"docs/a.md"}}
	return a, b
}

func TestMergeKeepsEveryEntryAnchorAndTerm(t *testing.T) {
	a, b := pair(t)
	s := New("Product", []capabilitytext.Capability{a, b}, nil)
	if !s.Merge([]string{"invites", "invites-2"}, "invites", "invites-2", normal) {
		t.Fatalf("refused: %+v", s.Record().Refused)
	}
	out := s.Output()
	if len(out.Capabilities) != 1 {
		t.Fatalf("capabilities %d, want 1", len(out.Capabilities))
	}
	m := out.Capabilities[0]
	want := []string{"invites/invite-expiry", "invites/invite-once", "invites/invite-email", "invites/invite-renew"}
	if got := ids(m); !slices.Equal(got, want) {
		t.Errorf("ids %v, want %v", got, want)
	}
	for _, e := range append(a.Entries(), b.Entries()...) {
		got := m.Entry(e.Slug)
		if got == nil || !slices.Equal(got.Lines, e.Lines) {
			t.Errorf("entry %s changed or lost: %+v", e.Slug, got)
		}
	}
	if got := m.TermsText(); got != "**Invite**: an offer to join.\n\n**Seat**: a paid place.\n" {
		t.Errorf("terms %q", got)
	}
	if m.Promise() != "A team grows by invitation." || m.Overview() != "Invites grow a team." || m.Area != areaA {
		t.Errorf("promise %q overview %q area %v", m.Promise(), m.Overview(), m.Area)
	}
	fp := m.Footprint
	if len(fp.Sources) != 2 || !slices.Equal(fp.Tests, []string{"app/invites_test.go"}) || !slices.Equal(fp.Docs, []string{"docs/a.md"}) {
		t.Errorf("footprint %+v", fp)
	}
	r := s.Record()
	if len(r.Merged) != 1 || r.Merged[0].PromiseOf != "invites-2" || len(r.Merged[0].DroppedOverviews) != 1 || r.Merged[0].DroppedOverviews[0].Text != "The second overview." {
		t.Errorf("record %+v", r.Merged)
	}
	if !slices.Equal(r.Consolidated, []string{"invites"}) {
		t.Errorf("consolidated %v", r.Consolidated)
	}
	checkForm(t, out)
}

func TestMergeWritesThePromiseOfItsMember(t *testing.T) {
	a, b := pair(t)
	for _, tc := range []struct{ name, promiseOf, want string }{
		{"invites", "invites-2", "promise: A team grows by invitation."},
		{"invites-2", "invites", "promise: People invite their team."},
		{"invites", "invites", "promise: People invite their team."},
	} {
		m, _, _, err := mergeGroup([]capabilitytext.Capability{a, b}, tc.name, tc.promiseOf, func(string) bool { return false }, normal)
		if err != nil {
			t.Fatal(err)
		}
		if got := m.Head[m.PromiseLine()]; got != tc.want || m.Name != tc.name {
			t.Errorf("%s from %s: name %q, promise line %q, want %q", tc.name, tc.promiseOf, m.Name, got, tc.want)
		}
	}
}

func TestMergeWithoutOverviewShowsThePromise(t *testing.T) {
	a := parse(t, "a", areaA, recovering.File("a", "A promise.", "# A\n\n## Rules\n\n"+entry("Ra", "ra", "#1", "A.", "a.")), "")
	b := parse(t, "b", areaB, recovering.File("b", "B promise.", "# B\n\nB overview.\n\n## Rules\n\n"+entry("Rb", "rb", "#2", "B.", "b.")), "")
	m, _, _, err := mergeGroup([]capabilitytext.Capability{a, b}, "a", "b", func(string) bool { return false }, normal)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := m.Folder()
	if m.Overview() != "" || !strings.Contains(f.Files[mapfolder.IndexFile], "\nB promise.\n") {
		t.Errorf("index %q", f.Files[mapfolder.IndexFile])
	}
}

func TestMergeRenameTakesTheNextFreeSlugAcrossTheMap(t *testing.T) {
	a := parse(t, "a", areaA, text("a", "A.", "", []string{entry("Ra", "x", "#1", "A.", "a.")}, nil, nil), "")
	b := parse(t, "b", areaB, text("b", "B.", "", []string{entry("Rb", "x", "#2", "B.", "b.")}, nil, nil), "")
	third := map[string]bool{"x-2": true} // a third capability holds x-2
	m, rec, _, err := mergeGroup([]capabilitytext.Capability{a, b}, "a", "a", func(s string) bool { return third[s] }, normal)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Rename{{"b/x", "a/x-3"}}; !slices.Equal(rec.Renamed, want) {
		t.Errorf("renamed %+v, want %+v", rec.Renamed, want)
	}
	if got := ids(m); !slices.Equal(got, []string{"a/x", "a/x-3"}) {
		t.Errorf("ids %v", got)
	}
	if got := m.Entry("x-3"); got == nil || got.Lines[1] != "`x-3` · #2" || got.Lines[0] != "### Rb" {
		t.Errorf("renamed entry %+v", got)
	}

	// Through the state, the pre-pass renames first and the slug a third
	// capability holds is skipped.
	c := parse(t, "c", areaB, text("c", "C.", "", []string{entry("Rc", "x-2", "#3", "C.", "c.")}, nil, nil), "")
	s := New("Product", []capabilitytext.Capability{a, b, c}, nil)
	if !s.Merge([]string{"a", "b"}, "a", "a", normal) {
		t.Fatalf("refused %+v", s.Record().Refused)
	}
	out := s.Output()
	if got := ids(find(t, out.Capabilities, "a")); !slices.Equal(got, []string{"a/x", "a/x-3"}) {
		t.Errorf("ids %v", got)
	}
	checkForm(t, out)
}

func TestMergeTermConflictKeepsTheFirst(t *testing.T) {
	a := parse(t, "a", areaA, text("a", "A.", "", []string{entry("Ra", "ra", "#1", "A.", "a.")}, nil, nil), "**Seat**: a paid place.\n\n**Team**: people.\n")
	b := parse(t, "b", areaB, text("b", "B.", "", []string{entry("Rb", "rb", "#2", "B.", "b.")}, nil, nil), "**seat**: a paid place.\n\n**team**: a group.\n_Avoid_: crew\n")
	m, rec, _, err := mergeGroup([]capabilitytext.Capability{a, b}, "a", "a", func(string) bool { return false }, normal)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.TermsText(); got != "**Seat**: a paid place.\n\n**Team**: people.\n" {
		t.Errorf("terms %q", got)
	}
	want := []TermConflict{{"b", "team", "a", "**team**: a group.\n_Avoid_: crew"}}
	if !slices.Equal(rec.TermConflicts, want) {
		t.Errorf("conflicts %+v, want %+v", rec.TermConflicts, want)
	}
}

func TestMergeRefusesHeadingAnchorClash(t *testing.T) {
	a := parse(t, "a", areaA, text("a", "A.", "", []string{entry("Seats are paid", "ra", "#1", "A.", "a.")}, nil, nil), "")
	b := parse(t, "b", areaB, text("b", "B.", "", []string{entry("Seats are paid!", "rb", "#2", "Other words.", "b.")}, nil, nil), "")
	s := New("Product", []capabilitytext.Capability{a, b}, nil)
	if s.Merge([]string{"a", "b"}, "a", "a", normal) {
		t.Fatal("merged two rules sharing a heading anchor")
	}
	refused(t, s.Record(), StepMerge, "heading anchor")
	if len(s.Output().Capabilities) != 2 {
		t.Error("the capabilities did not stay apart")
	}
}

func TestMergeSizeLimitRefusedAndApart(t *testing.T) {
	a := parse(t, "a", areaA, text("a", "A.", "", many("a", 15, "#1"), nil, nil), "")
	b := parse(t, "b", areaB, text("b", "B.", "", many("b", 15, "#2"), nil, nil), "")
	s := New("Product", []capabilitytext.Capability{a, b}, nil)
	if s.Merge([]string{"a", "b"}, "a", "b", normal) {
		t.Fatal("merged past the rules limit")
	}
	refused(t, s.Record(), StepMerge, "over the limit of 150")
	out := s.Output()
	if len(out.Capabilities) != 2 || out.Capabilities[0].File() != a.File() || out.Capabilities[1].File() != b.File() {
		t.Error("the capabilities did not stay apart unchanged")
	}
	if r := s.Record(); len(r.Merged) != 0 || len(r.Consolidated) != 0 {
		t.Errorf("record %+v", r)
	}
}

func TestMergeSizeLimitCountsAfterExactDuplicates(t *testing.T) {
	a := parse(t, "a", areaA, text("a", "A.", "", many("a", 15, "#1"), nil, nil), "")
	var dups []string
	for i := range 15 {
		dups = append(dups, entry(fmt.Sprintf("rule A %d", i), fmt.Sprintf("dup-%d", i), "#2", fmt.Sprintf("Body a %d!", i), "another reason."))
	}
	b := parse(t, "b", areaB, text("b", "B.", "", dups, nil, nil), "")
	raw := a.Clone()
	for _, e := range b.Entries() {
		raw.Add(e)
	}
	if f, _ := raw.Folder(); capabilitytext.Lines(f.Files[mapfolder.RulesFile]) <= 150 {
		t.Fatal("the raw merge fits: the test needs it over")
	}
	s := New("Product", []capabilitytext.Capability{a, b}, nil)
	if !s.Merge([]string{"a", "b"}, "a", "a", normal) {
		t.Fatalf("refused %+v", s.Record().Refused)
	}
	m := s.Output().Capabilities[0]
	if len(m.Entries()) != 15 {
		t.Errorf("entries %d, want 15", len(m.Entries()))
	}
	if e := m.Entry("a-3"); e == nil || e.Lines[1] != "`a-3` · #1 · #2" {
		t.Errorf("kept entry %+v", e)
	}
	if r := s.Record(); len(r.ExactMerges) != 15 || r.ExactMerges[0].Kept != "a/a-0" || !slices.Equal(r.ExactMerges[0].Merged, []string{"a/dup-0"}) {
		t.Errorf("exact merges %+v", r.ExactMerges)
	}
	checkForm(t, s.Output())
}

func TestMergeOneAreaAndSmallGroupsRefused(t *testing.T) {
	a, b := pair(t)
	c := parse(t, "c", areaA, text("c", "C.", "", []string{entry("Rc", "rc", "#3", "C.", "c.")}, nil, nil), "")
	for _, tc := range []struct {
		group      []string
		name, want string
	}{
		{[]string{"invites", "c"}, "invites", "one area"},
		{[]string{"invites"}, "invites", "two capabilities"},
		{[]string{"invites", "nope"}, "invites", "not a capability"},
		{[]string{"invites", "invites-2"}, "c", "not one of the group's"},
		{[]string{"invites", "invites", "invites-2"}, "invites", "named twice"},
	} {
		s := New("Product", []capabilitytext.Capability{a, b, c}, nil)
		if s.Merge(tc.group, tc.name, tc.name, normal) {
			t.Errorf("%v merged", tc.group)
			continue
		}
		refused(t, s.Record(), StepMerge, tc.want)
		if len(s.Output().Capabilities) != 3 {
			t.Errorf("%v: capabilities changed", tc.group)
		}
	}
}
