package fit

// Limits are the numbers a fit runs under: the input budget, each source's
// share of it, and the caps on each item. Propose holds propose's; recovery
// sets its own.
type Limits struct {
	// InputBudget is the most tokens of gathered sources the session reads.
	InputBudget int
	// ReplyReserve is the part of the appetite kept for the session's reply.
	ReplyReserve int
	// MinBudget is the smallest input budget worth a session.
	MinBudget int
	// DocsShare, PRsShare, CommitsShare and ClustersShare are the percent of
	// the budget docs, PRs, changes and co-change clusters may spend, in that
	// order, each plus what the one before left.
	DocsShare, PRsShare, CommitsShare, ClustersShare int
	// CommentsShare is the percent of what PRs may spend kept for review
	// comments: PR titles and bodies may not spend it, and what comments
	// leave passes on to changes.
	CommentsShare int
	// PRBodyTokens caps each PR body, CommentTokens each review comment.
	PRBodyTokens, CommentTokens int
	// CommitMessageTokens caps each change's message. Zero keeps subjects
	// only; above zero also keeps each change's message (without its subject
	// line), author, time and what it reverts.
	CommitMessageTokens int
}

// Propose is what init's propose phase fits under. Each number is a first
// guess, to be tuned from real runs (measure-before-budget).
var Propose = Limits{
	InputBudget: 100_000, ReplyReserve: 16_000, MinBudget: 5_000,
	DocsShare: 40, PRsShare: 40, CommitsShare: 15, ClustersShare: 5,
	CommentsShare: 25, PRBodyTokens: 500, CommentTokens: 300,
}

// What a prompt writes around each item, in bytes, sized from mapping's
// Prompt: its labels, a fence one backtick longer than the shortest, and line
// ends. An item costs its own text plus its wrapping.
const (
	docWrap     = 67 // "Doc N of N, cut by section:", the fence, path:, kind:, text:
	prWrap      = 46 // "#N, merged <date>:" (but the number), the fence, title:
	issuesWrap  = 8  // "issues:"
	issueWrap   = 3  // "- " and the line end
	bodyWrap    = 7  // "body:"
	commentWrap = 53 // "Review comment on #N:" (but the number), the fence, author:, text:
	rcWrap      = 18 // ", requests changes"
	changeWrap  = 2  // the space after the short id and the line end
	detailWrap  = 56 // author:, at: and the time, reverts:, message:
	clusterWrap = 24 // "cluster N, N changes:"
	pathWrap    = 3  // "- " and the line end
)
