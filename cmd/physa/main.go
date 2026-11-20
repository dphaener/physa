// Command physa is Physa's command line. It checks the map with check, lists
// its capabilities with index and prints the version of the build with
// version.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapindex"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: physa check [dir] | physa index [dir] | physa version\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command line and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-version":
			if len(args) == 1 {
				fmt.Fprintf(stdout, "physa %s\n", version)
				return 0
			}
		case "check":
			if len(args) <= 2 {
				dir := "."
				if len(args) == 2 {
					dir = args[1]
				}
				return check(dir, stdout, stderr)
			}
		case "index":
			if len(args) <= 2 {
				dir := "."
				if len(args) == 2 {
					dir = args[1]
				}
				return index(dir, stdout, stderr)
			}
		}
	}
	fmt.Fprint(stderr, usage)
	return 2
}

// check reports the map in dir: 0 when it is sound, 1 with problems, 2 when
// it cannot be read.
func check(dir string, stdout, stderr io.Writer) int {
	res, err := mapcheck.Check(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa check: %v\n", err)
		return 2
	}
	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintln(stderr, p.String())
		}
		return 1
	}
	fmt.Fprintf(stdout, "physa check: ok (%d capabilities, %d entries)\n", res.Capabilities, res.Entries)
	return 0
}

// index prints the index of the map in dir: 0 when it printed, even if the map
// has problems, and 2 when the map cannot be read or the index cannot be written.
func index(dir string, stdout, stderr io.Writer) int {
	m, problems, err := mapfile.Read(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa index: %v\n", err)
		return 2
	}
	if err := mapindex.Write(stdout, mapindex.Build(m)); err != nil {
		fmt.Fprintf(stderr, "physa index: %v\n", err)
		return 2
	}
	if n := len(problems); n == 1 {
		fmt.Fprintln(stderr, "physa index: the map has 1 problem; run physa check")
	} else if n > 1 {
		fmt.Fprintf(stderr, "physa index: the map has %d problems; run physa check\n", n)
	}
	return 0
}
