// Package initreport gives the measures of an init run from init's record
// alone: files in a capability, shared and unclaimed; PRs and changes placed;
// PRs in one capability; capabilities with no PR or change; where the rules'
// reasons came from; consolidation's counts; and spend per phase. It reads
// the record with no session, no repo files, no forge and no environment. A
// measure whose inputs are not stored, are stale or cannot be read says why
// it can't be given and holds no number. It renders the measures as plain
// text, as a section of the PR body and as JSON, in counts and shares only:
// no file path, capability name or PR number reaches it.
package initreport

import (
	"context"
	"encoding/json"

	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/consolidate"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/recoverinput"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

// Report is every measure of an init run.
type Report struct {
	Files         Measure[FileCounts]          `json:"files"`
	Placed        Measure[PlacedCounts]        `json:"placed"`
	OneCapability Measure[OneCounts]           `json:"prs_in_one_capability"`
	Unplaced      Measure[Ratio]               `json:"capabilities_with_no_pr_or_change"`
	Reasons       Measure[ReasonCounts]        `json:"rules_reasons"`
	Consolidation Measure[ConsolidationCounts] `json:"consolidation"`
	Spend         Measure[SpendCounts]         `json:"spend"`
}

// Measure is a measure's numbers in Value, or, when Value is nil, why it
// can't be given, marshalled as {"cant_be_given": why} and no number.
type Measure[T any] struct {
	CantBeGiven string
	Value       *T
}

// MarshalJSON writes the numbers, or only the reason they can't be given.
func (m Measure[T]) MarshalJSON() ([]byte, error) {
	if m.Value == nil {
		return json.Marshal(map[string]string{"cant_be_given": m.CantBeGiven})
	}
	return json.Marshal(m.Value)
}

func given[T any](v T) Measure[T] { return Measure[T]{Value: &v} }

func cant[T any](why string) Measure[T] { return Measure[T]{CantBeGiven: why} }

// Ratio is a count of a total that is not zero, and its whole percent.
type Ratio struct {
	Count   int `json:"count"`
	Of      int `json:"of"`
	Percent int `json:"percent"`
}

// FileCounts is the areas' source files: owned by one walk capability,
// shared, or unclaimed, each a share of them all.
type FileCounts struct {
	SourceFiles  int   `json:"source_files"`
	InCapability Ratio `json:"in_a_capability"`
	Shared       Ratio `json:"shared"`
	Unclaimed    Ratio `json:"unclaimed"`
}

// PlacedCounts is the gathered PRs placed, the changes no gathered PR holds
// placed on their own, and the changes that follow their PR.
type PlacedCounts struct {
	PRs           Measure[PlacingCounts] `json:"prs"`
	Changes       Measure[PlacingCounts] `json:"changes_on_their_own"`
	FollowTheirPR int                    `json:"changes_following_their_pr"`
}

// PlacingCounts is items placed of those gathered, and those placed nowhere.
type PlacingCounts struct {
	Ratio
	Nowhere []Nowhere `json:"nowhere"`
}

// Nowhere is how many items are placed nowhere for one reason.
type Nowhere struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// OneCounts is the gathered PRs whose source files a capability owns all
// belong to one, of the PRs touching such a file, and the PRs left out.
type OneCounts struct {
	Ratio
	LeftOut int `json:"left_out"`
}

// ReasonCounts is the rules `--write` would write by the first kind of
// anchor that applies, every kind in order; ProductRules are among Rules.
type ReasonCounts struct {
	Rules                  int            `json:"rules"`
	ByAnchor               []AnchorCount  `json:"by_anchor"`
	TestOnlyWithBecause    Measure[Ratio] `json:"test_only_with_because"`
	ProductRules           int            `json:"product_rules,omitempty"`
	ProductRulesNotCounted int            `json:"product_rules_not_counted,omitempty"`
	CapabilitiesNotCounted int            `json:"capabilities_not_counted,omitempty"`
}

// AnchorCount is the rules of one kind of anchor, a share of all rules.
type AnchorCount struct {
	Kind string `json:"kind"`
	Ratio
}

// ConsolidationCounts is what the stored consolidation did.
type ConsolidationCounts struct {
	CapabilityMerges       int `json:"capability_merges"`
	CapabilitiesMergedAway int `json:"capabilities_merged_away"`
	EntriesMergedExact     int `json:"entries_merged_exact"`
	EntriesMergedNear      int `json:"entries_merged_near"`
	Moved                  int `json:"rules_moved"`
	Dropped                int `json:"questions_dropped"`
	Lifted                 int `json:"rules_lifted"`
	Refused                int `json:"refused"`
	EntriesBefore          int `json:"entries_before"`
	EntriesAfter           int `json:"entries_after"`
	ProductRulesAfter      int `json:"product_rules_after"`
}

// SpendCounts is what each phase spent, in the store's order, and the total.
type SpendCounts struct {
	Phases []PhaseSpend `json:"phases"`
	Total  PhaseSpend   `json:"total"`
}

// PhaseSpend is one phase's raw tokens, weighted tokens and cost estimate.
// MicroUSD is left out when some of it has no price, and Cost says so.
type PhaseSpend struct {
	Phase          string `json:"phase"`
	Input          int64  `json:"input"`
	CachedInput    int64  `json:"cached_input"`
	Output         int64  `json:"output"`
	WeightedTokens int64  `json:"weighted_tokens"`
	MicroUSD       *int64 `json:"micro_usd,omitempty"`
	Unpriced       bool   `json:"unpriced,omitempty"`
	Cost           string `json:"cost"`
}

// stored is one input of init's record: ok when stored and read, bad
// saying why when stored and unreadable.
type stored[T any] struct {
	v   T
	ok  bool
	bad string
}

func got[T any](v T, ok bool, err error, bad string) stored[T] {
	if err != nil {
		return stored[T]{bad: bad}
	}
	return stored[T]{v: v, ok: ok}
}

// why is "" when s is read, else bad or absent.
func (s stored[T]) why(absent string) string {
	if s.bad != "" || s.ok {
		return s.bad
	}
	return absent
}

// record is what the measures read from init's record.
type record struct {
	gather  stored[store.Gather]
	areas   stored[areas.Record]
	walk    stored[walk.Record]
	place   stored[place.Record]
	rows    stored[[]initrecord.Recovered] // the rows --write would write, each with a file
	cons    stored[consolidate.Result]
	current bool
	spent   stored[[]initrecord.Spent]
}

// Load gives every measure of st's init record. It never fails as a whole:
// a measure whose inputs are missing, stale or unreadable says why instead.
func Load(ctx context.Context, st *initrecord.Store) Report {
	var r record
	g, ok, err := st.Gathered(ctx)
	r.gather = got(g, ok, err, badGather)
	a, ok, err := areas.Load(ctx, st)
	r.areas = got(a, ok, err, badAreas)
	w, ok, err := walk.Load(ctx, st)
	r.walk = got(w, ok, err, badWalk)
	p, ok, err := place.Load(ctx, st)
	r.place = got(p, ok, err, badPlace)
	rows, err := written(ctx, st)
	r.rows = got(rows, len(rows) > 0, err, badRows)
	c, ok, err := consolidate.Load(ctx, st)
	if err == nil && ok {
		r.current, err = consolidate.Current(ctx, st)
	}
	r.cons = got(c, ok, err, badConsolidation)
	s, err := st.Spending(ctx)
	r.spent = got(s, len(s) > 0, err, badSpend)
	return Report{Files: r.files(), Placed: r.placed(), OneCapability: r.oneCapability(), Unplaced: r.unplaced(),
		Reasons: r.reasons(), Consolidation: r.consolidation(), Spend: r.spend()}
}

// written is the recovered rows with a file that `--write` would write:
// those of the stored walk when placements are stored, else every row.
func written(ctx context.Context, st *initrecord.Store) ([]initrecord.Recovered, error) {
	src, _, err := recoverinput.Listed(ctx, st, mapping.Proposal{}, true)
	if err != nil {
		return nil, err
	}
	rows, err := st.Recovered(ctx)
	var out []initrecord.Recovered
	for _, row := range rows {
		if row.File != "" && src.Holds(row.ProposalAt) {
			out = append(out, row)
		}
	}
	return out, err
}
