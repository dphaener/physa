package initreport

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/spend"
)

// SectionHeading is the PR body section's heading line.
const SectionHeading = "## Measures"

// line is one line of the report, and whether it sits under the one before.
type line struct {
	text  string
	under bool
}

// Text is the report as plain text for the run's output: a first line, then
// one measure to a line, each detail indented under it, spend per phase last.
func Text(r Report) string {
	return render("Measures of init's record, in counts:\n", "", lines(r, true))
}

// Section is the report as the PR body's section: its heading and a list,
// without spend, which the body's "Tokens spent" table holds.
func Section(r Report) string { return render(SectionHeading+"\n\n", "- ", lines(r, false)) }

// Part is st's report as one part of the run's output: the key it goes
// under in a --json document, its plain text and its JSON value.
func Part(ctx context.Context, st *initrecord.Store) (key, text string, v Report) {
	r := Load(ctx, st)
	return "report", Text(r), r
}

func render(head, bullet string, ls []line) string {
	var b strings.Builder
	b.WriteString(head)
	for _, l := range ls {
		if l.under {
			b.WriteString("  ")
		}
		b.WriteString(bullet + l.text + "\n")
	}
	return b.String()
}

// lines is every measure's lines, in order, spend included when withSpend.
func lines(r Report, withSpend bool) []line {
	out := slices.Concat(
		lined("Source files", r.Files, filesLines),
		lined("PRs and changes placed", r.Placed, placedLines),
		lined("PRs in one capability", r.OneCapability, oneLines),
		lined("Capabilities with no PR or change", r.Unplaced, ratioLines),
		lined("Rules, by where their reasons came from", r.Reasons, reasonsLines),
		lined("Consolidation", r.Consolidation, consolidationLines),
	)
	if withSpend {
		out = append(out, lined("Spend per phase", r.Spend, spendLines)...)
	}
	return out
}

// lined is m's lines from f, or its one line saying why it can't be given.
func lined[T any](label string, m Measure[T], f func(string, T) []line) []line {
	if m.Value == nil {
		return []line{{text: label + ": can't be given: " + m.CantBeGiven + "."}}
	}
	return f(label, *m.Value)
}

func under(format string, a ...any) line { return line{text: fmt.Sprintf(format, a...), under: true} }

// of words a ratio: "3 of 6 (50%)", and part the count of a known total: "3 (50%)".
func (r Ratio) of() string { return fmt.Sprintf("%d of %d (%d%%)", r.Count, r.Of, r.Percent) }

func (r Ratio) part() string { return fmt.Sprintf("%d (%d%%)", r.Count, r.Percent) }

func ratioLines(label string, v Ratio) []line { return []line{{text: label + ": " + v.of()}} }

func filesLines(label string, f FileCounts) []line {
	return []line{
		{text: fmt.Sprintf("%s: %d", label, f.SourceFiles)},
		under("in a capability: %s", f.InCapability.part()),
		under("shared: %s", f.Shared.part()),
		under("unclaimed: %s", f.Unclaimed.part()),
	}
}

func placedLines(_ string, p PlacedCounts) []line {
	return slices.Concat(lined("PRs placed", p.PRs, placingLines), lined("Changes no gathered PR holds, placed", p.Changes, placingLines),
		[]line{{text: fmt.Sprintf("Changes that follow their PR: %d", p.FollowTheirPR)}})
}

func placingLines(label string, p PlacingCounts) []line {
	out := []line{{text: label + ": " + p.of()}}
	for _, n := range p.Nowhere {
		out = append(out, under("placed nowhere, %s: %d", n.Reason, n.Count))
	}
	return out
}

func oneLines(label string, o OneCounts) []line {
	return []line{
		{text: label + ": " + o.of() + ", of the PRs touching a source file a capability owns"},
		under("left out, touching no such file: %d", o.LeftOut),
	}
}

func reasonsLines(label string, r ReasonCounts) []line {
	out := []line{{text: fmt.Sprintf("%s: %d", label, r.Rules)}}
	for i, a := range r.ByAnchor { // in anchorKinds' order
		out = append(out, under("%s: %s", anchorKinds[i][1], a.part()))
	}
	for _, l := range lined("test-only rules carrying a Because: line", r.TestOnlyWithBecause, ratioLines) {
		out = append(out, under("%s", l.text))
	}
	counted := func(n int, format string) {
		if n > 0 {
			out = append(out, under(format, n))
		}
	}
	counted(r.ProductRules, "product rules among them: %d")
	counted(r.ProductRulesNotCounted, "product rules not counted, since their text can't be read: %d")
	counted(r.CapabilitiesNotCounted, "capabilities not counted, since their text can't be read: %d")
	return out
}

func consolidationLines(label string, c ConsolidationCounts) []line {
	return []line{
		{text: label + ":"},
		under("capabilities merged: %d %s, %d merged away", c.CapabilityMerges, plural(c.CapabilityMerges, "group", "groups"), c.CapabilitiesMergedAway),
		under("entries merged: %d exact, %d near", c.EntriesMergedExact, c.EntriesMergedNear),
		under("rules moved: %d", c.Moved),
		under("questions dropped as answered: %d", c.Dropped),
		under("rules lifted to the product: %d", c.Lifted),
		under("refused: %d", c.Refused),
		under("entries: %d before, %d after, and %d product %s", c.EntriesBefore, c.EntriesAfter, c.ProductRulesAfter, plural(c.ProductRulesAfter, "rule", "rules")),
	}
}

func spendLines(label string, s SpendCounts) []line {
	out := []line{{text: label + ":"}}
	for _, p := range append(append([]PhaseSpend{}, s.Phases...), s.Total) {
		out = append(out, under("%s: %d input, %d cached input, %d output; %s tokens (%s)",
			p.Phase, p.Input, p.CachedInput, p.Output, spend.Tokens(p.WeightedTokens), p.Cost))
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
