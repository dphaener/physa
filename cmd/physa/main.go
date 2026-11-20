// Command physa is Physa's command line. It prints the version of the build.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: physa version\n"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command line and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 {
		switch args[0] {
		case "version", "--version", "-version":
			fmt.Fprintf(stdout, "physa %s\n", version)
			return 0
		}
	}
	fmt.Fprint(stderr, usage)
	return 2
}
