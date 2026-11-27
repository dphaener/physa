package initreport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/consolidate"
	"github.com/dphaener/physa/internal/consolidating"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/productpromise"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

// The fixture is a small shop: five capabilities walked from four areas.
//
// Source files (12): app/cart.go, app/cart_total.go, app/pay.go,
// app/refund.go, app/legacy.go, web/login.go, web/session.go, web/form.go,
// lib/util.go, lib/log/log.go, lib/log/format.go, tools/gen.go.
// Tests: app/cart_test.go, app/pay_test.go, web/login_test.go. Docs:
// docs/pay.md, README.md. Other: config/app.yml.
//
// The walk: cart (area shop) owns app/cart.go and app/cart_total.go; pay
// (shop) owns app/pay.go and app/refund.go and names docs/pay.md; sign-in
// (accounts) owns web/login.go and web/session.go; sign-in-2 (web, renamed
// from sign-in) owns web/form.go; codegen (platform) owns tools/gen.go. It
// names lib/util.go shared, and the folder lib/log/ shared. app/legacy.go is
// left unclaimed, as an older walk could leave it.
var (
	gatherAt   = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	proposalAt = gatherAt.Add(30 * time.Minute) // when an earlier physa stored a proposal, for rows recovered from it
	areasAt    = gatherAt.Add(time.Hour)
	walkAt     = gatherAt.Add(2 * time.Hour)

	sourceFiles = []string{"app/cart.go", "app/cart_total.go", "app/pay.go", "app/refund.go", "app/legacy.go",
		"web/login.go", "web/session.go", "web/form.go", "lib/util.go", "lib/log/log.go", "lib/log/format.go", "tools/gen.go"}
	testFiles  = []string{"app/cart_test.go", "app/pay_test.go", "web/login_test.go"}
	docFiles   = []string{"docs/pay.md", "README.md"}
	otherFiles = []string{"config/app.yml"}
	sharedDir  = "lib/log/"
)

// fixturePaths is every path the fixture's record holds.
func fixturePaths() []string {
	return append(append(append(append([]string{sharedDir}, sourceFiles...), testFiles...), docFiles...), otherFiles...)
}

// fixture writes init's record into a store of its own, one phase at a
// time, in the order a run stores them.
type fixture struct {
	t  *testing.T
	st *initrecord.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s, err := store.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &fixture{t: t, st: initrecord.Of(s)}
}

func (f *fixture) check(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) report() Report { return Load(f.t.Context(), f.st) }

func change(id, subject string, files ...string) store.GatheredChange {
	return store.GatheredChange{ID: id, Short: id, Subject: subject, At: gatherAt, Files: files}
}

// shopGather is six PRs and ten changes.
//
//	#101 holds c1 (app/cart.go, app/cart_total.go, app/cart_test.go) and c2
//	     (app/cart_total.go): cart owns every source file, so it is placed on
//	     cart and is in one capability.
//	#102 holds c3 (app/pay.go, web/login.go): pay and sign-in each weigh 1,
//	     pay is taken first by name and covers half, so it is placed on pay;
//	     its source files belong to two capabilities, so it is not in one.
//	#103 holds no change: placed nowhere, no files known; left out.
//	#104 holds c4 (lib/util.go, shared): placed nowhere, no capability owns
//	     it; left out.
//	#105 holds c5 (app/refund.go, docs/pay.md): placed on pay, in one.
//	#106 holds c6 (README.md, a doc no footprint names): placed nowhere, no
//	     capability owns it; left out.
//
// c7 (web/session.go) and c11 (app/cart.go) are held by no PR and placed on
// sign-in and cart; c8 (config/app.yml) is placed nowhere, no capability
// owns it; c9 has no files and is placed nowhere, no files known.
func shopGather() store.Gather {
	return store.Gather{
		Run: store.GatherRun{At: gatherAt, Forge: "github"},
		PRs: []store.GatheredPR{{Number: 101, Title: "Cart totals"}, {Number: 102, Title: "Pay and sign in"}, {Number: 103, Title: "No change"},
			{Number: 104, Title: "Tidy util"}, {Number: 105, Title: "Refunds"}, {Number: 106, Title: "Readme"}},
		Changes: []store.GatheredChange{
			change("c1", "Add cart totals (#101)", "app/cart.go", "app/cart_total.go", "app/cart_test.go"),
			change("c2", "Fix cart rounding (#101)", "app/cart_total.go"),
			change("c3", "Pay and sign in together (#102)", "app/pay.go", "web/login.go"),
			change("c4", "Tidy util (#104)", "lib/util.go"),
			change("c5", "Refund flow (#105)", "app/refund.go", "docs/pay.md"),
			change("c6", "Session docs (#106)", "README.md"),
			change("c7", "Speed up login", "web/session.go"),
			change("c8", "Update config", "config/app.yml"),
			change("c9", "An empty change"),
			change("c11", "Tweak the cart", "app/cart.go"),
		},
	}
}

