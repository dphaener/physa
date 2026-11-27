package consolidateguard_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/consolidateguard"
	"github.com/dphaener/physa/internal/consolidating"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/recovering"
)

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

func normal(heading, body string) string {
	return strings.TrimSpace(nonWord.ReplaceAllString(strings.ToLower(heading+" "+body), " "))
}

func entry(heading, slug, anchors, body string) string {
	return "### " + heading + "\n`" + slug + "` · " + anchors + "\n" + body + "\nBecause: a reason.\n"
}

func capability(t *testing.T, name string, area capabilitytext.Area, rules, questions []string, terms string) capabilitytext.Capability {
	t.Helper()
	b := "# " + name + "\n\nThe overview of " + name + ".\n\n"
	if len(rules) > 0 {
		b += "## Rules\n\n" + strings.Join(rules, "\n") + "\n"
	}
	if len(questions) > 0 {
		b += "## Questions\n\n" + strings.Join(questions, "\n") + "\n"
	}
	c, err := capabilitytext.Parse(name, area, recovering.File(name, "The promise of "+name+".", b), terms)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func question(heading, slug string) string {
	return "### " + heading + "\n`" + slug + "` · raised by dphaener · 2026-01-02\nUnclear.\n"
}

var (
	areaA = capabilitytext.Area{Name: "a"}
	areaB = capabilitytext.Area{Name: "b"}
	areaC = capabilitytext.Area{Name: "c"}
)

// inputs is five capabilities over three areas, with a slug two of them
// share, rules repeated across areas and a question another entry answers.
func inputs(t *testing.T) []capabilitytext.Capability {
	return []capabilitytext.Capability{
		capability(t, "invites", areaA, []string{
			entry("Invites expire", "expiry", "#1", "A week."),
			entry("Seats are paid", "seat-a", "#2", "Each seat costs money."),
		}, []string{question("Do invites renew?", "renew")}, "**Invite**: an offer to join.\n"),
		capability(t, "links", areaA, []string{
			entry("Links are single use", "once", "#3", "A link works once."),
			entry("Used invites expire", "expiry-2", "abc1234", "A used link expires."),
		}, nil, ""),
		capability(t, "invites-2", areaB, []string{
			entry("Invites name a team", "expiry", "#4", "An invite names its team."),
		}, nil, "**Invite**: an offer to join.\n\n**Team**: people.\n"),
		capability(t, "billing", areaC, []string{
			entry("Seats are paid", "seat-c", "#5", "Each seat costs money."),
			entry("Renewal is yearly", "yearly", "#6", "Seats renew each year."),
		}, nil, "**Team**: a group.\n"),
		capability(t, "plans", areaC, []string{entry("Plans are fixed", "fixed", "#7", "Three plans.")}, nil, ""),
	}
}

// consolidate runs every step over inputs, as W2.T1 will.
func consolidate(t *testing.T, in []capabilitytext.Capability) (consolidating.Output, consolidating.Record) {
	t.Helper()
	s := consolidating.New("Product", in, nil)
	if !s.Merge([]string{"invites", "invites-2"}, "invites", "invites-2", normal) {
		t.Fatalf("merge refused: %+v", s.Record().Refused)
	}
	repeats := s.ExactDuplicates(normal)
	s.ApplyArea(areaA, consolidating.AreaChoices{
		Moves: []consolidating.MoveChoice{{ID: "links/expiry-2", To: "invites"}},
		Drops: []consolidating.DropChoice{{ID: "invites/renew", AnsweredBy: "invites/expiry"}},
	})
	s.ApplyArea(areaC, consolidating.AreaChoices{
		Merges: []consolidating.MergeChoice{{Keep: "billing/yearly", Drop: []string{"plans/fixed"}}},
	})
	if len(repeats) != 1 {
		t.Fatalf("repeats %+v", repeats)
	}
	if !s.Lift(consolidating.LiftChoice{Keep: repeats[0].IDs[0], With: repeats[0].IDs[1:]}, repeats[0].IDs, [][]string{repeats[0].IDs}) {
		t.Fatalf("lift refused: %+v", s.Record().Refused)
	}
	r := s.Record()
	if len(r.Refused) != 0 {
		t.Fatalf("refused %+v", r.Refused)
	}
	return s.Output(), r
}

func guarded(t *testing.T, in []capabilitytext.Capability, out consolidating.Output, rec consolidating.Record, want string) {
	t.Helper()
	got := consolidateguard.Guard(in, out, rec)
	for _, r := range got {
		if r.Step == consolidating.StepGuard && strings.Contains(r.Reason, want) {
			t.Logf("refused: %s: %s", r.Item, r.Reason)
			return
		}
	}
	t.Fatalf("no refusal holding %q in %+v", want, got)
}

func capOf(out *consolidating.Output, name string) *capabilitytext.Capability {
	for i := range out.Capabilities {
		if out.Capabilities[i].Name == name {
			return &out.Capabilities[i]
		}
	}
	return nil
}

func TestGuardAcceptsAConsolidation(t *testing.T) {
	in := inputs(t)
	out, rec := consolidate(t, in)
	if got := consolidateguard.Guard(in, out, rec); len(got) != 0 {
		t.Fatalf("refused %+v", got)
	}
	if len(rec.SlugRenames) != 1 || rec.SlugRenames[0] != (consolidating.Rename{From: "invites-2/expiry", To: "invites-2/expiry-3"}) {
		t.Errorf("renames %+v", rec.SlugRenames)
	}
	if len(rec.TermConflicts) != 1 || len(out.Rules) != 1 || len(rec.ExactMerges) != 0 {
		t.Errorf("record %+v, rules %+v", rec, out.Rules)
	}
}

func TestGuardRefusesAnEntryWithNoSource(t *testing.T) {
	in := inputs(t)
	out, rec := consolidate(t, in)
	added, _ := capabilitytext.Parse("x", areaA, recovering.File("x", "X.", "# X\n\n## Rules\n\n"+entry("Made up", "made-up", "#1", "Invented.")), "")
	capOf(&out, "links").Add(added.Entries()[0])
	guarded(t, in, out, rec, "no source among the recovered entries")
}

func TestGuardRefusesChangedWords(t *testing.T) {
	in := inputs(t)
	out, rec := consolidate(t, in)
	e := capOf(&out, "links").Entry("once")
	e.Lines[2] = "A link works twice."
	guarded(t, in, out, rec, "no source among the recovered entries")
	guarded(t, in, out, rec, "it is not in the output") // its source is lost too
}

func TestGuardRefusesAnAnchorNoInputHad(t *testing.T) {
	in := inputs(t)
	out, rec := consolidate(t, in)
	capabilitytext.AddAnchors(capOf(&out, "links").Entry("once"), capabilitytext.Entry{Anchors: []mapfile.Anchor{{Kind: mapfile.PR, Value: "404"}}})
	guarded(t, in, out, rec, `anchor "#404" is on none`)
}

func TestGuardRefusesAnEntryLostWithoutRecord(t *testing.T) {
	in := inputs(t)
	out, rec := consolidate(t, in)
	capOf(&out, "links").Remove("once")
	guarded(t, in, out, rec, "the record names it neither merged, dropped nor lifted")

	// A merge recorded without the kept entry taking the anchors loses them.
	out, rec = consolidate(t, in)
	y := capOf(&out, "billing").Entry("yearly")
	y.Lines[1], y.Anchors = "`yearly` · #6", y.Anchors[:1]
	guarded(t, in, out, rec, `anchor "#7" is not on billing/yearly`)
}

func TestGuardRefusesALostTermAndAForeignPromise(t *testing.T) {
	in := inputs(t)
	out, rec := consolidate(t, in)
	capOf(&out, "invites").Terms = nil
	guarded(t, in, out, rec, "the term is not in the output")

	out, rec = consolidate(t, in)
	c := capOf(&out, "invites")
	c.Head[c.PromiseLine()] = "promise: The promise of invites."
	guarded(t, in, out, rec, `whose promise the merge keeps`)
	c.Head[c.PromiseLine()] = "promise: Something new."
	guarded(t, in, out, rec, "is no recovered capability's")
}

func TestGuardRefusesSharedSlugsAndHeadingAnchors(t *testing.T) {
	in := inputs(t)
	out, rec := consolidate(t, in)
	billing := capOf(&out, "billing")
	e := billing.Entry("yearly")
	capabilitytext.Rename(e, "once")
	guarded(t, in, out, rec, "its slug is also links/once's")

	out, rec = consolidate(t, in)
	moved := capOf(&out, "links").Entry("once").Clone()
	capOf(&out, "links").Remove("once")
	moved.Heading = "Links are single use"
	inv := capOf(&out, "invites")
	inv.Add(moved)
	inv.Add(moved)
	guarded(t, in, out, rec, "has the anchor of another heading")
}

func TestGuardKeepsRenamedIDsApart(t *testing.T) {
	in := inputs(t)
	out, rec := consolidate(t, in)
	// invites-2/expiry was renamed before the merge: dropping invites/expiry
	// must not count as recording it, and the other way round.
	capOf(&out, "invites").Remove("expiry-3")
	rec.Dropped = append(rec.Dropped, consolidating.DroppedQuestion{ID: "invites/expiry", AnsweredBy: "invites/once"})
	guarded(t, in, out, rec, "the record names it neither merged, dropped nor lifted")
}
