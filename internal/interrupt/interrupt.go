// Package interrupt is how an interrupted physa init ends: the command's
// context was cancelled (SIGINT or SIGTERM), so init started nothing more,
// the sessions running were stopped through that context, and what was
// already finished stays in init's record. It says so, says what was kept
// and the line that continues, and gives the code to exit with.
package interrupt

import (
	"context"
	"fmt"
	"io"
)

// Code is the exit code of an interrupted run, the one shells give a run
// SIGINT ended. It tells an interrupt apart from the cap's stop (1) and a
// failure (2).
const Code = 130

// Interrupted reports whether ctx, the command's context, was cancelled.
func Interrupted(ctx context.Context) bool { return ctx.Err() != nil }

// Line is what init prints after "physa init: " when a run was
// interrupted: nothing more was started, the sessions running were
// stopped, what was kept, and continueLine to continue.
func Line(kept, continueLine string) string {
	return fmt.Sprintf("interrupted: nothing more was started and sessions running were stopped; %s. To continue, run %s.", kept, continueLine)
}

// Kept is what an interrupted recovery kept: the n capabilities this run
// recovered, stored in init's record beside any an earlier run kept, which
// recovery said when it started.
func Kept(n int) string {
	if n == 1 {
		return "1 capability recovered in this run is kept"
	}
	return fmt.Sprintf("%d capabilities recovered in this run are kept", n)
}

// Nothing is what a phase that stores only when whole kept when
// interrupted: nothing of what (such as "the walk") was stored.
func Nothing(what string) string { return "nothing of " + what + " was stored" }

// Say prints Line with kept and continueLine on w, after "physa init: ",
// and returns Code.
func Say(w io.Writer, kept, continueLine string) int {
	fmt.Fprintf(w, "physa init: %s\n", Line(kept, continueLine))
	return Code
}