func (f *fixture) gather(g store.Gather) *fixture {
	f.check(f.st.PutGather(f.t.Context(), g))
	return f
}

func (f *fixture) areas() *fixture {
	rec := areas.Record{
		Split: sourceset.Division{Files: map[sourceset.Kind][]string{
			sourceset.Source: sourceFiles, sourceset.Test: testFiles, sourceset.Doc: docFiles, sourceset.Other: otherFiles,
		}},
		Areas: []areas.AreaRecord{
			{Name: "shop", Promise: "People buy.", Files: sourceFiles[:5]},
			{Name: "accounts", Promise: "People sign in.", Files: sourceFiles[5:7]},
			{Name: "web", Promise: "People use the site.", Files: sourceFiles[7:8]},
			{Name: "platform", Promise: "The shop runs.", Files: sourceFiles[8:]},
		},
	}
	body, err := json.Marshal(rec)
	f.check(err)
	f.check(f.st.PutAreas(f.t.Context(), initrecord.Areas{GatherAt: gatherAt, At: areasAt, Model: "m", Body: body}))
	return f
}

func walked(name, area, promise string, sources, tests, docs []string) walk.CapabilityRecord {
	return walk.CapabilityRecord{Capability: walk.Capability{Name: name, Promise: promise, Sources: sources, Tests: tests, Docs: docs},
		Area: area, Entries: walk.Entries{Tests: tests, Docs: docs}}
}

// walkOrder is the walk's capabilities, in walk order.
var walkOrder = []string{"cart", "pay", "sign-in", "sign-in-2", "codegen"}

func (f *fixture) walk() *fixture {
	rec := walk.Record{
		Capabilities: []walk.CapabilityRecord{
			walked("cart", "shop", "People fill a cart.", []string{"app/cart.go", "app/cart_total.go"}, []string{"app/cart_test.go"}, nil),
			walked("pay", "shop", "People pay for their cart.", []string{"app/pay.go", "app/refund.go"}, []string{"app/pay_test.go"}, []string{"docs/pay.md"}),
			walked("sign-in", "accounts", "People sign in.", []string{"web/login.go", "web/session.go"}, []string{"web/login_test.go"}, nil),
			walked("sign-in-2", "web", "People sign in from the form.", []string{"web/form.go"}, nil, nil),
			walked("codegen", "platform", "Code is generated.", []string{"tools/gen.go"}, nil, nil),
		},
		Shared: []walk.SharedRecord{
			{Shared: walk.Shared{Path: "lib/util.go", Reason: "every area uses it"}, Area: "platform"},
			{Shared: walk.Shared{Path: sharedDir, Reason: "logging serves every area"}, Area: "platform"},
		},
		Renamed: []walk.Rename{{Area: "web", From: "sign-in", To: "sign-in-2"}},
	}
	body, err := json.Marshal(rec)
	f.check(err)
	f.check(f.st.PutWalk(f.t.Context(), initrecord.Walk{AreasAt: areasAt, At: walkAt, Model: "m", Body: body}))
	return f
}

// place stores the placements place's own phase makes of the stored
// gather, areas and walk.
func (f *fixture) place() *fixture {
	_, err := place.Run(f.t.Context(), f.st)
	f.check(err)
	return f
}

// rule is a rule's text; an empty anchors leaves the meta line with its
// slug alone, and an empty because leaves out the Because: line.
func rule(heading, slug, anchors, because string) string {
	meta := "`" + slug + "`"
	if anchors != "" {
		meta += " · " + anchors
	}
	s := "### " + heading + "\n" + meta + "\n" + heading + ".\n"
	if because != "" {
		s += "Because: " + because + "\n"
	}
	return s
}

