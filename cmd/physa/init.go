package main

import (
	"io"
	"time"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/initrun"
	"github.com/dphaener/physa/internal/vcs/git"
)

// initCommand builds physa init. Its gather, propose and recover phases are built.
func initCommand() *cli.Command {
	return &cli.Command{
		Name:    "init",
		Summary: "Build the map of an existing repo: gather its sources (--gather), propose a map from them (--propose), then recover each capability's file (--recover).",
		Usage:   initrun.Usage,
		Run: func(args []string, stdout, stderr io.Writer) int {
			return initrun.Run(args, stdout, stderr, initrun.Env{
				Store:    openStore,
				Repo:     git.Open,
				Forge:    newForge,
				PRExists: prResolver,
				Agent:    agentFor,
				Now:      time.Now,
			})
		},
	}
}
