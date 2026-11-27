package main

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/learnbranch"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/mapindex"
	"github.com/dphaener/physa/internal/mapslice"
	"github.com/dphaener/physa/internal/vcs/git"
)

// mapCommand builds the map group.
func mapCommand() *cobra.Command {
	const learnUsage = "physa map learn [--signals] [--write [--push [--pr]]] [--since <change>] [--appetite <tokens>] [--agent <name>] [--json]"
	sliceUsage := []string{
		"physa map slice <capability>... [--with decisions|questions|all] [--format md|json]",
		"physa map slice --entry <slug>... [--format md|json]",
	}
	usage := append(append([]string{"physa map", "physa map index [dir]"}, sliceUsage...),
		"physa map changes [<range>] [--json] [--strict]",
		"physa map records [--json]",
		"physa map fmt [--check] [dir]",
		learnUsage,
		"physa map hooks",
	)
	var cs changesOptions
	var sl mapslice.Line
	var lf learnbranch.Flags
	var asJSON, check bool
	return command.Declare(command.New("map", "Read the map: a bare map prints the index.", usage,
		func(_ context.Context, args []string, stdout, stderr io.Writer) int {
			if len(args) > 0 {
				return command.BadUsage
			}
			return index(".", stdout, stderr)
		},
		command.New("index", "List the capabilities of the map with their promises.", []string{"physa map index [dir]"},
			func(_ context.Context, args []string, stdout, stderr io.Writer) int {
				if len(args) > 1 {
					return command.BadUsage
				}
				dir := "."
				if len(args) == 1 {
					dir = args[0]
				}
				return index(dir, stdout, stderr)
			}),
		command.Declare(command.New("slice", "Print the slice of the map a task needs.", sliceUsage,
			func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				opts, ok := sl.Flags(args)
				if !ok {
					return command.BadUsage
				}
				return slice(ctx, ".", opts, stdout, stderr)
			}), sl.Declare),
		command.Declare(command.New("changes", "List the capability each change of a range names, with the findings.", []string{"physa map changes [<range>] [--json] [--strict]"},
			func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				if !cs.takeRange(args) {
					return command.BadUsage
				}
				return changes(ctx, ".", cs, stdout, stderr)
			}), func(fs *pflag.FlagSet) { cs = changesOptions{}; cs.declare(fs, true) }),
		command.Declare(command.New("records", "List the slice records, newest first.", []string{"physa map records [--json]"},
			func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				if len(args) > 0 {
					return command.BadUsage
				}
				return records(ctx, ".", asJSON, stdout, stderr)
			}), func(fs *pflag.FlagSet) { command.Switch(fs, "json", &asJSON, "") }),
		command.Declare(command.New("fmt", "Rewrite the map's folders to their format: contents, index links and spacing (--check writes nothing and exits 1 when a file would change).", []string{"physa map fmt [--check] [dir]"},
			func(_ context.Context, args []string, stdout, stderr io.Writer) int {
				if len(args) > 1 {
					return command.BadUsage
				}
				dir := "."
				if len(args) == 1 {
					dir = args[0]
				}
				return format(dir, check, stdout, stderr)
			}), func(fs *pflag.FlagSet) { command.Switch(fs, "check", &check, "") }),
		command.Declare(command.New("learn", "Find what the merged changes have to teach the map, and draft proposals from it for the stewards to review (--signals stops at the signals; --write commits the kept proposals to physa/learn, --push pushes it, --pr opens its PR).", []string{learnUsage},
			func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				if len(args) > 0 || !lf.Valid() {
					return command.BadUsage
				}
				return learnMap(ctx, ".", lf, stdout, stderr)
			}), lf.Declare),
		command.Declare(command.New("hooks", "Point the repo's hooks at the one that stamps Session: on commits made in Claude Code, so learning can join a change to its session.", []string{"physa map hooks"},
			func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				if len(args) > 0 {
					return command.BadUsage
				}
				return hooks(ctx, ".", stdout, stderr)
			}), nil),
	), nil)
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

// slice prints what the slice command line o asks for from the map in dir:
// 0 when it printed, and 2 when the map has a problem physa check map would
// report, a capability or slug is unknown, or the slice cannot be written.
// Nothing is printed or recorded on a failure.
func slice(ctx context.Context, dir string, o mapslice.Flags, stdout, stderr io.Writer) int {
	m, err := mapslice.Read(ctx, dir)
	if err != nil {
		return failed(stderr, "slice", err)
	}
	var out bytes.Buffer
	if err := mapslice.Write(&out, m, o); err != nil {
		return failed(stderr, "slice", err)
	}
	if _, err := out.WriteTo(stdout); err != nil {
		return failed(stderr, "slice", err)
	}
	recordSlice(ctx, dir, o.Req, stderr)
	return 0
}

// changes lists the commits of a range with their capability, then the
// findings against the map as each commit leaves it. It returns 0 whatever the
// findings, 1 when o.strict and there are findings, and 2 when the map or the
// history cannot be read or the output cannot be written.
func changes(ctx context.Context, dir string, o changesOptions, stdout, stderr io.Writer) int {
	_, problems, err := mapfile.Read(dir)
	if err != nil {
		return failed(stderr, "changes", err)
	}
	noteProblems(stderr, "changes", len(problems))
	repo, err := git.Open(ctx, dir)
	if err != nil {
		return failed(stderr, "changes", err)
	}
	commits, err := history.Read(ctx, repo, o.rng)
	if err != nil {
		return failed(stderr, "changes", err)
	}
	findings, err := history.Check(ctx, repo, commits)
	if err != nil {
		return failed(stderr, "changes", err)
	}
	if err := history.WriteChanges(stdout, o.rng, commits, findings, o.json); err != nil {
		return failed(stderr, "changes", err)
	}
	if o.strict && len(findings) > 0 {
		return 1
	}
	return 0
}

// format formats the map in dir: 0 when nothing is outside its format or it
// was rewritten, 1 when check is set and a file would change or a file is
// outside its format, and 2 when the map cannot be read or written.
func format(dir string, check bool, stdout, stderr io.Writer) int {
	changed, problems, err := mapfolder.FormatMap(dir, !check)
	if err != nil {
		return failed(stderr, "fmt", err)
	}
	for _, p := range changed {
		if check {
			fmt.Fprintf(stdout, "%s: contents or spacing differ; run physa map fmt\n", p)
		} else {
			fmt.Fprintf(stdout, "formatted %s\n", p)
		}
	}
	for _, p := range problems {
		fmt.Fprintln(stderr, p)
	}
	if len(problems) > 0 || (check && len(changed) > 0) {
		return 1
	}
	return 0
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
