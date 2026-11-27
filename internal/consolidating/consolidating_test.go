package consolidating

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/recovering"
)

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// normal is a stand-in for repeats.Normal: lower case, words one space
// apart, punctuation gone.
func normal(heading, body string) string {
	return strings.TrimSpace(nonWord.ReplaceAllString(strings.ToLower(heading+" "+body), " "))
}

// entry is an entry's text: heading, meta line, body and reason.
func entry(heading, slug, anchors, body, because string) string {
	meta := "`" + slug + "`"
	if anchors != "" {
		meta += " · " + anchors
	}
	return "### " + heading + "\n" + meta + "\n" + body + "\nBecause: " + because + "\n"
}

// text is a capability's single text as recovering.File writes it, with
// rules, decisions and questions given as entry texts.
func text(name, promise, overview string, rules, decisions, questions []string) string {
	b := "# " + strings.ToUpper(name[:1]) + name[1:] + "\n\n"
	if overview != "" {
		b += overview + "\n\n"
	}
	for _, s := range []struct {
		word string
		es   []string
	}{{"Rules", rules}, {"Decisions", decisions}, {"Questions", questions}} {
		if len(s.es) > 0 {
			b += "## " + s.word + "\n\n" + strings.Join(s.es, "\n") + "\n"
		}
	}
	return recovering.File(name, promise, b)
}

func parse(t *testing.T, name string, area capabilitytext.Area, file, terms string) capabilitytext.Capability {
	t.Helper()
	c, err := capabilitytext.Parse(name, area, file, terms)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return c
}

var (
	areaA = capabilitytext.Area{Name: "a"}
	areaB = capabilitytext.Area{Name: "b"}
	// areaBPart is an area named a, split from b: not areaA.
	areaBPart = capabilitytext.Area{Name: "a", Parent: "b"}
)

