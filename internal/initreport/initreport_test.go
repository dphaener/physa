package initreport

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/consolidate"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/store"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func jsonOf(t *testing.T, r Report) string {
	t.Helper()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// golden compares got with testdata/name, or rewrites it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run go test ./internal/initreport -run Golden -update", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the report (run go test ./internal/initreport -run Golden -update if the change is meant):\n%s", path, diff(string(want), got))
	}
}

// diff is the lines of want and got that differ, by line number.
func diff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < max(len(w), len(g)); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			fmt.Fprintf(&b, "line %d:\n- %s\n+ %s\n", i+1, wl, gl)
		}
	}
	return b.String()
}

// TestGoldenFull pins the report of the whole fixture (fixture_test.go). The
// numbers, each reached by hand from the fixture:
//
//   - Source files: 12. In a capability 8 (cart 2, pay 2, sign-in 2,
//     sign-in-2 1, codegen 1): 8/12 = 67%. Shared 3 (lib/util.go named, and
//     lib/log/log.go and lib/log/format.go under the shared folder): 25%.
//     Unclaimed 1 (app/legacy.go): 1/12 = 8%.
//   - PRs placed: 101, 102 and 105, 3 of 6 (50%); nowhere with no files
//     known 1 (103), and with no file a capability owns 2 (104, 106).
//   - Changes no gathered PR holds: c7, c8, c9 and c11; placed c7 and c11,
//     2 of 4 (50%); nowhere with no files 1 (c9), no owner 1 (c8). Changes
//     that follow their PR: c1 to c6, 6.
//   - PRs in one capability: of the PRs touching an owned source file (101,
//     102, 105), 101 (cart) and 105 (pay) are in one, 102 (pay and sign-in)
//     is not: 2 of 3 (67%). Left out: 103, 104 and 106, 3.
//   - Capabilities with no PR or change: sign-in-2 and codegen, 2 of 5 (40%).
//   - Rules, from the current consolidation: cart totals-round, empty-cart,
//     line-order, removed-lines, provider-config; pay refund-cap,
//     charge-once, retry-webhooks; sign-in session-day, rate-limited,
//     keep-email; and the one product rule that parses, logged: 12 rules,
//     with 1 product rule not counted. A PR or change 4 (totals-round,
//     retry-webhooks by its commit, keep-email, logged): 33%. A doc 2
//     (provider-config, from an other file; refund-cap, a doc before a
//     source file): 17%. A source file and a test 1 (line-order): 8%. A test
//     only 3 (empty-cart, removed-lines, rate-limited): 25%. A source file
//     only 1 (charge-once): 8%. None 1 (session-day): 8%. Test-only rules
//     carrying a Because: line: empty-cart and rate-limited, 2 of 3 (67%).
//   - Consolidation: 1 merge group, 1 merged away; 1 exact and 1 near
//     entry merged; 1 moved; 1 question dropped; 2 lifted; 2 refused;
//     entries 19 before, 12 after, and 2 product rules.
//   - Spend, in run order: discover, areas, walk, recover, consolidate, then
//     the total of 159200 input, 47300 cached input, 22900 output and 274k
//     weighted tokens, with no price, since areas has none.
func TestGoldenFull(t *testing.T) {
	f := full(t)
	if cur, err := consolidate.Current(t.Context(), f.st); err != nil || !cur {
		t.Fatalf("the fixture's consolidation is not current (%v, %v): consolidate's fingerprint has changed; make fingerprint in fixture_test.go match it", cur, err)
	}
	r := f.report()
	checkNumbers(t, r)
	golden(t, "full.txt", Text(r))
	golden(t, "full.md", Section(r))
	golden(t, "full.json", jsonOf(t, r))
}

