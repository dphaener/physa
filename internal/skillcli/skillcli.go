// Package skillcli holds Physa's skill for a person's agent: the guide that
// teaches it to use Physa's commands and terms as Physa means them, embedded
// in the binary and never read from the repo, so upgrading Physa updates what
// every agent is told. The skill command group puts it in reach: show writes
// the guide exactly as it is, on any output, with no styling; setup writes a
// short stub into Claude Code's skills directory that tells the agent to run
// show, and adds a session hook to Claude Code's settings.json that runs the
// hidden session command when a session starts; status says whether the stub
// and the hook are current, stale or missing, and remove deletes the stub and
// takes out only Physa's hook entry. Setup and remove ask on a terminal and
// need --yes off one, and never overwrite, remove or follow a SKILL.md Physa
// did not write, or edit a settings.json Physa cannot read as JSON.
package skillcli

import (
	_ "embed"
	"io"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/internal/command"
)

// guide is the skill's text. It points to the slice skill and never copies it.
//
//go:embed guide.md
var guide string

const showUsage = "physa skill show"

// Guide is the guide a person's agent loads, as show prints it.
func Guide() string { return guide }

// Command returns the skill group, reading the person's home directory and
// asking on the terminal. current reads the pursuit and change a session works
// on, for the hidden session command the session hook runs.
func Command(current CurrentFunc) *cobra.Command { return newCommand(defaultEnv(current)) }

func show(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return command.BadUsage
	}
	if _, err := io.WriteString(stdout, guide); err != nil {
		_, _ = io.WriteString(stderr, "physa skill show: "+err.Error()+"\n")
		return 1
	}
	return 0
}
