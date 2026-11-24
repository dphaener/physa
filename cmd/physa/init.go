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
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/vcs/git"
)

// initCommand builds physa init: every phase in order, or the one a flag names.
func initCommand() *cobra.Command {
	const summary = "Build the map of a repo, never bounded: it estimates its cost and asks first; with --write, commit it to physa/init; --gather, --propose or --recover runs one phase."
	return command.New("init", summary, initrun.Usage,
		func(args []string, stdout, stderr io.Writer) int {
			cache, _ := spend.Path() // no settings directory: prices are kept for the run only
			return initrun.Run(args, stdout, stderr, initrun.Env{
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
			})
		})
}