// checkNumbers holds the report of the full fixture to the numbers
// TestGoldenFull reaches by hand, so -update can't pin a wrong one.
func checkNumbers(t *testing.T, r Report) {
	t.Helper()
	if r.Files.Value == nil || r.Placed.Value == nil || r.Placed.Value.PRs.Value == nil || r.Placed.Value.Changes.Value == nil ||
		r.OneCapability.Value == nil || r.Unplaced.Value == nil || r.Reasons.Value == nil || r.Reasons.Value.TestOnlyWithBecause.Value == nil ||
		r.Consolidation.Value == nil || r.Spend.Value == nil {
		t.Fatalf("a measure of the full fixture can't be given:\n%s", Text(r))
	}
	ratio := func(n, of, pct int) Ratio { return Ratio{n, of, pct} }
	type nums struct{ got, want any }
	fc, pc, rc, sc := *r.Files.Value, *r.Placed.Value, *r.Reasons.Value, *r.Spend.Value
	byAnchor := map[string]Ratio{}
	for _, a := range rc.ByAnchor {
		byAnchor[a.Kind] = a.Ratio
	}
	for name, n := range map[string]nums{
		"source files":    {fc.SourceFiles, 12},
		"in a capability": {fc.InCapability, ratio(8, 12, 67)},
		"shared":          {fc.Shared, ratio(3, 12, 25)},
		"unclaimed":       {fc.Unclaimed, ratio(1, 12, 8)},
		"PRs placed":      {*pc.PRs.Value, PlacingCounts{ratio(3, 6, 50), []Nowhere{{place.NoFiles, 1}, {place.NoOwner, 2}}}},
		"changes placed":  {*pc.Changes.Value, PlacingCounts{ratio(2, 4, 50), []Nowhere{{place.NoFiles, 1}, {place.NoOwner, 1}}}},
		"follow PR":       {pc.FollowTheirPR, 6},
		"in one":          {*r.OneCapability.Value, OneCounts{ratio(2, 3, 67), 3}},
		"no PR":           {*r.Unplaced.Value, ratio(2, 5, 40)},
		"rules":           {[3]int{rc.Rules, rc.ProductRules, rc.ProductRulesNotCounted}, [3]int{12, 1, 1}},
		"by anchor": {byAnchor, map[string]Ratio{kindPR: ratio(4, 12, 33), kindDoc: ratio(2, 12, 17), kindSourceTest: ratio(1, 12, 8),
			kindTest: ratio(3, 12, 25), kindSource: ratio(1, 12, 8), kindNone: ratio(1, 12, 8)}},
		"because":       {*rc.TestOnlyWithBecause.Value, ratio(2, 3, 67)},
		"consolidation": {*r.Consolidation.Value, ConsolidationCounts{1, 1, 1, 1, 1, 1, 2, 2, 19, 12, 2}},
		"spend total":   {[4]int64{sc.Total.Input, sc.Total.CachedInput, sc.Total.Output, sc.Total.WeightedTokens}, [4]int64{159200, 47300, 22900, 274200}},
	} {
		if !reflect.DeepEqual(n.got, n.want) {
			t.Errorf("%s = %+v; want %+v", name, n.got, n.want)
		}
	}
	var phases []string
	for _, p := range sc.Phases {
		phases = append(phases, p.Phase)
	}
	if want := []string{"discover", "areas", "walk", "recover", "consolidate"}; !reflect.DeepEqual(phases, want) {
		t.Errorf("phases = %v; want %v", phases, want)
	}
	if p := sc.Phases[1]; !p.Unpriced || p.MicroUSD != nil || sc.Total.MicroUSD != nil || !sc.Total.Unpriced {
		t.Errorf("an unpriced phase and the total must say so and carry no money: %+v, total %+v", p, sc.Total)
	}
}

// TestGoldenProposalPath pins the report of a run on the proposal path:
// every measure of the walk says why it can't be given, and spend is whole.
func TestGoldenProposalPath(t *testing.T) {
	r := proposalPath(t).report()
	golden(t, "proposal.txt", Text(r))
	golden(t, "proposal.md", Section(r))
	golden(t, "proposal.json", jsonOf(t, r))
}

// want is a measure that can't be given: where its JSON sits, its label in
// the lines, and the reason.
type want struct {
	key    []string
	label  string
	reason string
}

