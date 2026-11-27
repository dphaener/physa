package initreport

import (
	"cmp"
	"math"
	"path"
	"strings"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/spend"
)

// anchorKinds is each kind of anchor as JSON and the lines name it, in the
// order the first that applies is taken.
var anchorKinds = [][2]string{{kindPR, "a PR or change"}, {kindDoc, "a doc, no PR"}, {kindSourceTest, "a source file and a test"},
	{kindTest, "a test only"}, {kindSource, "a source file only"}, {kindNone, "no anchor"}}

const (
	kindPR, kindDoc, kindSourceTest = "pr_or_change", "doc", "source_and_test"
	kindTest, kindSource, kindNone  = "test_only", "source_only", "none"
)

// ratio is n of of, which is not zero.
func ratio(n, of int) Ratio {
	return Ratio{Count: n, Of: of, Percent: int(math.Round(100 * float64(n) / float64(of)))}
}

// share is n of of, or why when of is zero.
func share(n, of int, why string) Measure[Ratio] {
	if of == 0 {
		return cant[Ratio](why)
	}
	return given(ratio(n, of))
}

// owners is the walk capabilities owning each source file.
func (r record) owners() map[string][]string {
	out := map[string][]string{}
	for _, c := range r.walk.v.Capabilities {
		for _, f := range c.Sources {
			out[f] = append(out[f], c.Name)
		}
	}
	return out
}

// files counts the areas' source files: in a capability when one walk
// capability owns it, shared when the walk names it shared (itself or a
// folder holding it) or two or more own it, and unclaimed otherwise.
func (r record) files() Measure[FileCounts] {
	if why := cmp.Or(r.areas.why(noAreas), r.walk.why(noWalk)); why != "" {
		return cant[FileCounts](why)
	}
	src := r.areas.v.Split.Files[sourceset.Source]
	if len(src) == 0 {
		return cant[FileCounts](noSourceFiles)
	}
	owners := r.owners()
	shared := func(f string) bool {
		for _, s := range r.walk.v.Shared {
			if s.Path == f || strings.HasSuffix(s.Path, "/") && strings.HasPrefix(f, s.Path) {
				return true
			}
		}
		return len(owners[f]) > 1
	}
	var one, many int
	for _, f := range src {
		if shared(f) {
			many++
		} else if len(owners[f]) == 1 {
			one++
		}
	}
	n := len(src)
	return given(FileCounts{SourceFiles: n, InCapability: ratio(one, n), Shared: ratio(many, n), Unclaimed: ratio(n-one-many, n)})
}

// placeWhy is why the placements can't be read, naming a missing walk.
func (r record) placeWhy() string {
	if !r.walk.ok && r.walk.bad == "" {
		return r.place.why(noPlaceNoWalk)
	}
	return r.place.why(noPlace)
}

// placed counts the gathered PRs placed and the changes no gathered PR
// holds placed on their own, and the changes that follow their PR.
func (r record) placed() Measure[PlacedCounts] {
	if why := cmp.Or(r.gather.why(noGather), r.placeWhy()); why != "" {
		return cant[PlacedCounts](why)
	}
	p, prWhy, freeWhy, placedFree := r.place.v, []string{}, []string{}, 0
	for _, n := range p.Nowhere.PRs {
		prWhy = append(prWhy, n.Reason)
	}
	for _, c := range p.Changes {
		if c.PR == 0 {
			placedFree++
		}
	}
	for _, c := range p.Nowhere.Changes {
		if c.PR == 0 {
			freeWhy = append(freeWhy, c.Reason)
		}
	}
	return given(PlacedCounts{
		PRs:           placing(len(p.PRs), len(r.gather.v.PRs), prWhy, noPRs),
		Changes:       placing(placedFree, placedFree+len(freeWhy), freeWhy, noFreeChanges),
		FollowTheirPR: len(p.Changes) + len(p.Nowhere.Changes) - placedFree - len(freeWhy),
	})
}

// placing is n placed of of, with those placed nowhere counted by place's
// two reasons.
func placing(n, of int, reasons []string, why string) Measure[PlacingCounts] {
	if of == 0 {
		return cant[PlacingCounts](why)
	}
	nowhere := []Nowhere{{place.NoFiles, 0}, {place.NoOwner, 0}}
	for _, reason := range reasons {
		for i := range nowhere {
			if nowhere[i].Reason == reason {
				nowhere[i].Count++
			}
		}
	}
	return given(PlacingCounts{Ratio: ratio(n, of), Nowhere: nowhere})
}

