package fit

import (
	"fmt"
	"strconv"
)

// Report says what Fit kept and cut: the budget, the tokens used, and one
// SourceReport per source, in order: docs, pr titles, pr bodies, review
// comments, commits, clusters.
type Report struct {
	Budget  int
	Used    int
	Sources []SourceReport
}

// SourceReport counts one source: Items gathered, Kept (whole or cut),
// Partial (kept but cut), and their estimated Tokens gathered and kept.
type SourceReport struct {
	Name       string
	Items      int
	Kept       int
	Partial    int
	Tokens     int
	KeptTokens int
}

// Lines says per source what was cut, one plain line each, e.g.
// "docs: 9 of 9 kept, 2 cut by section (14,300 of 52,000 tokens left out)".
// A source that lost nothing says "kept whole"; one with nothing gathered
// says "none".
func (r Report) Lines() []string {
	var out []string
	for _, s := range r.Sources {
		switch {
		case s.Items == 0:
			out = append(out, s.Name+": none")
		case s.Kept == s.Items && s.Partial == 0:
			out = append(out, s.Name+": kept whole")
		default:
			line := fmt.Sprintf("%s: %s of %s kept", s.Name, thousands(s.Kept), thousands(s.Items))
			if s.Partial > 0 {
				how := "cut short"
				if s.Name == "docs" {
					how = "cut by section"
				}
				line += fmt.Sprintf(", %s %s", thousands(s.Partial), how)
			}
			line += fmt.Sprintf(" (%s of %s tokens left out)", thousands(max(s.Tokens-s.KeptTokens, 0)), thousands(s.Tokens))
			out = append(out, line)
		}
	}
	return out
}

// thousands writes n with commas: 14300 is "14,300".
func thousands(n int) string {
	s := strconv.Itoa(n)
	neg := ""
	if n < 0 {
		neg, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return neg + s
}
