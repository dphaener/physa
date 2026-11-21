package learn

// The thresholds of triage, in one place. Each starts small and is tuned from
// real runs, never guessed further ahead (measure-before-budget, design 08
// section 3).
const (
	// DefaultChanges is how many of the newest changes are collected when no
	// starting change is given, so a first run on an old repo stays small.
	DefaultChanges = 50
	// CorrectionMinComments is how many review comments followed by a later
	// commit in the same PR make a correction.
	CorrectionMinComments = 1
	// ArguedMinComments is how many comments, review and discussion
	// together, a PR needs for its options to count as argued.
	ArguedMinComments = 3
	// ArguedMinPeople is how many distinct people those comments need.
	ArguedMinPeople = 2
	// UnloadedMinChanges is how many learned-from changes must serve a
	// capability without their sessions loading it before it is a signal.
	UnloadedMinChanges = 3
)