// oneCapability takes each gathered PR's source files a walk capability
// owns, from the changes the placements give it: a PR with none is left
// out, and one whose files all belong to one capability is in one.
func (r record) oneCapability() Measure[OneCounts] {
	if why := cmp.Or(r.gather.why(noGather), r.walk.why(noWalk), r.placeWhy()); why != "" {
		return cant[OneCounts](why)
	}
	prOf := map[string]int{}
	for _, c := range r.place.v.Changes {
		prOf[c.ID] = c.PR
	}
	for _, c := range r.place.v.Nowhere.Changes {
		prOf[c.ID] = c.PR
	}
	type held struct {
		pr  int
		cap string
	}
	owners, seen, caps := r.owners(), map[held]bool{}, map[int]int{}
	for _, ch := range r.gather.v.Changes {
		for _, f := range ch.Files {
			for _, c := range owners[f] {
				if h := (held{prOf[ch.ID], c}); !seen[h] {
					seen[h], caps[h.pr] = true, caps[h.pr]+1
				}
			}
		}
	}
	in, of := 0, 0
	for _, p := range r.gather.v.PRs {
		if n := caps[p.Number]; n > 0 {
			of++
			if n == 1 {
				in++
			}
		}
	}
	if of == 0 {
		return cant[OneCounts](noOwnedPR)
	}
	return given(OneCounts{Ratio: ratio(in, of), LeftOut: len(r.gather.v.PRs) - of})
}

// unplaced counts the walk's capabilities placed with no PR and no change.
func (r record) unplaced() Measure[Ratio] {
	if why := cmp.Or(r.walk.why(noWalk), r.placeWhy()); why != "" {
		return cant[Ratio](why)
	}
	placed := map[string]bool{}
	for _, c := range r.place.v.ByCapability {
		placed[c.Capability] = len(c.PRs)+len(c.Changes) > 0
	}
	none := 0
	for _, c := range r.walk.v.Capabilities {
		if !placed[c.Name] {
			none++
		}
	}
	return share(none, len(r.walk.v.Capabilities), noWalkCapability)
}

// texts is the capabilities `--write` would write, as name and text, and
// the product's rules: a current consolidation's, else the rows'.
func (r record) texts() (caps [][2]string, product []string) {
	gone, changed := map[string]bool{}, map[string]string{}
	if r.cons.ok && r.current {
		for _, a := range r.cons.v.MergedAway {
			gone[a.Name] = true
		}
		for _, c := range r.cons.v.Capabilities {
			changed[c.Name] = c.File
		}
		product = r.cons.v.ProductRules
	}
	for _, row := range r.rows.v {
		if !gone[row.Capability] {
			caps = append(caps, [2]string{row.Capability, cmp.Or(changed[row.Capability], row.File)})
		}
	}
	return caps, product
}

// productText wraps one stored product rule as a text the reader takes.
const productText = "---\npromise: The product.\n---\n\n# Product\n\n## Rules\n\n"

// reasons classes each rule `--write` would write by the first kind of
// anchor that applies, a path's kind coming from the areas' split.
func (r record) reasons() Measure[ReasonCounts] {
	if why := cmp.Or(r.rows.why(noRows), r.areas.why(noAreasForRules)); why != "" {
		return cant[ReasonCounts](why)
	}
	kinds := map[string]sourceset.Kind{}
	for k, files := range r.areas.v.Split.Files {
		for _, f := range files {
			kinds[f] = k
		}
	}
	rc, counts, because := ReasonCounts{}, map[string]int{}, 0
	count := func(rules []capabilitytext.Entry) {
		for _, e := range rules {
			k := kindOf(e, kinds)
			counts[k], rc.Rules = counts[k]+1, rc.Rules+1
			if k == kindTest && hasBecause(e) {
				because++
			}
		}
	}
	caps, product := r.texts()
	for _, c := range caps {
		pc, err := capabilitytext.Parse(c[0], capabilitytext.Area{}, c[1], "")
		if err != nil {
			rc.CapabilitiesNotCounted++
			continue
		}
		count(pc.Kinded(mapfolder.Rule))
	}
	for _, text := range product {
		pc, err := capabilitytext.Parse("product", capabilitytext.Area{}, productText+strings.TrimSpace(text)+"\n", "")
		if rules := pc.Kinded(mapfolder.Rule); err == nil && len(rules) == 1 {
			count(rules)
			rc.ProductRules++
		} else {
			rc.ProductRulesNotCounted++
		}
	}
	if rc.Rules == 0 {
		return cant[ReasonCounts](noRules)
	}
	for _, k := range anchorKinds {
		rc.ByAnchor = append(rc.ByAnchor, AnchorCount{Kind: k[0], Ratio: ratio(counts[k[0]], rc.Rules)})
	}
	rc.TestOnlyWithBecause = share(because, counts[kindTest], noTestOnly)
	return given(rc)
}

