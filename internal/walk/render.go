package walk

import (
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/sourceset"
)

// Text writes what the walk did in plain words: the totals, the names
// numbered, each area with its capabilities or its parts, and the model. It
// names no session and no file.
func Text(w io.Writer, r Result) error {
	fmt.Fprintf(w, "Walked the areas for their capabilities (%d): %s owned, %s shared, after fix rounds (%d).\n",
		r.Capabilities, sourceset.FileCount(r.Owned), sourceset.FileCount(r.Shared), r.FixRounds)
	for _, n := range r.Renamed {
		fmt.Fprintf(w, "An earlier area took the name %s, so %s keeps it as %s.\n", n.From, n.Area, n.To)
	}
	for _, a := range r.Areas {
		if len(a.Parts) > 0 {
			fmt.Fprintf(w, "- %s: split into %s\n", a.Name, strings.Join(a.Parts, ", "))
			continue
		}
		fmt.Fprintf(w, "- %s (%s owned, %s shared): %s\n", a.Name, sourceset.FileCount(a.Owned), sourceset.FileCount(a.Shared), strings.Join(a.Capabilities, ", "))
	}
	_, err := fmt.Fprintf(w, "Model: %s\n", r.Model)
	return err
}
