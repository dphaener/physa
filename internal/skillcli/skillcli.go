// Package skillcli holds Physa's skill for a person's agent: the guide that
// teaches it to use Physa's commands and terms as Physa means them, embedded
// in the binary and never read from the repo, so upgrading Physa updates what
// every agent is told. The skill command group puts it in reach: show writes
// the guide exactly as it is, on any output, with no styling; setup writes a
// short stub into Claude Code's skills directory that tells the agent to run
// show, status says whether the stub is current, stale or missing, and remove
// deletes it. Setup and remove ask on a terminal and need --yes off one, and
// never overwrite, remove or follow a SKILL.md Physa did not write.
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
// asking on the terminal.
func Command() *cobra.Command { return newCommand(defaultEnv()) }

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