var (
	files         = []string{"files"}
	placed        = []string{"placed"}
	placedPRs     = []string{"placed", "prs"}
	placedChanges = []string{"placed", "changes_on_their_own"}
	oneCapability = []string{"prs_in_one_capability"}
	unplaced      = []string{"capabilities_with_no_pr_or_change"}
	reasons       = []string{"rules_reasons"}
	because       = []string{"rules_reasons", "test_only_with_because"}
	consolidation = []string{"consolidation"}
	spent         = []string{"spend"}
)

const (
	filesLabel   = "Source files"
	placedLabel  = "PRs and changes placed"
	oneLabel     = "PRs in one capability"
	noPRLabel    = "Capabilities with no PR or change"
	rulesLabel   = "Rules, by where their reasons came from"
	consLabel    = "Consolidation"
	spendLabel   = "Spend per phase"
	becauseLabel = "test-only rules carrying a Because: line"
)

// zero is a zero standing in for a number: "0", "0%" or "0 of 0".
var zero = regexp.MustCompile(`(^|[^0-9])0([^0-9]|$)`)

// cantBeGiven checks that each of ws says its reason in Text, in Section
// (spend is not there) and in the JSON, with no zero in its place, and
// that every measure not in ws carries numbers.
func cantBeGiven(t *testing.T, r Report, ws ...want) {
	t.Helper()
	text, section := Text(r), Section(r)
	var doc map[string]any
	if err := json.Unmarshal([]byte(jsonOf(t, r)), &doc); err != nil {
		t.Fatal(err)
	}
	t.Logf("\n%s", text)
	named := map[string]bool{}
	for _, w := range ws {
		named[strings.Join(w.key, ".")] = true
		ln := w.label + ": can't be given: " + w.reason + "."
		found := ""
		for _, l := range strings.Split(text, "\n") {
			if strings.TrimSpace(l) == ln {
				found = l
			}
		}
		if found == "" {
			t.Errorf("Text has no line %q:\n%s", ln, text)
		} else if zero.MatchString(found) {
			t.Errorf("Text's line %q holds a zero", found)
		}
		inSection := strings.Contains(section, "- "+ln+"\n")
		if w.label == spendLabel && inSection || w.label != spendLabel && !inSection {
			t.Errorf("Section and %q (spend is never in it):\n%s", ln, section)
		}
		var v any = doc
		for _, k := range w.key {
			v = v.(map[string]any)[k]
		}
		if got, ok := v.(map[string]any); !ok || !reflect.DeepEqual(got, map[string]any{"cant_be_given": w.reason}) {
			t.Errorf("JSON %s = %v; want only cant_be_given %q, no number", strings.Join(w.key, "."), v, w.reason)
		}
	}
	for _, k := range []string{"files", "placed", "prs_in_one_capability", "capabilities_with_no_pr_or_change", "rules_reasons", "consolidation", "spend"} {
		if m := doc[k].(map[string]any); !named[k] && m["cant_be_given"] != nil {
			t.Errorf("JSON %s can't be given (%v), but this case gives it", k, m["cant_be_given"])
		}
	}
}

