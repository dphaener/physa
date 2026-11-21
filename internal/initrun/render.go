package initrun

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/recovery"
)

// exitCode is how a recovery run ends: 2 when any capability failed, else 1
// when any is not yet recovered, else 0.
func exitCode(r recovery.Report) int {
	code := 0
	for _, o := range r.Capabilities {
		switch o.Status {
		case recovery.StatusFailed:
			return 2
		case recovery.StatusNotYet:
			code = 1
		}
	}
	return code
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

// writeRecoveryJSON writes the report as one indented JSON document, each
// capability's file and terms included.
func writeRecoveryJSON(w io.Writer, r recovery.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// writeRecoveryText writes the report as text: a first line with the counts
// and the tokens used of the appetite, then one block per capability that
// ran, then the capabilities not yet recovered.
func writeRecoveryText(w io.Writer, r recovery.Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Recovered %d, not yet recovered %d, failed %d; used %d of %d tokens.\n",
		count(r, recovery.StatusRecovered), count(r, recovery.StatusNotYet), count(r, recovery.StatusFailed),
		r.Used.Input+r.Used.Output, r.Appetite)
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
		fmt.Fprintf(&b, "\nNot yet recovered (the appetite ran out; run again with --only %s):\n", strings.Join(notYet, ","))
		for _, n := range notYet {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// kept words what a capability kept: "3 rules, 1 decision, 0 questions, 2 terms".
func kept(c recovering.Counts) string {
	return strings.Join([]string{
		plural(c.Rules, "rule", "rules"), plural(c.Decisions, "decision", "decisions"),
		plural(c.Questions, "question", "questions"), plural(c.Terms, "term", "terms"),
	}, ", ")
}

func plural(n int, one, many string) string {
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
