package history

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SlugList joins slugs for display, or gives a dash when there are none.
func SlugList(slugs []string) string {
	if len(slugs) == 0 {
		return "—"
	}
	return strings.Join(slugs, ", ")
}

// WriteChanges prints the changes of the range rng with their capability and
// the findings against the map, as one JSON object (range, commits, findings)
// when asJSON, and as text otherwise.
func WriteChanges(w io.Writer, rng string, commits []Commit, findings []Finding, asJSON bool) error {
	if asJSON {
		out := struct {
			Range    string    `json:"range"`
			Commits  []Commit  `json:"commits"`
			Findings []Finding `json:"findings"`
		}{rng, commits, findings}
		return json.NewEncoder(w).Encode(out)
	}
	for _, c := range commits {
		if _, err := fmt.Fprintf(w, "%s %s\n  capability: %s\n", c.Short, c.Subject, SlugList(c.Capabilities)); err != nil {
			return err
		}
	}
	if len(findings) == 0 {
		_, err := fmt.Fprintf(w, "\nphysa map changes: ok (%d commits)\n", len(commits))
		return err
	}
	if _, err := fmt.Fprintf(w, "\n%d findings:\n", len(findings)); err != nil {
		return err
	}
	for _, f := range findings {
		if _, err := fmt.Fprintf(w, "%s: %s\n", f.Commit, f.Message); err != nil {
			return err
		}
	}
	return nil
}
