package place

import (
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/sourceset"
)

// Text writes what placement did in plain words, one count to a line, each
// reason named. It names no file but the broad entries it did not use.
func Text(w io.Writer, r Result) error {
	fmt.Fprintf(w, "Placed the gathered PRs and changes on the capabilities (%d) by their footprints, with no session.\n", r.Capabilities)
	fmt.Fprintf(w, "PRs placed: %d\n", r.PRsPlaced)
	for _, n := range r.PRsNowhere {
		fmt.Fprintf(w, "PRs placed nowhere, %s: %d\n", n.Reason, n.Count)
	}
	fmt.Fprintf(w, "Changes placed on their own: %d\n", r.ChangesPlaced)
	fmt.Fprintf(w, "Changes placed with their PR: %d\n", r.ChangesWithPR)
	for _, n := range r.ChangesNowhere {
		fmt.Fprintf(w, "Changes placed nowhere, %s: %d\n", n.Reason, n.Count)
	}
	fmt.Fprintf(w, "Capabilities with no PR or change: %d", r.Without.Count)
	if r.Without.Count > 0 {
		fmt.Fprintf(w, " (%s)", strings.Join(r.Without.Names, ", "))
	}
	fmt.Fprintf(w, "\nBroad entries not used to place (each names more than %s): %d\n", sourceset.FileCount(Broad), len(r.Broad))
	for _, b := range r.Broad {
		fmt.Fprintf(w, "- %s: the %s entry %s names %s\n", b.Capability, b.Kind, b.Entry, sourceset.FileCount(b.Files))
	}
	if !r.WalkEntries {
		fmt.Fprintln(w, "The stored walk kept no footprint entries, so each file it names placed as itself and none could be broad.")
	}
	_, err := fmt.Fprintf(w, "Files weighed: %d\n", r.FilesWeighed)
	return err
}
