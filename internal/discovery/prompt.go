package discovery

import (
	_ "embed"
	"strings"
)

// skill is the discovery skill: everything the session is told. It is
// embedded only.
//
//go:embed discovery.md
var skill string

// Prompt returns everything the session starts from: the discovery skill and
// nothing else. It says what to look for, never where: the session is given
// no paths, because finding where a repo keeps its prior art is its job.
func Prompt() string {
	return strings.TrimRight(skill, "\n") + "\n"
}
