package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapindex"
	"github.com/dphaener/physa/internal/mapslice"
	"github.com/dphaener/physa/internal/vcs/git"
)

// mapCommand builds the map group.
func mapCommand() *cli.Command {
	return &cli.Command{
		Name:    "map",
		Summary: "Read the map: a bare map prints the index.",
		Usage: []string{
			"physa map",
			"physa map index [dir]",
			"physa map slice <capability>... [--with decisions|questions|all] [--budget <tokens>] [--format md|json]",
			"physa map changes [<range>] [--json] [--strict]",
			"physa map records [--json]",
			"physa map learn [--signals] [--write [--push [--pr]]] [--since <change>] [--appetite <tokens>] [--agent <name>] [--json]",
		},
		Run: func(args []string, stdout, stderr io.Writer) int {
			if len(args) > 0 {
				return cli.BadUsage
			}
			return index(".", stdout, stderr)
		},
		Subs: []*cli.Command{
			{
				Name:    "index",
				Summary: "List the capabilities of the map with their promises.",
				Usage:   []string{"physa map index [dir]"},
				Run: func(args []string, stdout, stderr io.Writer) int {
					if len(args) > 1 {
						return cli.BadUsage
					}
					dir := "."
					if len(args) == 1 {
						dir = args[0]
					}
					return index(dir, stdout, stderr)
				},
			},
			{
				Name:    "slice",
				Summary: "Print the slice of the map a task needs.",
				Usage:   []string{"physa map slice <capability>... [--with decisions|questions|all] [--budget <tokens>] [--format md|json]"},
				Run: func(args []string, stdout, stderr io.Writer) int {
					opts, ok := parseSlice(args)
					if !ok {
						return cli.BadUsage
					}
					return slice(".", opts, stdout, stderr)
				},
			},
			{
				Name:    "changes",
				Summary: "List the capability each change of a range names, with the findings.",
				Usage:   []string{"physa map changes [<range>] [--json] [--strict]"},
				Run: func(args []string, stdout, stderr io.Writer) int {
					opts, ok := parseChanges(args, true)
					if !ok {
						return cli.BadUsage
					}
					return changes(".", opts, stdout, stderr)
				},
			},
			{
				Name:    "records",
				Summary: "List the slice records, newest first.",
				Usage:   []string{"physa map records [--json]"},
				Run: func(args []string, stdout, stderr io.Writer) int {
					asJSON, ok := parseRecords(args)
					if !ok {
						return cli.BadUsage
					}
					return records(".", asJSON, stdout, stderr)
				},
			},
			{
				Name:    "learn",
				Summary: "Find what the merged changes have to teach the map, and draft proposals from it for the stewards to review (--signals stops at the signals; --write commits the kept proposals to physa/learn, --push pushes it, --pr opens its PR).",
				Usage:   []string{"physa map learn [--signals] [--write [--push [--pr]]] [--since <change>] [--appetite <tokens>] [--agent <name>] [--json]"},
				Run: func(args []string, stdout, stderr io.Writer) int {
					opts, ok := parseLearn(args)
					if !ok {
						return cli.BadUsage
					}
					return learnMap(".", opts, stdout, stderr)
				},
			},
		},
	}
}

// index prints the index of the map in dir: 0 when it printed, even if the map
// has problems, and 2 when the map cannot be read or the index cannot be written.
func index(dir string, stdout, stderr io.Writer) int {
	m, problems, err := mapfile.Read(dir)
	if err != nil {
		return failed(stderr, "index", err)
	}
	if err := mapindex.Write(stdout, mapindex.Build(m)); err != nil {
		return failed(stderr, "index", err)
	}
	noteProblems(stderr, "index", len(problems))
	return 0
}

// sliceOptions is a parsed physa map slice command line.
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
		return failed(stderr, "slice", err)
	}
	s, err := mapslice.Build(m, o.req)
	if err != nil {
		return failed(stderr, "slice", err)
	}
	write := mapslice.WriteMD
	if o.json {
		write = mapslice.WriteJSON
	}
	if err := write(stdout, s); err != nil {
		return failed(stderr, "slice", err)
	}
	recordSlice(dir, o.req, stderr)
	noteProblems(stderr, "slice", len(problems))
	if s.OverBudget {
		fmt.Fprintf(stderr, "physa map slice: over budget: what is never cut is %d tokens, over the %d-token budget\n", s.Used, s.Budget)
	}
	return 0
}

// changes lists the commits of a range with their capability, then the
// findings against the map as each commit leaves it. It returns 0 whatever the
// findings, 1 when o.strict and there are findings, and 2 when the map or the
// history cannot be read or the output cannot be written.
func changes(dir string, o changesOptions, stdout, stderr io.Writer) int {
	_, problems, err := mapfile.Read(dir)
	if err != nil {
		return failed(stderr, "changes", err)
	}
	noteProblems(stderr, "changes", len(problems))
	repo, err := git.Open(dir)
	if err != nil {
		return failed(stderr, "changes", err)
	}
	commits, err := history.Read(repo, o.rng)
	if err != nil {
		return failed(stderr, "changes", err)
	}
	findings, err := history.Check(repo, commits)
	if err != nil {
		return failed(stderr, "changes", err)
	}
	if o.json {
		out := struct {
			Range    string            `json:"range"`
			Commits  []history.Commit  `json:"commits"`
			Findings []history.Finding `json:"findings"`
		}{o.rng, commits, findings}
		if err := json.NewEncoder(stdout).Encode(out); err != nil {
			return failed(stderr, "changes", err)
		}
	} else {
		for _, c := range commits {
			fmt.Fprintf(stdout, "%s %s\n  capability: %s\n", c.Short, c.Subject, slugList(c.Capabilities))
		}
		if len(findings) == 0 {
			fmt.Fprintf(stdout, "\nphysa map changes: ok (%d commits)\n", len(commits))
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

// failed prints err as the failure of the command named cmd and returns the
// exit code 2.
func failed(stderr io.Writer, cmd string, err error) int {
	fmt.Fprintf(stderr, "physa map %s: %v\n", cmd, err)
	return 2
}

// noteProblems tells on stderr that the map has n problems, when it has any.
func noteProblems(stderr io.Writer, cmd string, n int) {
	switch {
	case n == 1:
		fmt.Fprintf(stderr, "physa map %s: the map has 1 problem; run physa check map\n", cmd)
	case n > 1:
		fmt.Fprintf(stderr, "physa map %s: the map has %d problems; run physa check map\n", cmd, n)
	}
}