// checkForm writes out as a map in a scratch folder, as the map writes it,
// and runs the map check on it for form only: every problem fails t.
func checkForm(t *testing.T, out Output) {
	t.Helper()
	dir := t.TempDir()
	var terms []string
	for _, c := range out.Capabilities {
		f, err := c.Folder()
		if err != nil {
			t.Fatalf("split %s: %v", c.Name, err)
		}
		if err := f.Write(dir, mapfolder.CapabilityDir(c.Name)); err != nil {
			t.Fatal(err)
		}
		if tt := strings.TrimSpace(c.TermsText()); tt != "" {
			terms = append(terms, tt)
		}
	}
	files := map[string]string{mapfolder.IndexFile: mapfolder.Index("---\npromise: The product.\n---", out.Product, "The product.", nil)}
	if len(out.Rules) > 0 {
		files[mapfolder.IndexFile] = mapfolder.Index("---\npromise: The product.\n---", out.Product, "The product.", []mapfolder.Kind{mapfolder.Rule})
		files[mapfolder.RulesFile] = ProductRules(out.Product, out.Rules)
	}
	if err := (mapfolder.Folder{Files: files}).Write(dir, mapfolder.ProductDir); err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string]string{
		".physa/README.md":      "# The map\n",
		recovering.GlossaryPath: recovering.Glossary(strings.Join(terms, "\n\n")),
		"docs/a.md":             "a\n",
		"docs/b.md":             "b\n",
		"docs/c.md":             "c\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := mapfile.Read(dir); err != nil {
		t.Fatal(err)
	}
	res, err := mapcheck.CheckWith(t.Context(), dir, mapcheck.Options{FormOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Problems {
		t.Errorf("map check: %s:%d: %s", p.Path, p.Line, p.Message)
	}
}

// refused fails t unless rec refuses an item at step whose reason holds
// want, and returns that reason.
func refused(t *testing.T, rec Record, step, want string) string {
	t.Helper()
	for _, r := range rec.Refused {
		if r.Step == step && strings.Contains(r.Reason, want) {
			t.Logf("refused at %s: %s: %s", r.Step, r.Item, r.Reason)
			return r.Reason
		}
	}
	t.Fatalf("no refusal at %s holding %q in %+v", step, want, rec.Refused)
	return ""
}

func ids(c capabilitytext.Capability) []string {
	var out []string
	for _, e := range c.Entries() {
		out = append(out, ID(c.Name, e.Slug))
	}
	return out
}

func find(t *testing.T, caps []capabilitytext.Capability, name string) capabilitytext.Capability {
	t.Helper()
	for _, c := range caps {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no capability %q", name)
	return capabilitytext.Capability{}
}

func TestUnchangedCapabilitiesRenderByteForByte(t *testing.T) {
	files := []string{
		text("invites", "People invite their team.", "Invites are how a team grows.",
			[]string{entry("Invites expire", "invite-expiry", "#12 abc1234", "An invite lasts a week.", "stale links leak access."),
				entry("One seat per invite", "invite-seat", "from docs/a.md", "Each invite holds a seat.\n\nUntil it is used.", "seats are paid.")},
			[]string{"### We email invites\n`invite-email` · decided by dphaener, in their words · 2026-01-02 · #3\nInvites go by email.\nBecause: everyone has one.\nRejected: links in chat."},
			[]string{"### Should invites renew?\n`invite-renew` · raised by dphaener · 2026-01-02\nNobody has decided.\n\n```\n## not a section\n### not an entry\n```"}),
		text("billing", "People pay for seats.", "", []string{entry("Seats are billed monthly", "seat-bill", "#4", "Monthly.", "cash flow.")}, nil, nil),
		recovering.File("empty", "Nothing yet.", "# Empty\n\nOnly an overview."),
	}
	terms := []string{"**Invite**: an offer to join a team.\n_Avoid_: invitation\n\n**Seat**: a paid place.\n", "", "**Plan**: what a team pays for.\n"}
	var caps []capabilitytext.Capability
	for i, f := range files {
		c := parse(t, fmt.Sprintf("c%d", i), areaA, f, terms[i])
		if got := c.File(); got != f {
			t.Errorf("file %d renders\n%q\nwant\n%q", i, got, f)
		}
		if got := c.TermsText(); got != terms[i] {
			t.Errorf("terms %d render %q, want %q", i, got, terms[i])
		}
		caps = append(caps, c)
	}
	s := New("Product", caps, nil)
	for i, c := range s.Output().Capabilities {
		if c.File() != files[i] || c.TermsText() != terms[i] {
			t.Errorf("capability %d changed with nothing done", i)
		}
	}
	if r := s.Record(); len(r.Consolidated) != 0 {
		t.Errorf("consolidated %v with nothing done", r.Consolidated)
	}
	checkForm(t, s.Output())
}

func TestParseReadsEntriesAndAnchors(t *testing.T) {
	c := parse(t, "invites", areaA, text("invites", "P.", "", []string{
		entry("Invites expire", "invite-expiry", "decided by dphaener, in their words · 2026-01-02 · #12 abc1234 · from docs/a.md", "B.", "R."),
	}, nil, []string{entry("Renew?", "invite-renew", "", "Q.", "R.")}), "")
	es := c.Entries()
	if len(es) != 2 || es[0].Kind != mapfolder.Rule || es[1].Kind != mapfolder.Question {
		t.Fatalf("entries %+v", es)
	}
	want := []mapfile.Anchor{{Kind: mapfile.PR, Value: "12"}, {Kind: mapfile.Commit, Value: "abc1234"}, {Kind: mapfile.Doc, Value: "docs/a.md"}}
	if !slices.Equal(es[0].Anchors, want) || es[0].Slug != "invite-expiry" {
		t.Errorf("anchors %+v slug %q", es[0].Anchors, es[0].Slug)
	}
	if got := es[0].Rest(); !slices.Equal(got, []string{"decided by dphaener, in their words", "2026-01-02"}) {
		t.Errorf("rest %q", got)
	}
	if c.Promise() != "P." || c.Title() != "Invites" {
		t.Errorf("promise %q title %q", c.Promise(), c.Title())
	}
}

func TestAddAnchorsKeepsWordsAndChecks(t *testing.T) {
	c := parse(t, "invites", areaA, text("invites", "P.", "", nil, []string{
		entry("Invites expire", "invite-expiry", "decided by dphaener, in their words · 2026-01-02 · #12", "An invite lasts a week.", "stale links leak access."),
	}, nil), "")
	e := c.Entry("invite-expiry")
	words := e.Words()
	o1 := capabilitytext.Entry{Anchors: []mapfile.Anchor{{Kind: mapfile.PR, Value: "12"}, {Kind: mapfile.Commit, Value: "def5678"}, {Kind: mapfile.Doc, Value: "docs/b.md"}}}
	o2 := capabilitytext.Entry{Anchors: []mapfile.Anchor{{Kind: mapfile.PR, Value: "40"}, {Kind: mapfile.Doc, Value: "docs/b.md"}}}
	added := capabilitytext.AddAnchors(e, o1, o2)
	if !slices.Equal(added, []string{"def5678", "from docs/b.md", "#40"}) {
		t.Errorf("added %q", added)
	}
	if want := "`invite-expiry` · decided by dphaener, in their words · 2026-01-02 · #12 · def5678 #40 · from docs/b.md"; e.Lines[1] != want {
		t.Errorf("meta %q, want %q", e.Lines[1], want)
	}
	if !slices.Equal(e.Words(), words) {
		t.Errorf("words changed: %q", e.Words())
	}
	checkForm(t, Output{Product: "Product", Capabilities: []capabilitytext.Capability{c}})
	re := parse(t, "invites", areaA, c.File(), "")
	if got := re.Entry("invite-expiry").Anchors; len(got) != 4 {
		t.Errorf("anchors read back %+v", got)
	}
}

func TestPrePassRenamesSlugsAndListsTermConflicts(t *testing.T) {
	a := parse(t, "a", areaA, text("a", "A.", "", []string{entry("Ra", "shared", "#1", "A.", "a.")}, nil, nil), "**Seat**: a paid place.\n\n**Team**: people.\n")
	b := parse(t, "b", areaB, text("b", "B.", "", []string{entry("Rb", "shared", "#2", "B.", "b.")}, nil, nil), "**seat**: a paid place.\n\n**Team**: a group.\n")
	c := parse(t, "c", areaB, text("c", "C.", "", []string{entry("Rc", "shared-2", "#3", "C.", "c.")}, nil, nil), "")
	s := New("Product", []capabilitytext.Capability{a, b, c}, nil)
	r := s.Record()
	if want := []Rename{{"b/shared", "b/shared-3"}}; !slices.Equal(r.SlugRenames, want) {
		t.Errorf("renames %+v, want %+v", r.SlugRenames, want)
	}
	if len(r.TermConflicts) != 1 || r.TermConflicts[0].Name != "Team" || r.TermConflicts[0].KeptIn != "a" || r.TermConflicts[0].Text != "**Team**: a group." {
		t.Errorf("term conflicts %+v", r.TermConflicts)
	}
	out := s.Output()
	if got := find(t, out.Capabilities, "b").TermsText(); got != "" {
		t.Errorf("b keeps terms %q", got)
	}
	if !slices.Equal(r.Consolidated, []string{"b"}) {
		t.Errorf("consolidated %v", r.Consolidated)
	}
	checkForm(t, out)
}

// areaMap is four capabilities: two in area a, one in area b and one in
// the part a of b.
func areaMap(t *testing.T) []capabilitytext.Capability {
	return []capabilitytext.Capability{
		parse(t, "a1", areaA, text("a1", "A1.", "", []string{
			entry("Seats are paid", "a1-seat", "#1", "Each seat costs money.", "a."),
			entry("Invites expire", "a1-expire", "#2", "A week.", "a."),
		}, nil, []string{
			"### Do seats renew?\n`a1-renew` · raised by dphaener · 2026-01-02\nUnclear.\n",
			"### Is billing monthly?\n`a1-monthly` · raised by dphaener · 2026-01-02\nUnclear.\n",
		}), ""),
		parse(t, "a2", areaA, text("a2", "A2.", "", []string{
			entry("seats are paid!", "a2-seat", "#3 abc1234", "Each seat  costs money", "another reason."),
			entry("Billing is monthly", "a2-monthly", "#4", "Monthly.", "a."),
		}, nil, nil), ""),
		parse(t, "b1", areaB, text("b1", "B1.", "", []string{
			entry("Seats are paid", "b1-seat", "#5", "Each seat costs money.", "b."),
		}, nil, nil), ""),
		parse(t, "p1", areaBPart, text("p1", "P1.", "", []string{
			entry("Seats are paid", "p1-seat", "#6", "Each seat costs money.", "p."),
		}, nil, nil), ""),
	}
}

func TestExactDuplicatesMergeInAnAreaAndRepeatAcross(t *testing.T) {
	s := New("Product", areaMap(t), nil)
	repeats := s.ExactDuplicates(normal)
	r := s.Record()
	if want := []EntryMerge{{"a1/a1-seat", []string{"a2/a2-seat"}, []string{"#3", "abc1234"}}}; len(r.ExactMerges) != 1 ||
		r.ExactMerges[0].Kept != want[0].Kept || !slices.Equal(r.ExactMerges[0].Merged, want[0].Merged) || !slices.Equal(r.ExactMerges[0].AnchorsAdded, want[0].AnchorsAdded) {
		t.Errorf("exact merges %+v, want %+v", r.ExactMerges, want)
	}
	out := s.Output()
	a1, a2 := find(t, out.Capabilities, "a1"), find(t, out.Capabilities, "a2")
	if e := a1.Entry("a1-seat"); e == nil || e.Lines[1] != "`a1-seat` · #1 · #3 abc1234" {
		t.Errorf("kept %+v", e)
	}
	if a2.Entry("a2-seat") != nil {
		t.Error("the duplicate stayed")
	}
	if len(repeats) != 1 || !slices.Equal(repeats[0].IDs, []string{"a1/a1-seat", "b1/b1-seat", "p1/p1-seat"}) || len(repeats[0].Areas) != 3 {
		t.Errorf("repeats %+v", repeats)
	}
	if !slices.Equal(r.Consolidated, []string{"a1", "a2"}) {
		t.Errorf("consolidated %v", r.Consolidated)
	}
	checkForm(t, out)
}

func TestApplyAreaMergesMovesAndDrops(t *testing.T) {
	s := New("Product", areaMap(t), nil)
	s.ApplyArea(areaA, AreaChoices{
		Merges: []MergeChoice{{Keep: "a1/a1-seat", Drop: []string{"a2/a2-seat"}}},
		Moves:  []MoveChoice{{ID: "a2/a2-monthly", To: "a1"}},
		Drops:  []DropChoice{{ID: "a1/a1-monthly", AnsweredBy: "a1/a2-monthly"}},
	})
	r := s.Record()
	if len(r.Refused) != 0 {
		t.Fatalf("refused %+v", r.Refused)
	}
	a1 := find(t, s.Output().Capabilities, "a1")
	if got := ids(a1); !slices.Equal(got, []string{"a1/a1-seat", "a1/a1-expire", "a1/a2-monthly", "a1/a1-renew"}) {
		t.Errorf("a1 %v", got)
	}
	if e := a1.Entry("a1-seat"); e.Lines[1] != "`a1-seat` · #1 · #3 abc1234" {
		t.Errorf("merged meta %q", e.Lines[1])
	}
	if len(r.EntryMerges) != 1 || !slices.Equal(r.Moves, []Move{{"a2/a2-monthly", "a1/a2-monthly"}}) || !slices.Equal(r.Dropped, []DroppedQuestion{{"a1/a1-monthly", "a1/a2-monthly"}}) {
		t.Errorf("record %+v", r)
	}
	checkForm(t, s.Output())
}

func TestApplyAreaRefusesBadItems(t *testing.T) {
	big := many("big", 24, "#9")
	caps := append(areaMap(t), parse(t, "full", areaA, text("full", "F.", "", big, nil, nil), ""))
	caps = append(caps, parse(t, "a3", areaA, text("a3", "A3.", "", []string{entry("Invites expire", "a3-expire", "#7", "Other words.", "a.")}, nil, nil), ""))
	for _, tc := range []struct {
		name string
		ch   AreaChoices
		want string
	}{
		{"unknown id", AreaChoices{Merges: []MergeChoice{{"a1/nope", []string{"a2/a2-seat"}}}}, "not an entry"},
		{"outside the area", AreaChoices{Merges: []MergeChoice{{"a1/a1-seat", []string{"b1/b1-seat"}}}}, "not in the area"},
		{"kind mismatch", AreaChoices{Merges: []MergeChoice{{"a1/a1-seat", []string{"a1/a1-renew"}}}}, "only entries of one kind"},
		{"empty merge", AreaChoices{Merges: []MergeChoice{{"a1/a1-seat", nil}}}, "names no entry"},
		{"move a question", AreaChoices{Moves: []MoveChoice{{"a1/a1-renew", "a2"}}}, "only rules move"},
		{"move to itself", AreaChoices{Moves: []MoveChoice{{"a1/a1-seat", "a1"}}}, "already in"},
		{"move out of the area", AreaChoices{Moves: []MoveChoice{{"a1/a1-seat", "b1"}}}, "not in the area"},
		{"move onto a heading anchor", AreaChoices{Moves: []MoveChoice{{"a1/a1-expire", "a3"}}}, "has the anchor of a heading"},
		{"move past the rules limit", AreaChoices{Moves: []MoveChoice{{"a1/a1-expire", "full"}}}, "over the limit of 150"},
		{"drop a rule", AreaChoices{Drops: []DropChoice{{"a1/a1-seat", "a1/a1-expire"}}}, "only questions"},
		{"empty drop", AreaChoices{Drops: []DropChoice{{"a1/a1-renew", ""}}}, "names no entry"},
		{"drop answered outside", AreaChoices{Drops: []DropChoice{{"a1/a1-renew", "b1/b1-seat"}}}, "not in the area"},
		{"already removed", AreaChoices{
			Merges: []MergeChoice{{"a1/a1-seat", []string{"a2/a2-seat"}}},
			Moves:  []MoveChoice{{"a2/a2-seat", "a1"}},
		}, "already removed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New("Product", caps, nil)
			before := s.Output()
			s.ApplyArea(areaA, tc.ch)
			refused(t, s.Record(), StepArea, tc.want)
			if tc.name != "already removed" {
				for i, c := range s.Output().Capabilities {
					if c.File() != before.Capabilities[i].File() {
						t.Errorf("%s changed", c.Name)
					}
				}
			}
		})
	}
}
