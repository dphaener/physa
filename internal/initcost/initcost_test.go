package initcost

import (
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/spend"
)

// priced is a cache that knows claude-code's default model at $4 a million.
func priced() *spend.Cache {
	c := spend.Open("")
	c.Learn("claude-code", adapter.Result{Model: "claude-opus-5-5", Cost: adapter.Cost{{Model: "claude-opus-5-5", Input: 1_000_000, MicroUSD: 4_000_000}}}, time.Now())
	return c
}

func TestUnit(t *testing.T) {
	got := Unit(priced(), "claude-code")
	if !strings.Contains(got, "each worth an input token of claude-opus-5-5 ($4.00 a million)") || strings.Contains(strings.ToLower(got), "estimate") {
		t.Errorf("Unit = %q", got)
	}
	if got := Unit(spend.Open(""), "claude-code"); !strings.Contains(got, "no prices are known yet") {
		t.Errorf("Unit with no prices = %q", got)
	}
}

func TestTable(t *testing.T) {
	got := Table([]Row{{Phase: "propose", Input: 10, Output: 2, Weighted: spend.Spend{Tokens: 20, MicroUSD: 80}}, {Phase: "recover", Input: 5, Weighted: spend.Spend{Tokens: 5, Unpriced: true}}})
	for _, want := range []string{"| propose | 10 | 0 | 2 | 20 | under $0.01 |", "| recover | 5 | 0 | 0 | 5 | none: no price yet |", "| total | 15 | 0 | 2 | 25 | none: no price yet |"} {
		if !strings.Contains(got, want) {
			t.Errorf("table lacks %q:\n%s", want, got)
		}
	}
}
