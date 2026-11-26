// Package shares reads init's sources in shares when they are more than
// one session holds: propose's (design 02 §2.3.1), and one capability's in
// recovery (§2.4). It lays the sources, in the order reading gives them,
// into shares that each read their sources whole, and merges what propose's
// shares proposed. It never splits, cuts or skips a source
// (init-no-cut): a source larger than one share stops init, named, before
// anything is spent on propose. When the whole prompt fits one session,
// propose runs as before, unchanged.
package shares

import (
	"fmt"
	"time"

	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
)

// Fits reports whether propose's whole prompt for in, with its reply
// reserve, fits one session of an agent whose window is window estimated
// tokens. A window of 0 is unknown, and then everything fits: propose runs
// one session, as before.
func Fits(in reading.Input, window int) bool {
	return window == 0 || reading.Tokens(mapping.Prompt(in))+mapping.ReplyReserve <= window
}

// TooLarge is returned by Lay when one source, with the skill and headings
// alone, is more than one share holds. Nothing is cut, so init stops before
// spending on propose.
type TooLarge struct {
	// Source names the source as a person reads it: "doc <path>", "PR #N",
	// "change <short id>" or "cluster N".
	Source string
	// Tokens is the source's estimated tokens, as propose writes it.
	Tokens int
	// Room is the most estimated tokens one share's prompt may take.
	Room int
	// Leave says how to leave the source out of the gather.
	Leave string
}

func (e *TooLarge) Error() string {
	return fmt.Sprintf("%s takes about %s estimated tokens, and with the skill it is more than one share of propose holds (about %s tokens). "+
		"Nothing is cut, so init stops before spending on propose. %s",
		e.Source, reading.Thousands(int64(e.Tokens)), reading.Thousands(int64(e.Room)), e.Leave)
}

// tooLarge names the source of kind k at index i of in.
func tooLarge(in reading.Input, k, i, tokens, room int) *TooLarge {
	e := &TooLarge{Tokens: tokens, Room: room}
	switch k {
	case kindDoc:
		e.Source = "doc " + in.Docs[i].Path
		e.Leave = "To leave it out of the gather, move it out of the working tree (or, if gather's search found it, ignore it in git: the search skips files git ignores), then gather again with physa init --gather."
	case kindPR:
		p := in.PRs[i]
		e.Source = fmt.Sprintf("PR #%d", p.Number)
		merged := p.MergedAt.UTC()
		e.Leave = fmt.Sprintf("To leave it out of the gather, gather again with --since a date after it merged on %s (physa init --gather --since %s), or with a smaller --max-prs.",
			merged.Format(time.DateOnly), merged.AddDate(0, 0, 1).Format(time.DateOnly))
	case kindChange:
		c := in.Commits[i]
		e.Source = "change " + c.Short
		e.Leave = "To leave it out of the gather, gather again with --since a date after the change's (physa init --gather --since <date>)."
		if !c.At.IsZero() {
			at := c.At.UTC()
			e.Leave = fmt.Sprintf("To leave it out of the gather, gather again with --since a date after the change's, %s (physa init --gather --since %s).",
				at.Format(time.DateOnly), at.AddDate(0, 0, 1).Format(time.DateOnly))
		}
	default:
		e.Source = fmt.Sprintf("cluster %d", i+1)
		e.Leave = "Clusters come from the changes gathered: to leave it out, gather again with a later --since (physa init --gather --since <date>), so fewer changes are read."
	}
	return e
}
