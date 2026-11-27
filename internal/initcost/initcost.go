// Package initcost is init being up front about its cost, since it is never
// bounded (design 10 Q6): what a token of its spend is worth, and the table of
// what each phase spent. Spend reads in weighted tokens first, with about what
// it cost beside it when prices are known.
package initcost

import (
	"fmt"
	"strings"

	"github.com/dphaener/physa/internal/spend"
)

// Unit says what a token of a run of the adapter named agent is worth: an
// input token of its default model, at its cached price, or a raw token
// while no price is known.
func Unit(c *spend.Cache, agent string) string {
	if model, p, ok := c.Reference(agent); ok {
		return fmt.Sprintf("Spend is shown in tokens weighted by price, each worth an input token of %s (%s a million), with about what they cost beside them.", model, spend.Dollars(p.PerMillion))
	}
	return "Spend is shown in tokens; no prices are known yet, so they count raw and no cost is shown."
}

// Row is one phase's tokens, raw and Weighted.
type Row struct {
	Phase                      string
	Input, CachedInput, Output int64
	Weighted                   spend.Spend
}

// Table is rows as a markdown table, with their total, the raw counts
// beside the weighted tokens and the cost estimate.
func Table(rows []Row) string {
	var b strings.Builder
	b.WriteString("| Phase | Input | Cached input | Output | Weighted | Cost estimate |\n|---|---:|---:|---:|---:|---:|\n")
	total := Row{Phase: "total"}
	for _, t := range rows {
		total.Input, total.CachedInput, total.Output, total.Weighted = total.Input+t.Input, total.CachedInput+t.CachedInput, total.Output+t.Output, total.Weighted.Add(t.Weighted)
	}
	for _, t := range append(rows, total) {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %s | %s |\n", t.Phase, t.Input, t.CachedInput, t.Output, spend.Tokens(t.Weighted.Tokens), t.Weighted.Cost())
	}
	return b.String()
}