func TestCantBeGiven(t *testing.T) {
	t.Run("proposal path", func(t *testing.T) {
		cantBeGiven(t, proposalPath(t).report(),
			want{files, filesLabel, noAreas},
			want{placed, placedLabel, noPlaceNoWalk},
			want{oneCapability, oneLabel, noWalk},
			want{unplaced, noPRLabel, noWalk},
			want{reasons, rulesLabel, noAreasForRules},
			want{consolidation, consLabel, noConsolidation})
	})
	t.Run("no walk", func(t *testing.T) {
		f := newFixture(t).gather(shopGather()).areas().spent()
		cantBeGiven(t, f.report(),
			want{files, filesLabel, noWalk},
			want{placed, placedLabel, noPlaceNoWalk},
			want{oneCapability, oneLabel, noWalk},
			want{unplaced, noPRLabel, noWalk},
			want{reasons, rulesLabel, noRows},
			want{consolidation, consLabel, noConsolidation})
	})
	t.Run("no placements", func(t *testing.T) {
		f := newFixture(t).gather(shopGather()).areas().walk().recovered(recoveredRows(walkAt)).spent()
		cantBeGiven(t, f.report(),
			want{placed, placedLabel, noPlace},
			want{oneCapability, oneLabel, noPlace},
			want{unplaced, noPRLabel, noPlace},
			want{consolidation, consLabel, noConsolidation})
	})
	t.Run("no recovery", func(t *testing.T) {
		f := newFixture(t).gather(shopGather()).areas().walk().place().spent()
		cantBeGiven(t, f.report(),
			want{reasons, rulesLabel, noRows},
			want{consolidation, consLabel, noConsolidation})
	})
	t.Run("no consolidation", func(t *testing.T) {
		f := newFixture(t).gather(shopGather()).areas().walk().place().recovered(recoveredRows(walkAt)).spent()
		cantBeGiven(t, f.report(), want{consolidation, consLabel, noConsolidation})
	})
	t.Run("stale consolidation", func(t *testing.T) {
		f := full(t)
		f.recovered([]initrecord.Recovered{{Capability: "codegen", ProposalAt: walkAt, At: walkAt.Add(4 * 3600e9), Status: "recovered",
			File: capText("codegen", "Code is generated.", []string{rule("Generated code is never edited", "never-edit", "from tools/gen.go", "the generator overwrites it")}, nil, nil)}})
		r := f.report()
		cantBeGiven(t, r, want{consolidation, consLabel, staleCons})
		// With the consolidation stale, the rules are the recovered rows':
		// cart 5, pay 6, sign-in 3, sign-in-2 3 and codegen 1.
		if rc := r.Reasons.Value; rc == nil || rc.Rules != 18 || rc.ProductRules != 0 {
			t.Errorf("rules = %+v; want 18 from the rows and none from the product", rc)
		}
	})
	t.Run("no spend", func(t *testing.T) {
		rows := recoveredRows(walkAt)
		f := newFixture(t).gather(shopGather()).areas().walk().place().recovered(rows).consolidated(rows)
		cantBeGiven(t, f.report(), want{spent, spendLabel, noSpend})
	})
	t.Run("empty store", func(t *testing.T) {
		cantBeGiven(t, newFixture(t).report(),
			want{files, filesLabel, noAreas},
			want{placed, placedLabel, noGather},
			want{oneCapability, oneLabel, noGather},
			want{unplaced, noPRLabel, noWalk},
			want{reasons, rulesLabel, noRows},
			want{consolidation, consLabel, noConsolidation},
			want{spent, spendLabel, noSpend})
	})
	t.Run("no PR owns a source file and every change is in a PR", func(t *testing.T) {
		g := store.Gather{PRs: []store.GatheredPR{{Number: 7}, {Number: 8}},
			Changes: []store.GatheredChange{change("a", "Tidy (#7)", "lib/util.go"), change("b", "Docs (#8)", "docs/pay.md")}}
		r := newFixture(t).gather(g).areas().walk().place().report()
		cantBeGiven(t, r,
			want{placedChanges, "Changes no gathered PR holds, placed", noFreeChanges},
			want{oneCapability, oneLabel, noOwnedPR},
			want{reasons, rulesLabel, noRows},
			want{consolidation, consLabel, noConsolidation},
			want{spent, spendLabel, noSpend})
		if p := r.Placed.Value.PRs.Value; p == nil || p.Ratio != (Ratio{1, 2, 50}) {
			t.Errorf("PRs placed = %+v; want 1 of 2 (#8 by the doc pay names)", p)
		}
	})
	t.Run("no PR gathered", func(t *testing.T) {
		g := store.Gather{Changes: []store.GatheredChange{change("a", "Cart", "app/cart.go")}}
		cantBeGiven(t, newFixture(t).gather(g).areas().walk().place().report(),
			want{placedPRs, "PRs placed", noPRs},
			want{oneCapability, oneLabel, noOwnedPR},
			want{reasons, rulesLabel, noRows},
			want{consolidation, consLabel, noConsolidation},
			want{spent, spendLabel, noSpend})
	})
	t.Run("no rule", func(t *testing.T) {
		rows := []initrecord.Recovered{{Capability: "cart", ProposalAt: walkAt, Status: "recovered", File: capText("cart", "People fill a cart.", nil, []string{cartDecision}, nil)}}
		cantBeGiven(t, newFixture(t).gather(shopGather()).areas().walk().place().recovered(rows).spent().report(),
			want{reasons, rulesLabel, noRules},
			want{consolidation, consLabel, noConsolidation})
	})
	t.Run("no test-only rule", func(t *testing.T) {
		rows := recoveredRows(walkAt)[1:2] // pay's rules: none rests on tests only
		r := newFixture(t).gather(shopGather()).areas().walk().place().recovered(rows).spent().report()
		cantBeGiven(t, r, want{consolidation, consLabel, noConsolidation})
		ln := becauseLabel + ": can't be given: " + noTestOnly + "."
		if !strings.Contains(Text(r), "  "+ln+"\n") || !strings.Contains(Section(r), "  - "+ln+"\n") {
			t.Errorf("no line %q in\n%s\n%s", ln, Text(r), Section(r))
		}
		var doc map[string]any
		_ = json.Unmarshal([]byte(jsonOf(t, r)), &doc)
		if got := doc[because[0]].(map[string]any)[because[1]]; !reflect.DeepEqual(got, map[string]any{"cant_be_given": noTestOnly}) {
			t.Errorf("JSON %v = %v", because, got)
		}
	})
}

