// Package preflight says, before init spends, what would fail its agent's
// sessions or thin the map it builds: the agent's own warnings about the
// machine's setup, and a gather missing full history or PRs. It only warns;
// init goes on either way.
package preflight

import (
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/gather"
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

// Warn writes each warning on w as init's, one line each.
func Warn(w io.Writer, warnings []string) {
	for _, s := range warnings {
		fmt.Fprintf(w, "physa init: warning: %s\n", s)
	}
}
