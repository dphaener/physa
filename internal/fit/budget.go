package fit

import "fmt"

// Each number below is a first guess, to be tuned from real runs
// (measure-before-budget).
const (
	// InputBudget is the most tokens of gathered sources the proposer reads.
	InputBudget = 100_000
	// ReplyReserve is the part of the appetite kept for the session's reply.
	ReplyReserve = 16_000
	// MinBudget is the smallest input budget worth a session.
	MinBudget = 5_000

	// DocsShare is the percent of the budget docs may spend first.
	DocsShare = 40
	// PRsShare is the percent of the budget PRs may spend, plus what docs left.
	PRsShare = 40
	// CommitsShare is the percent of the budget commit subjects may spend,
	// plus what PRs left.
	CommitsShare = 15
	// ClustersShare is the percent of the budget co-change clusters may
	// spend, plus what commits left.
	ClustersShare = 5

	// PRBodyTokens caps each PR body.
	PRBodyTokens = 500
	// CommentTokens caps each review comment.
	CommentTokens = 300
	// ClusterMaxFiles is the most files a change may touch and still count
	// for clusters; a larger change says little about what belongs together.
	ClusterMaxFiles = 20
	// ClusterMinChanges is the fewest changes a pair of files must share to
	// be clustered.
	ClusterMinChanges = 3
	// ClusterPaths is the most paths listed for one cluster.
	ClusterPaths = 8
)

// Tokens estimates the tokens in s as bytes / 4, the rule gather uses.
func Tokens(s string) int { return len(s) / 4 }

// Budget returns the input budget for a session under appetite, after the
// reply's reserve and overhead (the skill and the prompt's own text):
// min(InputBudget, appetite − ReplyReserve − overhead). It errors, naming the
// smallest appetite that works, when that leaves less than MinBudget.
func Budget(appetite int64, overhead int) (int, error) {
	b := appetite - ReplyReserve - int64(overhead)
	if b < MinBudget {
		least := int64(MinBudget + ReplyReserve + overhead)
		return 0, fmt.Errorf("the appetite of %s tokens is too small: init needs at least %s (%s for the sources, %s for the reply and %s for the skill)",
			thousands(int(appetite)), thousands(int(least)), thousands(MinBudget), thousands(ReplyReserve), thousands(overhead))
	}
	if b > InputBudget {
		b = InputBudget
	}
	return int(b), nil
}
