package main

import (
	"io"
	"os"
	"time"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/initrun"
	"github.com/dphaener/physa/internal/vcs/git"
)

// initCommand builds physa init: every phase in order, or the one a flag names.
func initCommand() *cli.Command {
	return &cli.Command{
		Name:    "init",
		Summary: "Build the map of a repo under one appetite and, with --write, commit it to physa/init; --gather, --propose or --recover runs one phase.",
		Usage:   initrun.Usage,
		Run: func(args []string, stdout, stderr io.Writer) int {
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
				IsTerminal: stdinIsTerminal,
			})
		},
	}
}

// stdinIsTerminal reports whether a person is at standard input to answer:
// it is a character device, and not the null device, which is one too.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(fi, null)
}
