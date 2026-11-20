// Command physa is Physa's command line. It checks the map with check, lists
// its capabilities with index, prints the slice of the map a task needs with
// slice, lists the capability each commit of a range names with trailers,
// runs the PR check with pr-check, lists the slice records with records and
// prints the version of the build with version. It reads version control only through the git adapter.
// physa sync is not built yet: it returns with digests, after the engine.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapindex"
	"github.com/dphaener/physa/internal/mapslice"
	"github.com/dphaener/physa/internal/vcs/git"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: physa check [dir] | physa index [dir] | physa version\n" +
	"       physa slice <capability>... [--with decisions|questions|all] [--budget <tokens>] [--format md|json]\n" +
	"       physa trailers [<range>] [--json] [--strict]\n" +
	"       physa pr-check [<range>] [--json] [--strict]\n" +
	"       physa records [--json]\n" +
	"       physa sync: not yet; it returns with digests, after the engine (design 07 \u00a77)\n"

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
		case "records":
			if asJSON, ok := parseRecords(args[1:]); ok {
				return records(".", asJSON, stdout, stderr)
			}
		case "pr-check":
			opts, ok := parsePRCheck(args[1:])
			if ok {
				return prCheck(".", opts, stdout, stderr)
			}
		case "trailers":
			opts, ok := parseTrailers(args[1:])
			if ok {
				return trailers(".", opts, stdout, stderr)
			}
		}
	}
	fmt.Fprint(stderr, usage)
	return 2
}

// check reports the map in dir: 0 when it is sound, 1 with problems, 2 when
// it cannot be read.
func check(dir string, stdout, stderr io.Writer) int {
	res, err := mapcheck.CheckWith(dir, mapcheck.Options{PRExists: prResolver(dir), OpenRepo: git.Open})
	if err != nil {
		fmt.Fprintf(stderr, "physa check: %v\n", err)
		return 2
	}
	if res.PRAnchors == mapcheck.PRAnchorsFormOnly && res.PRNote != "" {
		fmt.Fprintf(stderr, "physa check: PR anchors checked for form only: %s\n", res.PRNote)
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
	recordSlice(dir, o.req, stderr)
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

// defaultRange is the range physa trailers reads when none is given.
const defaultRange = "origin/main..HEAD"

// trailersOptions is a parsed physa trailers command line.
type trailersOptions struct {
	rng          string
	json, strict bool
}

// parseTrailers reads the arguments after "trailers": at most one range and the
// flags --json and --strict, in any order. It reports false for anything else.
func parseTrailers(args []string) (trailersOptions, bool) {
	o := trailersOptions{rng: defaultRange}
	seen := false
	for _, a := range args {
		switch {
		case a == "--json":
			o.json = true
		case a == "--strict":
			o.strict = true
		case strings.HasPrefix(a, "--") || seen:
			return o, false
		default:
			o.rng, seen = a, true
		}
	}
	return o, true
}

// trailers lists the commits of a range with their capability, then the
// findings against the map as each commit leaves it. It returns 0 whatever the
// findings, 1 when o.strict and there are findings, and 2 when the map or the
// history cannot be read or the output cannot be written.
func trailers(dir string, o trailersOptions, stdout, stderr io.Writer) int {
	_, problems, err := mapfile.Read(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa trailers: %v\n", err)
		return 2
	}
	if n := len(problems); n == 1 {
		fmt.Fprintln(stderr, "physa trailers: the map has 1 problem; run physa check")
	} else if n > 1 {
		fmt.Fprintf(stderr, "physa trailers: the map has %d problems; run physa check\n", n)
	}
	repo, err := git.Open(dir)
	if err != nil {
		fmt.Fprintf(stderr, "physa trailers: %v\n", err)
		return 2
	}
	commits, err := history.Read(repo, o.rng)
	if err != nil {
		fmt.Fprintf(stderr, "physa trailers: %v\n", err)
		return 2
	}
	findings, err := history.Check(repo, commits)
	if err != nil {
		fmt.Fprintf(stderr, "physa trailers: %v\n", err)
		return 2
	}
	if o.json {
		out := struct {
			Range    string            `json:"range"`
			Commits  []history.Commit  `json:"commits"`
			Findings []history.Finding `json:"findings"`
		}{o.rng, commits, findings}
		if err := json.NewEncoder(stdout).Encode(out); err != nil {
			fmt.Fprintf(stderr, "physa trailers: %v\n", err)
			return 2
		}
	} else {
		for _, c := range commits {
			fmt.Fprintf(stdout, "%s %s\n  capability: %s\n", c.Short, c.Subject, slugList(c.Capabilities))
		}
		if len(findings) == 0 {
			fmt.Fprintf(stdout, "\nphysa trailers: ok (%d commits)\n", len(commits))
		} else {
			fmt.Fprintf(stdout, "\n%d findings:\n", len(findings))
			for _, f := range findings {
				fmt.Fprintf(stdout, "%s: %s\n", f.Commit, f.Message)
			}
		}
	}
	if o.strict && len(findings) > 0 {
		return 1
	}
	return 0
}

// slugList joins slugs for display, or gives a dash when there are none.
func slugList(slugs []string) string {
	if len(slugs) == 0 {
		return "\u2014"
	}
	return strings.Join(slugs, ", ")
}
