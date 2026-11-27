package areas

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/sourceset"
)

// Text writes what the phase did in plain words: the files listed and left
// out by reason, how many are tests, docs, other and source, the patterns
// unused or dropped, the parts and merges, each area with its file count and
// promise, and the sessions' model. It names no session.
func Text(w io.Writer, r Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Listed %s", sourceset.FileCount(r.Listed))
	var left []string
	for reason, n := range r.Left {
		left = append(left, fmt.Sprintf("%d %s", n, reason))
	}
	if sort.Strings(left); len(left) > 0 {
		fmt.Fprintf(&b, ", left out %s", strings.Join(left, ", "))
	}
	k := r.Kinds
	fmt.Fprintf(&b, ".\nOf the rest: %d tests, %d docs, %d other and %d source.\n", k[sourceset.Test], k[sourceset.Doc], k[sourceset.Other], r.Source)
	if len(r.Unused) > 0 {
		fmt.Fprintf(&b, "Patterns that matched no file: %s.\n", strings.Join(r.Unused, ", "))
	}
	if len(r.Dropped) > 0 {
		fmt.Fprintf(&b, "Patterns dropped for matching every file: %s.\n", strings.Join(r.Dropped, ", "))
	}
	fmt.Fprintf(&b, "Divided the source into areas (%d), from parts (%d) and merges (%d):\n", len(r.Areas), r.Parts, len(r.Merges))
	for _, a := range r.Areas {
		fmt.Fprintf(&b, "- %s (%s): %s\n", a.Name, sourceset.FileCount(a.Files), strings.TrimSpace(a.Promise))
	}
	fmt.Fprintf(&b, "Model: %s\n", r.Model)
	_, err := io.WriteString(w, b.String())
	return err
}
