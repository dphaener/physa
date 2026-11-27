package walk

import (
	"slices"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/sourceset"
)

var area = Area{Name: "orders", Promise: "People place and refund orders.", Files: []string{
	"server/refunds/refund.go", "server/refunds/policy.go", "server/orders/order.go", "client/form.ts", "util/glue.go",
}}

var split = sourceset.Division{Files: map[sourceset.Kind][]string{
	sourceset.Test: {"tests/refund_test.go", "tests/order_test.go", "tests/more/a_test.go"},
	sourceset.Doc:  {"docs/refunds.md", "docs/orders.md"},
}}

func whole() Answer {
	return Answer{
		Capabilities: []Capability{
			{Name: "order-refunds", Promise: "People refund orders.", Sources: []string{"server/refunds/", "client/form.ts"}, Tests: []string{"tests/refund_test.go"}, Docs: []string{"docs/"}},
			{Name: "order-placing", Promise: "People place orders.", Sources: []string{"server/orders/order.go"}, Tests: []string{"tests/"}},
		},
		Shared: []Shared{{Path: "util/", Reason: "glue"}},
	}
}

func TestCheckWholeAnswerHasNoProblemsAndExpandsPaths(t *testing.T) {
	fp, p := Check(area, whole(), split)
	if !p.Empty() {
		t.Fatalf("problems: %+v", p)
	}
	r := fp.Capabilities[0]
	if !slices.Equal(r.Sources, []string{"client/form.ts", "server/refunds/policy.go", "server/refunds/refund.go"}) {
		t.Errorf("sources %v", r.Sources)
	}
	if !slices.Equal(r.Docs, []string{"docs/orders.md", "docs/refunds.md"}) {
		t.Errorf("docs %v", r.Docs)
	}
	if got := fp.Capabilities[1].Tests; len(got) != 3 {
		t.Errorf("folder tests %v", got)
	}
	if len(fp.Shared) != 1 || fp.Shared[0] != (Shared{"util/glue.go", "glue"}) {
		t.Errorf("shared %v", fp.Shared)
	}
	// Every source file is owned once or shared.
	seen := map[string]int{}
	for _, c := range fp.Capabilities {
		for _, f := range c.Sources {
			seen[f]++
		}
	}
	for _, s := range fp.Shared {
		seen[s.Path]++
	}
	for _, f := range area.Files {
		if seen[f] != 1 {
			t.Errorf("%s seen %d times", f, seen[f])
		}
	}
}

func TestCheckUnclaimedIsInTheFixPrompt(t *testing.T) {
	ans := whole()
	ans.Shared = nil
	_, p := Check(area, ans, split)
	if !slices.Equal(p.Unclaimed, []string{"util/glue.go"}) {
		t.Fatalf("unclaimed %v", p.Unclaimed)
	}
	if !strings.Contains(FixPrompt(p), "util/glue.go") {
		t.Error("the fix prompt does not name the file")
	}
}

func TestCheckTwiceNamesBothClaimants(t *testing.T) {
	ans := whole()
	ans.Capabilities[1].Sources = append(ans.Capabilities[1].Sources, "client/form.ts")
	_, p := Check(area, ans, split)
	if got := p.Twice["client/form.ts"]; !slices.Equal(got, []string{"order-placing", "order-refunds"}) && !slices.Equal(got, []string{"order-refunds", "order-placing"}) {
		t.Fatalf("twice %v", p.Twice)
	}
	if f := FixPrompt(p); !strings.Contains(f, "client/form.ts") || !strings.Contains(f, "order-placing") {
		t.Errorf("fix prompt: %s", f)
	}
}

func TestCheckMoreSpecificClaimWins(t *testing.T) {
	ans := whole()
	ans.Capabilities[1].Sources = append(ans.Capabilities[1].Sources, "server/refunds/policy.go")
	if _, p := Check(area, ans, split); !p.Empty() {
		t.Errorf("problems: %+v", p)
	}
}

