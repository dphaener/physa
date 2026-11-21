package fit

import (
	"fmt"
	"strconv"
)

// Each cluster number below is a first guess, to be tuned from real runs
// (measure-before-budget).
const (
	// ClusterMaxFiles is the most files a change may touch and still count
	// for clusters; a larger change says little about what belongs together.
	ClusterMaxFiles = 20
	// ClusterMinChanges is the fewest changes a pair of files must share to
	// be clustered.
	ClusterMinChanges = 3
	// ClusterPaths is the most paths listed for one cluster.
	ClusterPaths = 8
)

// BytesPerTokenTenths is the bytes per token every estimate uses, in tenths:
// 2.3. It was measured on #25's real propose prompts: 178,659 bytes read as
// 66,069 input tokens on Physa (2.70 a token) and 321,819 as 135,135 on
// nebari (2.38); the lower, rounded down, so estimates err high.
const BytesPerTokenTenths = 23

// BytesTokens estimates the tokens in n bytes, rounding up.
func BytesTokens(n int) int { return (n*10 + BytesPerTokenTenths - 1) / BytesPerTokenTenths }

// Tokens estimates the tokens in s; see BytesTokens.
func Tokens(s string) int { return BytesTokens(len(s)) }

// tokenBytes is the most bytes estimated at no more than t tokens.
func tokenBytes(t int) int { return t * BytesPerTokenTenths / 10 }

// Figure is the bytes per token as people read it: "2.3".
func Figure() string {
	return strconv.Itoa(BytesPerTokenTenths/10) + "." + strconv.Itoa(BytesPerTokenTenths%10)
}

// Budget is BudgetWith under the Propose limits.
func Budget(appetite int64, overhead int) (int, error) {
	return BudgetWith(appetite, overhead, Propose)
}

// BudgetWith returns the input budget for a session under appetite, after
// the reply's reserve and overhead (the skill and the prompt's own text):
// min(InputBudget, appetite − ReplyReserve − overhead). It errors, naming the
// smallest appetite that works, when that leaves less than MinBudget.
func BudgetWith(appetite int64, overhead int, l Limits) (int, error) {
	b := appetite - int64(l.ReplyReserve) - int64(overhead)
	if b < int64(l.MinBudget) {
		least := l.MinBudget + l.ReplyReserve + overhead
		return 0, fmt.Errorf("the appetite of %s tokens is too small: init needs at least %s (%s for the sources, %s for the reply and %s for the skill)",
			thousands(int(appetite)), thousands(least), thousands(l.MinBudget), thousands(l.ReplyReserve), thousands(overhead))
	}
	if b > int64(l.InputBudget) {
		b = int64(l.InputBudget)
	}
	return int(b), nil
}
