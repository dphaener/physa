package learn

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// WriteText writes a run's triage for people: what was learned from, which
// sources could not be read, the changes with signals and their evidence,
// and the changes with nothing to learn.
func WriteText(w io.Writer, r Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Learning from %d changes (%s), %d already learned.\n", r.Changes, r.Range, r.AlreadyLearned)
	if r.Forge != "" {
		fmt.Fprintf(&b, "Forge: %s\n", r.Forge)
	} else {
		fmt.Fprintf(&b, "Forge: unavailable (%s): %s and %s signals not checked.\n",
			reasonOf(r, KindCorrection), KindCorrection, KindArguedOptions)
	}
	if reason := reasonOf(r, KindServedNotLoaded); reason != "" {
		fmt.Fprintf(&b, "Slice records: unavailable (%s).\n", reason)
	} else {
		fmt.Fprintf(&b, "Slice records: %d of %d changes had none and were not counted.\n", r.RecordsMissing, r.Changes)
	}
	for _, u := range r.Unavailable {
		fmt.Fprintf(&b, "Not checked: %s (%s).\n", u.Kind, u.Reason)
	}

	fmt.Fprintf(&b, "\nSignals (%d changes):\n", len(r.Learn))
	for _, c := range r.Learn {
		fmt.Fprintf(&b, "  %s %s\n", c.Short, c.Subject)
		for _, s := range c.Signals {
			line := fmt.Sprintf("    %s [%s]", s.Kind, s.Rule)
			if s.Capability != "" {
				line += ": " + s.Capability
			}
			b.WriteString(line + "\n")
			for _, e := range s.Evidence {
				fmt.Fprintf(&b, "      %s\n", e)
			}
		}
	}
	fmt.Fprintf(&b, "\nNothing to learn (%d):\n", len(r.Nothing))
	for _, c := range r.Nothing {
		fmt.Fprintf(&b, "  %s %s\n", c.Short, c.Subject)
	}
	if len(r.Incomplete) > 0 {
		fmt.Fprintf(&b, "\nNot fully triaged (%d):\n", len(r.Incomplete))
		for _, c := range r.Incomplete {
			fmt.Fprintf(&b, "  %s %s (%s)\n", c.Short, c.Subject, c.Reason)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteJSON writes a run's triage for tools, as one JSON object.
func WriteJSON(w io.Writer, r Result) error {
	return json.NewEncoder(w).Encode(r)
}

// reasonOf returns why kind was not checked, or empty when it was.
func reasonOf(r Result, kind string) string {
	for _, u := range r.Unavailable {
		if u.Kind == kind {
			return u.Reason
		}
	}
	return ""
}
