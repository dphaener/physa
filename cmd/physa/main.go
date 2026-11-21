// Command physa is Physa's command line, a tree of grouped commands. check
// checks the map, and the changes of a range against it: check map and check
// changes. map reads the map: map index lists its capabilities, map slice
// prints the slice a task needs, map changes lists the capability each change
// of a range names, map records lists the slice records and map learn
// finds what the merged changes have to teach the map: with --signals it
// stops at the signals, without it an agent drafts proposals that are checked
// on a scratch copy of the map and printed, never written; a bare map
// prints the index. init --gather gathers a repo's sources into the store.
// version prints the version of the build and help prints the commands. It reads version control only through the git adapter.
// physa sync is not built yet: it returns with digests, after the engine.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/dphaener/physa/internal/cli"
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
	root.Subs = []*cli.Command{
		checkCommand(),
		mapCommand(),
		initCommand(),
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
	}
	return root
}
