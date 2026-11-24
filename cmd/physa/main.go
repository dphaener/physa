// Command physa is Physa's command line, a tree of grouped commands: check
// checks the map and a range of changes; map reads the map, slices it, lists
// changes and records, formats it and learns from merged changes; init
// gathers a repo's sources and proposes its first map; idea adds, lists
// and shows ideas; pursue starts a pursuit of an idea against a signal;
// pursuit lists, shows and stops pursuits and holds their plans; evidence
// lists the facts observed in version control and the forge;
// engine starts, checks and stops the machine's engine; api calls the
// engine's API by name; sync is not built yet; version prints the build's
// version and help the commands.
// It reads version control only through the git adapter.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/enginecli"
	"github.com/dphaener/physa/internal/evidencecli"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/pursuitcli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command line and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	return command.Execute(rootCommand(), args, stdout, stderr)
}

// rootCommand builds the tree of commands.
func rootCommand() *cobra.Command {
	root := command.New("physa", "Physa: product management for agents.", []string{"physa <command> [--help]"}, nil,
		checkCommand(), mapCommand(), initCommand(), ideacli.Command(newForge))
	root.AddCommand(pursuitcli.Commands(newForge)...)
	root.AddCommand(evidencecli.Command(newForge))
	root.AddCommand(enginecli.Commands(version)...)
	root.AddCommand(
		command.New("sync", "Not built yet: it returns with digests, after the engine.", []string{"physa sync"},
			func(_ []string, _, stderr io.Writer) int {
				fmt.Fprintln(stderr, "physa sync: not yet; it returns with digests, after the engine (design 07 §7)")
				return 2
			}),
		command.New("version", "Print the version of this build.", []string{"physa version"},
			func(args []string, stdout, _ io.Writer) int {
				if len(args) > 0 {
					return command.BadUsage
				}
				fmt.Fprintf(stdout, "physa %s\n", version)
				return 0
			}),
		command.New("help", "Print the commands.", []string{"physa help"},
			func(args []string, stdout, _ io.Writer) int {
				if len(args) > 0 {
					return command.BadUsage
				}
				command.Help(root, stdout)
				return 0
			}),
	)
	return root
}
