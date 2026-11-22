// Package agentenv says whether Physa is running inside an agent's session,
// from the markers each adapter knows, so commands only a person may run can
// refuse deterministically.
package agentenv

import "github.com/dphaener/physa/internal/adapter/claude"

// detector reports whether one agent's session markers are set.
type detector struct {
	name string
	in   func(getenv func(string) string) bool
}

// detectors lists the agents Physa can tell it is inside, in the order they
// are checked.
var detectors = []detector{
	{name: "Claude Code", in: claude.InSession},
}

// Detect returns the name of the agent whose session getenv's environment
// marks, and true; or "" and false when no known agent's marker is set. The
// caller passes the environment's lookup.
func Detect(getenv func(string) string) (agent string, ok bool) {
	for _, d := range detectors {
		if d.in(getenv) {
			return d.name, true
		}
	}
	return "", false
}