// kindOf is the first kind of e's anchors that applies: a PR or change; a
// doc (a `from` path that is not a test and not a source file); a source
// file and a test; a test only; a source file only; none.
func kindOf(e capabilitytext.Entry, kinds map[string]sourceset.Kind) string {
	var src, test, doc bool
	for _, a := range e.Anchors {
		if a.Kind != mapfile.Doc {
			return kindPR
		}
		switch kinds[path.Clean(a.Value)] {
		case sourceset.Source:
			src = true
		case sourceset.Test:
			test = true
		default:
			doc = true
		}
	}
	switch {
	case doc:
		return kindDoc
	case src && test:
		return kindSourceTest
	case test:
		return kindTest
	case src:
		return kindSource
	default:
		return kindNone
	}
}

// hasBecause says whether e carries a `Because:` line with words after it.
func hasBecause(e capabilitytext.Entry) bool {
	for _, l := range e.Words() {
		if rest, ok := strings.CutPrefix(l, "Because:"); ok && strings.TrimSpace(rest) != "" {
			return true
		}
	}
	return false
}

// consolidation counts what the stored consolidation did, while current.
func (r record) consolidation() Measure[ConsolidationCounts] {
	if why := r.cons.why(noConsolidation); why != "" || !r.current {
		return cant[ConsolidationCounts](cmp.Or(why, staleCons))
	}
	res, rec := r.cons.v, r.cons.v.Record
	c := ConsolidationCounts{
		CapabilityMerges: len(rec.Merged), CapabilitiesMergedAway: len(res.MergedAway),
		Moved: len(rec.Moves), Dropped: len(rec.Dropped), Lifted: len(rec.Lifts), Refused: len(rec.Refused),
		EntriesBefore: res.Before.Entries, EntriesAfter: res.After.Entries, ProductRulesAfter: res.After.ProductRules,
	}
	for _, m := range rec.ExactMerges {
		c.EntriesMergedExact += len(m.Merged)
	}
	for _, m := range rec.EntryMerges {
		c.EntriesMergedNear += len(m.Merged)
	}
	return given(c)
}

// spend is each recorded phase's spend in the store's order, and the total.
func (r record) spend() Measure[SpendCounts] {
	if why := r.spent.why(noSpend); why != "" {
		return cant[SpendCounts](why)
	}
	sc, total, all := SpendCounts{}, PhaseSpend{Phase: "total"}, spend.Spend{}
	for _, p := range r.spent.v {
		sc.Phases = append(sc.Phases, phaseSpend(PhaseSpend{Phase: p.Phase, Input: p.Raw.Input, CachedInput: p.Raw.CachedInput, Output: p.Raw.Output}, p.Spend))
		total.Input, total.CachedInput, total.Output = total.Input+p.Raw.Input, total.CachedInput+p.Raw.CachedInput, total.Output+p.Raw.Output
		all = all.Add(p.Spend)
	}
	sc.Total = phaseSpend(total, all)
	return given(sc)
}

// phaseSpend is p with s's weighted tokens and cost; no money when unpriced.
func phaseSpend(p PhaseSpend, s spend.Spend) PhaseSpend {
	p.WeightedTokens, p.Unpriced, p.Cost = s.Tokens, s.Unpriced, s.Cost()
	if !s.Unpriced {
		p.MicroUSD = &s.MicroUSD
	}
	return p
}