func TestCheckOutsideNotTestNotDocNoSource(t *testing.T) {
	ans := whole()
	ans.Capabilities[0].Sources = append(ans.Capabilities[0].Sources, "elsewhere/x.go")
	ans.Shared = append(ans.Shared, Shared{Path: "nowhere/", Reason: "x"})
	ans.Capabilities[0].Tests = []string{"server/orders/order.go"}
	ans.Capabilities[0].Docs = []string{"tests/refund_test.go"}
	ans.Capabilities = append(ans.Capabilities, Capability{Name: "empty-one", Promise: "Nothing.", Sources: []string{"server/none/"}})
	_, p := Check(area, ans, split)
	if !slices.Contains(p.Outside, "order-refunds: elsewhere/x.go") || !slices.Contains(p.Outside, "shared: nowhere/") {
		t.Errorf("outside %v", p.Outside)
	}
	if !slices.Equal(p.NotTest, []string{"order-refunds: server/orders/order.go"}) || !slices.Equal(p.NotDoc, []string{"order-refunds: tests/refund_test.go"}) {
		t.Errorf("not test %v, not doc %v", p.NotTest, p.NotDoc)
	}
	if !slices.Equal(p.NoSource, []string{"empty-one"}) {
		t.Errorf("no source %v", p.NoSource)
	}
	f := FixPrompt(p)
	for _, want := range []string{"elsewhere/x.go", "server/orders/order.go", "tests/refund_test.go", "empty-one"} {
		if !strings.Contains(f, want) {
			t.Errorf("fix prompt lacks %s", want)
		}
	}
}

func TestCheckNames(t *testing.T) {
	ans := whole()
	ans.Capabilities[1].Name = "order-refunds"
	ans.Capabilities[0].Promise = " "
	ans.Capabilities = append(ans.Capabilities, Capability{Name: "Bad_Name", Promise: "x", Sources: []string{"util/"}})
	_, p := Check(area, ans, split)
	if len(p.Names) != 3 {
		t.Errorf("names %v", p.Names)
	}
}

func TestCheckSplitPartsOwnEveryFileOnce(t *testing.T) {
	ans := Answer{Split: []Part{
		{Name: "refunds", Promise: "Refund.", Sources: []string{"server/refunds/", "client/form.ts"}},
		{Name: "orders", Promise: "Place.", Sources: []string{"server/orders/", "util/"}},
	}}
	parts, p := CheckSplit(area, ans)
	if !p.Empty() || len(parts) != 2 || parts[0].Parent != "orders" || len(parts[0].Files) != 3 {
		t.Fatalf("parts %+v problems %+v", parts, p)
	}
}

func TestCheckSplitProblems(t *testing.T) {
	ans := Answer{Split: []Part{
		{Name: "refunds", Promise: "Refund.", Sources: []string{"server/refunds/", "client/form.ts"}},
		{Name: "refunds", Promise: "Place.", Sources: []string{"server/refunds/policy.go", "server/orders/"}},
	}}
	_, p := CheckSplit(area, ans)
	if !slices.Equal(p.Unclaimed, []string{"util/glue.go"}) || len(p.Twice) != 0 || len(p.Names) != 1 {
		t.Errorf("problems %+v", p)
	}
	ans.Split[1].Name = "orders"
	ans.Split[1].Sources = []string{"server/refunds/", "server/orders/", "util/"}
	_, p = CheckSplit(area, ans)
	if len(p.Twice) != 2 || !strings.Contains(FixPrompt(p), "server/refunds/policy.go") || !strings.Contains(FixPrompt(p), "part") {
		t.Errorf("problems %+v", p)
	}
	_, p = CheckSplit(area, Answer{Split: ans.Split[:1]})
	if len(p.Names) == 0 {
		t.Error("one part passed")
	}
}

func TestCheckExpandsOtherAndReportsAPathThatIsNotOther(t *testing.T) {
	sp := sourceset.Division{Files: map[sourceset.Kind][]string{
		sourceset.Test: split.Files[sourceset.Test], sourceset.Doc: split.Files[sourceset.Doc],
		sourceset.Other: {"config/refunds.yml", "config/orders.yml"},
	}}
	ans := whole()
	ans.Capabilities[0].Other = []string{"config/"}
	fp, p := Check(area, ans, sp)
	if !p.Empty() || len(fp.Capabilities[0].Other) != 2 || fp.Capabilities[0].Entries.Other[0] != "config/" {
		t.Fatalf("other %v entries %v problems %+v", fp.Capabilities[0].Other, fp.Capabilities[0].Entries, p)
	}
	ans.Capabilities[0].Other = []string{"server/orders/order.go"}
	_, p = Check(area, ans, sp)
	if !slices.Equal(p.NotOther, []string{"order-refunds: server/orders/order.go"}) || p.Empty() {
		t.Fatalf("not other %v", p.NotOther)
	}
	if f := FixPrompt(p); !strings.Contains(f, "Other paths that are not other files") || !strings.Contains(f, "server/orders/order.go") {
		t.Errorf("fix prompt %s", f)
	}
}
