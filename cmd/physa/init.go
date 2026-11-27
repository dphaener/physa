package main

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/initrun"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/skillcli"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// initCommand builds physa init on d: every phase in order, or the one a flag names.
func initCommand(d deps) *cobra.Command {
	const summary = "Build the map of a repo: it runs to the end, or with --cap stops before a session that would pass the cap; with --write, commit it to physa/init; --gather, --propose, --areas, --walk, --place, --recover or --consolidate runs one phase; --consolidate merges the recovered capabilities and lifts the product's rules; --recover recovers from the walk and placements when both are stored, else from the proposal."
	var f initrun.Flags
	var c *cobra.Command
	c = command.Declare(command.New("init", summary, initrun.Usage,
		func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			cache, _ := spend.Path() // no settings directory: prices are kept for the run only
			return initrun.Run(ctx, f, args, command.Words(c), stdout, stderr, initrun.Env{
				Store:      d.openStore,
				Repo:       openRepo,
				Forge:      d.newForge,
				PRExists:   d.prResolver,
				Agent:      d.agentFor,
				Now:        time.Now,
				Writer:     openWriter,
				Hooks:      openHooks,
				Stdin:      os.Stdin,
				IsTerminal: func() bool { return output.IsTerminal(os.Stdin) },
				Spend:      cache,
				Skill:      d.skillNotice,
			})
		}), f.Declare)
	return c
}

// skillNotice is the line init prints when the agent of d a name selects has no
// current Physa skill on this account, or "" when it has one, the agent is
// unknown or the home cannot be found.
func (d deps) skillNotice(name string) string {
	a, err := d.agentFor(name)
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return skillcli.Notice(a.Name(), home)
}

// openRepo, openWriter and openHooks open git's repo, writer and hooks at
// dir, for the seams that take a context and a directory.
func openRepo(ctx context.Context, dir string) (vcs.Repo, error) { return git.Open(ctx, dir) }

func openWriter(ctx context.Context, dir string) (vcs.Writer, error) {
	return git.OpenWriter(ctx, dir)
}

func openHooks(ctx context.Context, dir string) (vcs.Hooks, error) { return git.OpenHooks(ctx, dir) }
