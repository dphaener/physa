// Command physa is Physa's command line. It checks the map with check, lists
// its capabilities with index, prints the slice of the map a task needs with
// slice and prints the version of the build with version.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapindex"
	"github.com/dphaener/physa/internal/mapslice"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: physa check [dir] | physa index [dir] | physa version\n" +
	"       physa slice <capability>... [--with decisions|questions|all] [--budget <tokens>] [--format md|json]\n"

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
		case "slice":
			opts, ok := parseSlice(args[1:])
			if ok {
				return slice(".", opts, stdout, stderr)
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

// sliceOptions is a parsed physa slice command line.
type sliceOptions struct {
	req  mapslice.Request
	json bool
}

// parseSlice reads the arguments after "slice": capability names and the flags
// --with, --budget and --format, in any order, as "--flag value" or
// "--flag=value". It reports false for anything else.
func parseSlice(args []string) (sliceOptions, bool) {
	var o sliceOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			o.req.Capabilities = append(o.req.Capabilities, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		if !hasVal {
			if i+1 >= len(args) {
				return o, false
			}
			i++
			val = args[i]
		}
		switch name {
		case "with":
			switch val {
			case "decisions":
				o.req.WithDecisions = true
			case "questions":
				o.req.WithQuestions = true
			case "all":
				o.req.WithDecisions, o.req.WithQuestions = true, true
			default:
				return o, false
			}
		case "budget":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return o, false
			}
			o.req.Budget = n
		case "format":
			if val != "md" && val != "json" {
				return o, false
			}
			o.json = val == "json"
		default:
			return o, false
		}
	}
	return o, len(o.req.Capabilities) > 0
}

// slice prints the slice of the map in dir: 0 when it printed, even if the map
// has problems or the slice is over budget, and 2 when the map cannot be read,
// a capability is unknown or the slice cannot be written.
func slice(dir string, o sliceOptions, stdout, stderr io.Writer) int {
	m, problems, err := mapfile.Read(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa slice: %v\n", err)
		return 2
	}
	s, err := mapslice.Build(m, o.req)
	if err != nil {
		fmt.Fprintf(stderr, "physa slice: %v\n", err)
		return 2
	}
	write := mapslice.WriteMD
	if o.json {
		write = mapslice.WriteJSON
	}
	if err := write(stdout, s); err != nil {
		fmt.Fprintf(stderr, "physa slice: %v\n", err)
		return 2
	}
	if n := len(problems); n == 1 {
		fmt.Fprintln(stderr, "physa slice: the map has 1 problem; run physa check")
	} else if n > 1 {
		fmt.Fprintf(stderr, "physa slice: the map has %d problems; run physa check\n", n)
	}
	if s.OverBudget {
		fmt.Fprintf(stderr, "physa slice: over budget: what is never cut is %d tokens, over the %d-token budget\n", s.Used, s.Budget)
	}
	return 0
}
