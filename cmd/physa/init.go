package main

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/initrun"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/skillcli"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/vcs/git"
)

// initCommand builds physa init: every phase in order, or the one a flag names.
func initCommand() *cobra.Command {
	const summary = "Build the map of a repo: it runs to the end, or with --cap stops before a session that would pass the cap; with --write, commit it to physa/init; --gather, --propose, --areas, --walk, --place or --recover runs one phase; --recover recovers from the walk and placements when both are stored, else from the proposal."
	var f initrun.Flags
	var c *cobra.Command
	c = command.Declare(command.New("init", summary, initrun.Usage,
		func(args []string, stdout, stderr io.Writer) int {
			cache, _ := spend.Path() // no settings directory: prices are kept for the run only
			return initrun.Run(f, args, command.Words(c), stdout, stderr, initrun.Env{
				Store:      openStore,
				Repo:       git.Open,
				Forge:      newForge,
				PRExists:   prResolver,
				Agent:      agentFor,
				Now:        time.Now,
				Writer:     git.OpenWriter,
				Hooks:      git.OpenHooks,
				Stdin:      os.Stdin,
				IsTerminal: func() bool { return output.IsTerminal(os.Stdin) },
				Spend:      cache,
				Skill:      skillNotice,
			})
		}), f.Declare)
	return c
}

// skillNotice is the line init prints when the agent a name selects has no
// current Physa skill on this account, or "" when it has one, the agent is
// unknown or the home cannot be found.
func skillNotice(name string) string {
	a, err := agentFor(name)
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return skillcli.Notice(a.Name(), home)
}
