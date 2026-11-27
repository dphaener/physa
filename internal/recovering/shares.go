package recovering

import (
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/shares"
)

// Reserve is what a share's room keeps beside its sources: the reply, and
// from the second share on the text the shares before it recovered, each
// about as large as a reply.
const Reserve = 2 * reading.ReplyReserve

// MinRoom is the smallest room for a share's sources that halving goes down
// to; below it the capability fails, as too long. A first guess, to be
// tuned from real runs (measure-before-budget).
const MinRoom = 20_000

// Fits reports whether a prompt of system and message, with a reply reserve, fits one
// session of an agent whose window is window estimated tokens. A window of
// 0 is unknown, and then it fits: the capability runs one session, and
// goes to shares only when that session is refused as too long.
func Fits(system, message string, window int) bool {
	return window == 0 || reading.Tokens(system)+reading.Tokens(message)+reading.ReplyReserve <= window
}

// Lay lays sources, in's or those of its shares not yet read, into shares
// whose messages each take at most room estimated tokens
// (shares.LayCapability).
func (in Input) Lay(sources reading.Input, room int) ([]reading.Input, error) {
	return shares.LayCapability(sources, room, func(s reading.Input) string {
		in.Sources = s
		_, message := Split(in)
		return message
	})
}

// InShare is in for share k of parts, with earlier and its terms, what the
// shares before it ended on.
func (in Input) InShare(parts []reading.Input, k int, earlier, terms string) Input {
	in.Sources, in.Share, in.Shares, in.Earlier, in.EarlierTerms = parts[k], k+1, len(parts), earlier, terms
	return in
}

// Half is the room for a share after a session was refused as too long:
// half of room, or, when the capability ran in one session with no room
// laid, half of what that session's message took.
func Half(room int, message string) int {
	if room == 0 {
		room = reading.Tokens(message)
	}
	return room / 2
}
