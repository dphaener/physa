// Package preflight says, before init spends, what would fail its agent's
// sessions or thin the map it builds: the agent's own warnings about the
// machine's setup, a gather missing full history or PRs, and a stored gather
// the forge stopped short, which a later phase still runs on. It only warns;
// init goes on either way.
package preflight

import (
	"fmt"
	"io"
	"time"

	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/store"
)

// Gather is what s lacks that a full map is built from, a sentence each:
// history from a shallow clone, or PRs, for want of the forge. A forge that
// worked and found none is left alone, as a young repo's is.
func Gather(s gather.Summary) []string {
	var w []string
	if s.History.Shallow {
		w = append(w, "the clone is shallow, so history holds only the changes it fetched, and the map will be thin. Run git fetch --unshallow, then run init again.")
	}
	if !s.PRs.Available {
		w = append(w, fmt.Sprintf("no PRs were gathered (%s), so the map comes from history and docs alone and will be thin. Give the forge access (for GitHub, gh auth login), then run init again.", s.PRs.Reason))
	}
	return w
}

// Stopped is the warning for a stored gather the forge stopped short, as
// run marks it, a sentence: the date its PRs reach back to (or that none
// was listed) and the date asked for, why, and that the phase runs on what
// was gathered. A complete gather gives none.
func Stopped(run store.GatherRun) []string {
	if !run.Stopped {
		return nil
	}
	asked := run.Since.UTC().Format(time.DateOnly)
	reach, missed := "no PR was listed", "every PR since "+asked
	if !run.Reached.IsZero() {
		at := run.Reached.UTC().Format(time.DateOnly)
		reach, missed = "its PRs reach back to "+at, "the PRs merged before "+at
	}
	return []string{fmt.Sprintf("the stored gather was stopped short by the forge (%s): %s of the asked %s. This phase runs on what was gathered, so the map will miss %s. Run physa init --gather again for the rest.",
		run.StopReason, reach, asked, missed)}
}

// Warn writes each warning on w as init's, one line each.
func Warn(w io.Writer, warnings []string) {
	for _, s := range warnings {
		fmt.Fprintf(w, "physa init: warning: %s\n", s)
	}
}
