// Command physa is Physa's command line, a tree of grouped commands: check
// checks the map and a range of changes; map reads the map, slices it, lists
// changes and records, formats it and learns from merged changes; init
// gathers a repo's sources and proposes its first map; idea adds, lists
// and shows ideas; pursue starts a pursuit of an idea against a signal;
// pursuit lists, shows and stops pursuits and holds their plans; evidence
// lists the facts observed in version control and the forge;
// skill prints Physa's skill guide for a person's agent;
// engine starts, checks and stops the machine's engine; api calls the
// engine's API by name; sync is not built yet; version prints the build's
// version and help the commands.
// It reads version control only through the git adapter.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/adapter/claude"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/enginecli"
	"github.com/dphaener/physa/internal/evidencecli"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/forge/github"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/proc"
	"github.com/dphaener/physa/internal/pursuitcli"
	"github.com/dphaener/physa/internal/skillcli"
	"github.com/dphaener/physa/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// main makes the one context every command's job gets, cancelled on an
// interrupt or SIGTERM, and exits with the command's code. The first signal
// cancels the context and gives the signals back to the default handling,
// so a second one ends the process, even when it is blocked on a read that
// takes no context, such as a question on the terminal.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	code := run(ctx, realDeps(), os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// deps are what the commands reach through: the forge hosting the repo in a
// directory, that repo's store, the agents Physa drafts with, by name, and
// the process's state. main passes the real ones; the tests pass fakes.
type deps struct {
	newForge  func(dir string) forge.Forge
	openStore func(ctx context.Context, dir string) (*store.Store, error)
	agents    map[string]func() adapter.Adapter
	proc      func() proc.State
}

// realDeps are the real ones: GitHub through gh, the store in the repo's git
// directory, Claude Code, and the working directory and environment, read once.
func realDeps() deps {
	wd, err := os.Getwd()
	st := proc.State{Dir: ".", Wd: wd, WdErr: err, Env: os.Environ()}
	return deps{
		proc:      func() proc.State { return st },
		newForge:  func(dir string) forge.Forge { return github.New(dir) },
		openStore: func(ctx context.Context, dir string) (*store.Store, error) { return gitStore(ctx, dir, st.Environ()) },
		agents:    map[string]func() adapter.Adapter{"claude": func() adapter.Adapter { return claude.New(st.Environ(), st.Getenv) }},
	}
}

// run executes one command line with ctx and d and returns the process exit code.
func run(ctx context.Context, d deps, args []string, stdout, stderr io.Writer) int {
	return command.Execute(ctx, rootCommand(d), args, stdout, stderr)
}

// rootCommand builds the tree of commands on d.
func rootCommand(d deps) *cobra.Command {
	root := command.New("physa", "Physa: product management for agents.", []string{"physa <command> [--help]"}, nil,
		checkCommand(d), mapCommand(d), initCommand(d), ideacli.Command(d.proc(), d.newForge))
	root.AddCommand(pursuitcli.Commands(d.proc(), d.newForge)...)
	root.AddCommand(evidencecli.Command(d.proc(), d.newForge))
	root.AddCommand(skillcli.Command(d.proc(), pursuitcli.Current))
	root.AddCommand(enginecli.Commands(version, d.proc())...)
	root.AddCommand(
		command.New("sync", "Not built yet: it returns with digests, after the engine.", []string{"physa sync"},
			func(_ context.Context, _ []string, _, stderr io.Writer) int {
				fmt.Fprintln(stderr, "physa sync: not yet; it returns with digests, after the engine (design 07 §7)")
				return 2
			}),
		command.New("version", "Print the version of this build.", []string{"physa version"},
			func(_ context.Context, args []string, stdout, _ io.Writer) int {
				if len(args) > 0 {
					return command.BadUsage
				}
				fmt.Fprintf(stdout, "physa %s\n", version)
				return 0
			}),
		command.New("help", "Print the commands.", []string{"physa help"},
			func(_ context.Context, args []string, stdout, _ io.Writer) int {
				if len(args) > 0 {
					return command.BadUsage
				}
				command.Help(root, stdout)
				return 0
			}),
	)
	return root
}
