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

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/enginecli"
	"github.com/dphaener/physa/internal/evidencecli"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/pursuitcli"
	"github.com/dphaener/physa/internal/skillcli"
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
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes one command line with ctx and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return command.Execute(ctx, rootCommand(), args, stdout, stderr)
}

// rootCommand builds the tree of commands.
func rootCommand() *cobra.Command {
	root := command.New("physa", "Physa: product management for agents.", []string{"physa <command> [--help]"}, nil,
		checkCommand(), mapCommand(), initCommand(), ideacli.Command(newForge))
	root.AddCommand(pursuitcli.Commands(newForge)...)
	root.AddCommand(evidencecli.Command(newForge))
	root.AddCommand(skillcli.Command(pursuitcli.Current))
	root.AddCommand(enginecli.Commands(version)...)
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
