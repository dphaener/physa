package main

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/initrun"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/skillcli"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/vcs/git"
)

// initCommand builds physa init: every phase in order, or the one a flag names.
func initCommand() *cobra.Command {
	const summary = "Build the map of a repo, never bounded: it estimates its cost and asks first; with --write, commit it to physa/init; --gather, --propose or --recover runs one phase."
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
				Confirm:    func(q string) (bool, error) { return picker.ConfirmOrNo(os.Stdin, os.Stderr, q) },
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
