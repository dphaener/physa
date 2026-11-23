// Package recoveryreport prints what recovering a map's capabilities did, as
// text for people or as one JSON document for tools. It reads a
// recovery.Report and changes nothing.
package recoveryreport

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recovery"
)

// WriteJSON writes the report as one indented JSON document, each
// capability's file and terms included.
func WriteJSON(w io.Writer, r recovery.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText writes the report as text: a first line with the counts and the
// tokens used, then one block per capability that ran, then
// the capabilities not yet recovered.
func WriteText(w io.Writer, r recovery.Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Recovered %d, not yet recovered %d, failed %d; used %s.\n",
		count(r, recovery.StatusRecovered), count(r, recovery.StatusNotYet), count(r, recovery.StatusFailed),
		plural(r.Used.Input+r.Used.Output, "token", "tokens"))
	var notYet []string
	for _, o := range r.Capabilities {
		if o.Status == recovery.StatusNotYet {
			notYet = append(notYet, o.Name)
			continue
		}
		fmt.Fprintf(&b, "\n## %s: %s\n", o.Name, o.Status)
		if o.Status == recovery.StatusRecovered {
			fmt.Fprintf(&b, "Kept %s.\n", kept(o.Counts))
		}
		for _, d := range o.Dropped {
			fmt.Fprintf(&b, "Dropped %s\n", dropped(d))
		}
		for _, p := range o.Problems {
			fmt.Fprintf(&b, "Problem: %s\n", p)
		}
		if o.Error != "" {
			fmt.Fprintf(&b, "Error: %s\n", o.Error)
		}
		fmt.Fprintf(&b, "Attempts: %d; session %s; tokens: %s.\n", o.Attempts, orNone(o.SessionID), o.Tokens)
	}
	if len(notYet) > 0 {
		fmt.Fprintf(&b, "\nNot yet recovered (the run stopped before reaching them; run again with --only %s):\n", strings.Join(notYet, ","))
		for _, n := range notYet {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// count says how many of the report's capabilities ended with status.
func count(r recovery.Report, status string) int {
	n := 0
	for _, o := range r.Capabilities {
		if o.Status == status {
			n++
		}
	}
	return n
}

// kept words what a capability kept: "3 rules, 1 decision, 0 questions, 2 terms".
func kept(c recovering.Counts) string {
	return strings.Join([]string{
		plural(int64(c.Rules), "rule", "rules"), plural(int64(c.Decisions), "decision", "decisions"),
		plural(int64(c.Questions), "question", "questions"), plural(int64(c.Terms), "term", "terms"),
	}, ", ")
}

// plural is "1 one" when n is 1, else "n many".
func plural(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// dropped words one dropped entry or term and why.
func dropped(d recovering.Dropped) string {
	s := d.Kind + " " + fmt.Sprintf("%q", d.Heading)
	if d.Slug != "" {
		s += " (" + d.Slug + ")"
	}
	return s + ": " + strings.Join(d.Reasons, "; ")
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
