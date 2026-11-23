// Package spend weighs what sessions spend (design 10 §1): each kind of token
// counts by its price relative to input, and one weighted token is worth the
// input price of the adapter's default model. Dollars are the measure: the
// adapter reports what each session cost, and spend backs each model's input
// price out of it with the published ratios between token kinds, keeping it
// machine-wide, newest wins. A model with no price yet counts at raw tokens,
// with no cost shown. Spend reads in weighted tokens first, with the cost
// estimate beside it: "2.1M tokens (about $28)". A Meter weighs an
// adapter's sessions as they end, and the cache also keeps the ratio init
// estimates recovery with, measured from its finished runs.
package spend

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
)

// The published ratios between token kinds, in thousandths of the model's
// own input price. A cache read's ratio depends on the model (ReadRatio).
const (
	inputRatio   = 1000
	write5mRatio = 1250
	write1hRatio = 2000
	outputRatio  = 5000
)

var (
	// readsAt5 are the families whose cache reads cost 5% of input.
	readsAt5 = regexp.MustCompile(`(?i)(opus|sonnet)-5[-.]5\b`)
	// readsAt2p5 are the families whose cache reads cost 2.5% of input.
	readsAt2p5 = regexp.MustCompile(`(?i)(fable|mythos)-5\b`)
)

// ReadRatio is what a cache read of model costs, in thousandths of the
// model's input price: 50 for Claude Opus 5.5 and Claude Sonnet 5.5, 25 for
// Claude Fable 5.x and Claude Mythos 5.x, and 100 for any other model.
func ReadRatio(model string) int64 {
	switch {
	case readsAt5.MatchString(model):
		return 50
	case readsAt2p5.MatchString(model):
		return 25
	}
	return 100
}

// units is c in thousandths of an input token of its own model: uncached
// input + cache reads × the read ratio + 5-minute writes × 1.25 + 1-hour
// writes × 2 + output × 5.
func units(c adapter.ModelCost) int64 {
	return c.Input*inputRatio + c.CacheRead*ReadRatio(c.Model) + c.CacheWrite5m*write5mRatio +
		c.CacheWrite1h*write1hRatio + c.Output*outputRatio
}

// InputPrice backs c's model's input price out of what c cost: dollars ÷ its
// tokens weighed by the ratios, in millionths of a dollar per million input
// tokens. It reports false when c carries no dollars or no tokens.
func InputPrice(c adapter.ModelCost) (int64, bool) {
	u := units(c)
	if c.MicroUSD <= 0 || u <= 0 {
		return 0, false
	}
	return int64(math.Round(float64(c.MicroUSD) * 1e9 / float64(u))), true
}

// Spend is spend weighed: Tokens weighted tokens and MicroUSD their cost
// estimate in millionths of a dollar. Unpriced says some of it counted at raw
// tokens, for want of a price, so no cost is shown for it.
type Spend struct {
	Tokens   int64 `json:"tokens"`
	MicroUSD int64 `json:"micro_usd"`
	Unpriced bool  `json:"unpriced,omitempty"`
}

// Add is s and o together.
func (s Spend) Add(o Spend) Spend {
	return Spend{Tokens: s.Tokens + o.Tokens, MicroUSD: s.MicroUSD + o.MicroUSD, Unpriced: s.Unpriced || o.Unpriced}
}

// String reads like "2.1M tokens (about $28)", or "2.1M tokens" when
// Unpriced.
func (s Spend) String() string {
	if s.Unpriced {
		return Tokens(s.Tokens) + " tokens"
	}
	return fmt.Sprintf("%s tokens (%s)", Tokens(s.Tokens), s.Cost())
}

// Cost is the cost estimate: "about $28", "under $0.01", or "none: no price
// yet" when Unpriced.
func (s Spend) Cost() string {
	switch {
	case s.Unpriced:
		return "none: no price yet"
	case s.MicroUSD > 0 && s.MicroUSD < 5_000:
		return "under $0.01"
	}
	return "about " + Dollars(s.MicroUSD)
}

// Tokens is n shortened: "980", "7.4k", "74k", "2.1M".
func Tokens(n int64) string {
	f := float64(n)
	switch {
	case n >= 999_500:
		return trim(fmt.Sprintf("%.1f", f/1e6)) + "M"
	case n >= 9_950:
		return fmt.Sprintf("%.0fk", f/1e3)
	case n >= 1000:
		return trim(fmt.Sprintf("%.1f", f/1e3)) + "k"
	}
	return fmt.Sprint(n)
}

// Dollars is micro, in millionths of a dollar, shown as an estimate: whole
// dollars from $10, cents below.
func Dollars(micro int64) string {
	if micro >= 9_995_000 {
		return fmt.Sprintf("$%.0f", float64(micro)/1e6)
	}
	return fmt.Sprintf("$%.2f", float64(micro)/1e6)
}

// trim drops a ".0" ending.
func trim(s string) string { return strings.TrimSuffix(s, ".0") }

// name is model's name as prices are kept under: without a bracketed suffix
// such as "[1m]".
func name(model string) string {
	n, _, _ := strings.Cut(model, "[")
	return strings.TrimSpace(n)
}

// costOf is what r cost per model, or, when the agent reported no cost, its
// tokens as one model's: uncached input (cache writes among it), cache reads
// and output, with no dollars.
func costOf(r adapter.Result) adapter.Cost {
	if len(r.Cost) > 0 {
		return r.Cost
	}
	t := r.Tokens
	if t == (adapter.Tokens{}) {
		return nil
	}
	return adapter.Cost{{Model: r.Model, Input: t.Input - t.CachedInput, CacheRead: t.CachedInput, Output: t.Output}}
}
