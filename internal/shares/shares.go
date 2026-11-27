// Package shares lays one capability's sources into shares for recovery
// (design 02 §2.4), when they are more than one session holds. It lays the
// sources, in the order reading gives them, into shares that each read their
// sources whole, and joins them again. It never splits, cuts or skips a
// source (init-no-cut): a source larger than one share is an error naming
// it, and the capability is not recovered.
package shares

import (
	"fmt"
	"time"

	"github.com/dphaener/physa/internal/reading"
)

// TooLarge is returned by LayFor when one source, with the prompt around it
// alone, is more than one share holds. Nothing is cut.
type TooLarge struct {
	// Source names the source as a person reads it: "doc <path>", "PR #N"
	// or "change <short id>".
	Source string
	// Tokens is the source's estimated tokens, as the prompt writes it.
	Tokens int
	// Room is the most estimated tokens one share's prompt may take.
	Room int
	// Leave says how to leave the source out of the gather.
	Leave string
}

func (e *TooLarge) Error() string {
	return fmt.Sprintf("%s takes about %s estimated tokens, and with the rest of its prompt it is more than one share holds (about %s tokens). Nothing is cut. %s",
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
	default:
		c := in.Commits[i]
		e.Source = "change " + c.Short
		e.Leave = "To leave it out of the gather, gather again with --since a date after the change's (physa init --gather --since <date>)."
		if !c.At.IsZero() {
			at := c.At.UTC()
			e.Leave = fmt.Sprintf("To leave it out of the gather, gather again with --since a date after the change's, %s (physa init --gather --since %s).",
				at.Format(time.DateOnly), at.AddDate(0, 0, 1).Format(time.DateOnly))
		}
	}
	return e
}
