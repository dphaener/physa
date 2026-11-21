package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/vcs/git"
)

// checkCommand builds the check group.
func checkCommand() *cli.Command {
	return &cli.Command{
		Name:    "check",
		Summary: "Check the map and the changes of a range against it.",
		Usage: []string{
			"physa check [<range>] [--strict]",
			"physa check map [dir]",
			"physa check changes [<range>] [--json] [--strict]",
		},
		Run: func(args []string, stdout, stderr io.Writer) int {
			opts, ok := parseChanges(args, false)
			if !ok {
				return cli.BadUsage
			}
			return checkBare(".", opts, stdout, stderr)
		},
		Subs: []*cli.Command{
			{
				Name:    "map",
				Summary: "Check the map: 0 when sound, 1 with problems, 2 when it cannot be read.",
				Usage:   []string{"physa check map [dir]"},
				Run: func(args []string, stdout, stderr io.Writer) int {
					if len(args) > 1 {
						return cli.BadUsage
					}
					dir := "."
					if len(args) == 1 {
						dir = args[0]
					}
					return checkMap(dir, "physa check map", stdout, stderr)
				},
			},
			{
				Name:    "changes",
				Summary: "Check each change of a range, and the map as each leaves it.",
				Usage:   []string{"physa check changes [<range>] [--json] [--strict]"},
				Run: func(args []string, stdout, stderr io.Writer) int {
					opts, ok := parseChanges(args, true)
					if !ok {
						return cli.BadUsage
					}
					return checkChanges(".", "physa check changes", opts, stdout, stderr)
				},
			},
		},
	}
}

// checkBare is physa check: the change check when a range is given or the
// default range resolves, else the map check alone.
func checkBare(dir string, o changesOptions, stdout, stderr io.Writer) int {
	const name = "physa check"
	if !o.given {
		repo, err := git.Open(dir)
		if err != nil {
			return checkMap(dir, name, stdout, stderr)
		}
		if _, err := repo.Changes(o.rng); err != nil {
			return checkMap(dir, name, stdout, stderr)
		}
	}
	r, ok := runChanges(dir, name, o, stdout, stderr)
	if !ok {
		return 2
	}
	if len(r.Map.Problems) > 0 || (o.strict && !r.Clean()) {
		return 1
	}
	return 0
}

// checkMap reports the map in dir: 0 when it is sound, 1 with problems, 2 when
// it cannot be read. Its messages start with name.
func checkMap(dir, name string, stdout, stderr io.Writer) int {
	res, err := mapcheck.CheckWith(dir, mapcheck.Options{PRExists: prResolver(dir), OpenRepo: git.Open})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 2
	}
	if res.PRAnchors == mapcheck.PRAnchorsFormOnly && res.PRNote != "" {
		fmt.Fprintf(stderr, "%s: PR anchors checked for form only: %s\n", name, res.PRNote)
	}
	if len(res.Problems) > 0 {
		for _, p := range res.Problems {
			fmt.Fprintln(stderr, p.String())
		}
		return 1
	}
	fmt.Fprintf(stdout, "%s: ok (%s, %s)\n", name, plural(res.Capabilities, "capability", "capabilities"), plural(res.Entries, "entry", "entries"))
	return 0
}

// defaultRange is the range the change commands read when none is given.
const defaultRange = "origin/main..HEAD"

// changesOptions is a parsed change command line.
type changesOptions struct {
	rng          string
	given        bool // a range was named
	json, strict bool
}

// parseChanges reads at most one range and the flags --strict and, when
// allowJSON, --json, in any order. It reports false for anything else.
func parseChanges(args []string, allowJSON bool) (changesOptions, bool) {
	o := changesOptions{rng: defaultRange}
	for _, a := range args {
		switch {
		case a == "--json" && allowJSON:
			o.json = true
		case a == "--strict":
			o.strict = true
		case strings.HasPrefix(a, "--") || o.given:
			return o, false
		default:
			o.rng, o.given = a, true
		}
	}
	return o, true
}

// plural returns n with its noun in singular or plural form.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
