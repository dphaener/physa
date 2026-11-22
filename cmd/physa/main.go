// Command physa is Physa's command line, a tree of grouped commands: check
// checks the map and a range of changes; map reads the map, slices it, lists
// changes and records, formats it and learns from merged changes; init
// gathers a repo's sources and proposes its first map; idea adds, lists,
// shows and shapes ideas; bet bets on ideas and lists, shows and stops bets;
// evidence lists the facts observed in version control and the forge;
// engine starts, checks and stops the machine's engine; api calls the
// engine's API by name; sync is not built yet; version prints the build's
// version and help the commands.
// It reads version control only through the git adapter.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/dphaener/physa/internal/betcli"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/enginecli"
	"github.com/dphaener/physa/internal/evidencecli"
	"github.com/dphaener/physa/internal/ideacli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command line and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	return cli.Dispatch(rootCommand(), args, stdout, stderr)
}

// rootCommand builds the tree of commands.
func rootCommand() *cli.Command {
	root := &cli.Command{
		Name:    "physa",
		Summary: "Physa: product management for agents.",
		Usage:   []string{"physa <command> [--help]"},
	}
	root.Subs = append([]*cli.Command{checkCommand(), mapCommand(), initCommand(), ideacli.Command(newForge), betcli.Command(newForge), evidencecli.Command(newForge)}, enginecli.Commands(version)...)
	root.Subs = append(root.Subs, []*cli.Command{
		{
			Name:    "sync",
			Summary: "Not built yet: it returns with digests, after the engine.",
			Usage:   []string{"physa sync"},
			Run: func(_ []string, _, stderr io.Writer) int {
				fmt.Fprintln(stderr, "physa sync: not yet; it returns with digests, after the engine (design 07 §7)")
				return 2
			},
		},
		{
			Name:    "version",
			Summary: "Print the version of this build.",
			Usage:   []string{"physa version"},
			Run: func(args []string, stdout, _ io.Writer) int {
				if len(args) > 0 {
					return cli.BadUsage
				}
				fmt.Fprintf(stdout, "physa %s\n", version)
				return 0
			},
		},
		{
			Name:    "help",
			Summary: "Print the commands.",
			Usage:   []string{"physa help"},
			Run: func(args []string, stdout, _ io.Writer) int {
				if len(args) > 0 {
					return cli.BadUsage
				}
				cli.Help(root, stdout)
				return 0
			},
		},
	}...)
	return root
}