// capText is a capability's single text as recovery writes it.
func capText(name, promise string, rules, decisions, questions []string) string {
	b := "# " + strings.ToUpper(name[:1]) + name[1:] + "\n\n"
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

// The rules recovered, and the kind of anchor each is classed by.
var (
	totalsRound   = rule("Totals round half up", "totals-round", "#101", "the PR says shoppers asked for it")        // a PR or change
	emptyCart     = rule("An empty cart totals nothing", "empty-cart", "from app/cart_test.go", "a regression test") // a test only, with a reason
	lineOrder     = rule("Cart lines keep their order", "line-order", "from app/cart.go · from app/cart_test.go", "the test pins it")
	removedLines  = rule("Totals leave out removed lines", "removed-lines", "from app/cart_test.go", "") // a test only, with no reason
	cartLogged    = rule("Every change is logged", "logged", "#101", "support reads the log")
	providerRule  = rule("Config names the provider", "provider-config", "from config/app.yml", "an other file, counted as a doc")
	refundCap     = rule("Refunds never pass the charge", "refund-cap", "from docs/pay.md · from app/pay.go", "the doc says so")
	chargeOnce    = rule("A payment is charged once", "charge-once", "from app/pay.go", "the code guards it")
	chargeOnce2   = rule("A payment is charged once", "charge-once-2", "from app/pay.go", "the code guards it")
	retryHooks    = rule("Webhooks are retried", "retry-webhooks", "abc1234", "the change says the provider drops them")
	payLogged     = rule("Every change is logged", "logged-2", "#105", "support reads the log")
	sessionDay    = rule("Sessions end after a day", "session-day", "", "the team wants it") // no anchor
	rateLimited   = rule("Login is rate limited", "rate-limited", "from web/login_test.go", "a test for an attack")
	signInAudit   = rule("Sign-ins are audited", "audit", "from web/login.go", "the code writes an audit row")
	rateLimit     = rule("Login is rate limited", "rate-limit", "from web/login_test.go", "a test for an attack")
	keepEmail     = rule("The form keeps the email", "keep-email", "#102", "the PR asked for it")
	formAudit     = rule("Sign-ins are audited", "audit-2", "from web/form.go", "the code writes an audit row")
	cartDecision  = "### The cart lives in the session\n`cart-in-session` · #101\nThe session holds it.\nBecause: no account is needed.\n"
	guestQuestion = "### Do guest carts expire?\n`guest-carts` · #101\nNo one has said.\n"
)

// recoveredRows is each walk capability's recovery, made at at: cart (5
// rules, 1 decision, 1 question), pay (6 rules), sign-in (3 rules) and
// sign-in-2 (3 rules) are recovered, 19 entries in all; codegen is not yet.
func recoveredRows(at time.Time) []initrecord.Recovered {
	row := func(name, file string) initrecord.Recovered {
		return initrecord.Recovered{Capability: name, ProposalAt: at, At: at.Add(time.Hour), Status: recovery.StatusRecovered, File: file, SessionID: "rec-" + name, Model: "m"}
	}
	return []initrecord.Recovered{
		row("cart", capText("cart", "People fill a cart.", []string{totalsRound, emptyCart, lineOrder, removedLines, cartLogged}, []string{cartDecision}, []string{guestQuestion})),
		row("pay", capText("pay", "People pay for their cart.", []string{refundCap, chargeOnce, chargeOnce2, retryHooks, providerRule, payLogged}, nil, nil)),
		row("sign-in", capText("sign-in", "People sign in.", []string{sessionDay, rateLimited, signInAudit}, nil, nil)),
		row("sign-in-2", capText("sign-in-2", "People sign in from the form.", []string{rateLimit, keepEmail, formAudit}, nil, nil)),
		{Capability: "codegen", ProposalAt: at, At: at.Add(time.Hour), Status: recovery.StatusNotYet},
	}
}

func (f *fixture) recovered(rows []initrecord.Recovered) *fixture {
	f.check(f.st.PutRecovered(f.t.Context(), rows))
	return f
}

// fingerprint is the fingerprint consolidation keeps of what it was made
// from, as consolidate's read makes it: the walk's time, the product's
// promise (the fixture's areas state none) and each recovered row with a
// file, in walk order. The golden test checks consolidate.Current agrees.
func fingerprint(rows []initrecord.Recovered) string {
	h := sha256.New()
	fmt.Fprintf(h, "walk %s\npromise %q\n", walkAt.UTC().Format(time.RFC3339Nano), productpromise.NoPromise)
	for _, name := range walkOrder {
		for _, r := range rows {
			if r.Capability == name && r.Status == recovery.StatusRecovered && r.File != "" {
				fmt.Fprintf(h, "%q %q %q %q\n", r.Capability, r.Status, r.File, r.Terms)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// The product's rules the consolidation lifted, as it stores them: one
// rule, and one stored text the reader can't parse.
var productRules = []string{
	strings.TrimSpace(rule("Every change is logged", "logged", "#101 #105", "two areas hold it")),
	"Sign-ins are audited, stored without its heading.",
}

// consolidated stores a consolidation made from rows, current while they
// are. It merged sign-in-2 into sign-in (1 group, 1 merged away), merged
// charge-once-2 into charge-once (1 exact) and rate-limit into rate-limited
// (1 near), moved provider-config from pay to cart, dropped the question
// guest-carts as answered, lifted logged with logged-2 and audit with
// audit-2 (2 lifts), and refused 2 items. Entries: 19 before; after, cart
// keeps 5 rules and its decision, pay 3 rules and sign-in 3 rules, 12 in
// all, and the product 2 rules.
func (f *fixture) consolidated(rows []initrecord.Recovered) *fixture {
	capOf := func(name, area, promise string, rules, decisions []string) consolidate.Capability {
		return consolidate.Capability{Name: name, Promise: promise, Area: capabilitytext.Area{Name: area}, File: capText(name, promise, rules, decisions, nil)}
	}
	res := consolidate.Result{
		Fingerprint: fingerprint(rows), WalkAt: walkAt, ProductPromise: productpromise.NoPromise,
		Capabilities: []consolidate.Capability{
			capOf("cart", "shop", "People fill a cart.", []string{totalsRound, emptyCart, lineOrder, removedLines, providerRule}, []string{cartDecision}),
			capOf("pay", "shop", "People pay for their cart.", []string{refundCap, chargeOnce, retryHooks}, nil),
			capOf("sign-in", "accounts", "People sign in.", []string{sessionDay, rateLimited, keepEmail}, nil),
		},
		MergedAway:   []consolidate.MergedAway{{Name: "sign-in-2", Into: "sign-in"}},
		ProductRules: productRules,
		Record: consolidating.Record{
			Merged:      []consolidating.CapabilityMerge{{Group: []string{"sign-in", "sign-in-2"}, Name: "sign-in", PromiseOf: "sign-in"}},
			ExactMerges: []consolidating.EntryMerge{{Kept: "pay/charge-once", Merged: []string{"pay/charge-once-2"}}},
			EntryMerges: []consolidating.EntryMerge{{Kept: "sign-in/rate-limited", Merged: []string{"sign-in/rate-limit"}}},
			Moves:       []consolidating.Move{{From: "pay/provider-config", To: "cart/provider-config"}},
			Dropped:     []consolidating.DroppedQuestion{{ID: "cart/guest-carts", AnsweredBy: "cart/cart-in-session"}},
			Lifts: []consolidating.Lift{{Kept: "cart/logged", With: []string{"pay/logged-2"}},
				{Kept: "sign-in/audit", With: []string{"sign-in/audit-2"}}},
			Refused: []consolidating.Refusal{{Step: consolidating.StepMerge, Item: "cart", Reason: "no candidate group holds it"},
				{Step: consolidating.StepLift, Item: "sign-in/session-day", Reason: "the product's rules would pass their budget"}},
			Consolidated: []string{"cart", "pay", "sign-in"},
		},
		Before: consolidate.Counts{Capabilities: 4, Entries: 19},
		After:  consolidate.Counts{Capabilities: 3, Entries: 12, ProductRules: 2},
	}
	body, err := json.Marshal(res)
	f.check(err)
	f.check(f.st.PutConsolidated(f.t.Context(), initrecord.Consolidated{WalkAt: walkAt, At: walkAt.Add(3 * time.Hour), Model: "m", Body: body}))
	return f
}

// spent records five phases' spend, out of run order; areas has no price.
//
//	discover     1200 input,   300 cached,   400 output;   3400 weighted, $0.051
//	areas        8000 input,  2000 cached,  1500 output;  15400 weighted, no price
//	walk        40000 input, 10000 cached,  6000 output;  70400 weighted, $1.056
//	recover     90000 input, 30000 cached, 12000 output; 150000 weighted, $2.25
//	consolidate 20000 input,  5000 cached,  3000 output;  35000 weighted, $0.528
//	total      159200 input, 47300 cached, 22900 output; 274200 weighted, no price, since areas has none
func (f *fixture) spent() *fixture {
	for _, p := range []struct {
		phase                string
		in, cached, out, wtd int64
		micro                int64
		unpriced             bool
	}{
		{"recover", 90000, 30000, 12000, 150000, 2_250_000, false},
		{"consolidate", 20000, 5000, 3000, 35000, 528_000, false},
		{"areas", 8000, 2000, 1500, 15400, 0, true},
		{"discover", 1200, 300, 400, 3400, 51_000, false},
		{"walk", 40000, 10000, 6000, 70400, 1_056_000, false},
	} {
		f.check(f.st.AddSpent(f.t.Context(), initrecord.Spent{Phase: p.phase, Count: spend.Count{
			Spend: spend.Spend{Tokens: p.wtd, MicroUSD: p.micro, Unpriced: p.unpriced},
			Raw:   adapter.Tokens{Input: p.in, CachedInput: p.cached, Output: p.out},
		}}))
	}
	return f
}

// full is the whole fixture: every phase stored, consolidation current.
func full(t *testing.T) *fixture {
	rows := recoveredRows(walkAt)
	return newFixture(t).gather(shopGather()).areas().walk().place().recovered(rows).consolidated(rows).spent()
}
