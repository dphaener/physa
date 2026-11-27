// Package agentspointer keeps the few marked lines init adds to AGENTS.md:
// it puts them between their markers and changes nothing outside them.
package agentspointer

import "strings"

// The AGENTS.md pointer's markers and the block between them (design 01
// section 7).
const (
	startMarker = "<!-- physa -->"
	endMarker   = "<!-- /physa -->"
	// Pointer is the block, markers included.
	Pointer = startMarker + "\n" +
		"Before changing code, run `physa map index`, then `physa map slice <capabilities>` for the ones your task touches.\n" +
		"No CLI? Read .physa/README.md.\n" +
		endMarker
)

// With is agents with the pointer block: put between its markers when
// it has them, else appended after one blank line. Nothing outside the
// markers changes, and an absent file is the block alone.
func With(agents string) string {
	if start := strings.Index(agents, startMarker); start >= 0 {
		if end := strings.Index(agents[start:], endMarker); end >= 0 {
			end += start + len(endMarker)
			return agents[:start] + Pointer + agents[end:]
		}
	}
	if agents == "" {
		return Pointer + "\n"
	}
	if !strings.HasSuffix(agents, "\n") {
		agents += "\n"
	}
	if !strings.HasSuffix(agents, "\n\n") {
		agents += "\n"
	}
	return agents + Pointer + "\n"
}