// pathLike is a token holding a slash between two other characters.
var pathLike = regexp.MustCompile(`[^\s/]/[^\s/]`)

func TestNoPathInTheReport(t *testing.T) {
	for name, r := range map[string]Report{"full": full(t).report(), "proposal path": proposalPath(t).report()} {
		for kind, out := range map[string]string{"Text": Text(r), "Section": Section(r), "JSON": jsonOf(t, r)} {
			for _, p := range fixturePaths() {
				if strings.Contains(out, p) {
					t.Errorf("%s %s holds the path %q", name, kind, p)
				}
			}
			if m := pathLike.FindString(out); m != "" {
				t.Errorf("%s %s holds a path-like token around %q", name, kind, m)
			}
		}
	}
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join("testdata", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range fixturePaths() {
			if strings.Contains(string(b), p) {
				t.Errorf("testdata/%s holds the path %q", e.Name(), p)
			}
		}
	}
}

func TestBodyThatDoesNotDecode(t *testing.T) {
	t.Run("placements", func(t *testing.T) {
		f := full(t)
		f.check(f.st.PutPlace(t.Context(), initrecord.Place{WalkAt: walkAt, At: walkAt, Body: []byte("{not json")}))
		r := f.report()
		cantBeGiven(t, r,
			want{placed, placedLabel, badPlace},
			want{oneCapability, oneLabel, badPlace},
			want{unplaced, noPRLabel, badPlace})
		if whole := full(t).report(); !reflect.DeepEqual(r.Files, whole.Files) || !reflect.DeepEqual(r.Reasons, whole.Reasons) ||
			!reflect.DeepEqual(r.Consolidation, whole.Consolidation) || !reflect.DeepEqual(r.Spend, whole.Spend) {
			t.Errorf("a placement that does not decode changed the other measures")
		}
	})
	t.Run("areas", func(t *testing.T) {
		f := full(t)
		if _, err := f.st.DB().ExecContext(t.Context(), `UPDATE init_areas SET body = '[broken' WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
		r := f.report()
		cantBeGiven(t, r,
			want{files, filesLabel, badAreas},
			want{reasons, rulesLabel, badAreas})
		if whole := full(t).report(); !reflect.DeepEqual(r.Placed, whole.Placed) || !reflect.DeepEqual(r.OneCapability, whole.OneCapability) ||
			!reflect.DeepEqual(r.Unplaced, whole.Unplaced) || !reflect.DeepEqual(r.Consolidation, whole.Consolidation) || !reflect.DeepEqual(r.Spend, whole.Spend) {
			t.Errorf("areas that do not decode changed the other measures")
		}
	})
}
