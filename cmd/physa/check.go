package main

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/mapcheck"
)

// checkCommand builds the check group on d.
func checkCommand(d deps) *cobra.Command {
	usage := []string{
		"physa check [<range>] [--strict]",
		"physa check map [dir]",
		"physa check changes [<range>] [--json] [--strict]",
	}
	var bare, sub changesOptions
	return command.Declare(command.New("check", "Check the map and the changes of a range against it.", usage,
		func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			if !bare.takeRange(args) {
				return command.BadUsage
			}
			return d.checkBare(ctx, d.proc().Dir, bare, stdout, stderr)
		},
		command.New("map", "Check the map: 0 when sound, 1 with problems, 2 when it cannot be read.", []string{"physa check map [dir]"},
			func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				if len(args) > 1 {
					return command.BadUsage
				}
				dir := d.proc().Dir
				if len(args) == 1 {
					dir = args[0]
				}
				return d.checkMap(ctx, dir, "physa check map", stdout, stderr)
			}),
		command.Declare(command.New("changes", "Check each change of a range, and the map as each leaves it.", []string{"physa check changes [<range>] [--json] [--strict]"},
			func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				if !sub.takeRange(args) {
					return command.BadUsage
				}
				return d.checkChanges(ctx, d.proc().Dir, "physa check changes", sub, stdout, stderr)
			}), func(fs *pflag.FlagSet) { sub = changesOptions{}; sub.declare(fs, true) }),
	), func(fs *pflag.FlagSet) { bare = changesOptions{}; bare.declare(fs, false) })
}

// checkBare is physa check: the change check when a range is given or the
// default range resolves, else the map check alone.
func (d deps) checkBare(ctx context.Context, dir string, o changesOptions, stdout, stderr io.Writer) int {
	const name = "physa check"
	if !o.given {
		repo, err := d.openRepo(ctx, dir)
		if err != nil {
			return d.checkMap(ctx, dir, name, stdout, stderr)
		}
		if _, err := repo.Changes(ctx, o.rng); err != nil {
			return d.checkMap(ctx, dir, name, stdout, stderr)
		}
	}
	r, ok := d.runChanges(ctx, dir, name, o, stdout, stderr)
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
func (d deps) checkMap(ctx context.Context, dir, name string, stdout, stderr io.Writer) int {
	res, err := mapcheck.CheckWith(ctx, dir, mapcheck.Options{PRExists: d.prResolver(dir), OpenRepo: d.openRepo})
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

// declare declares --strict and, when withJSON, --json on fs, bound to o.
func (o *changesOptions) declare(fs *pflag.FlagSet, withJSON bool) {
	command.Switch(fs, "strict", &o.strict, "")
	if withJSON {
		command.Switch(fs, "json", &o.json, "")
	}
}

// takeRange reads at most one range from the positionals into o. It reports
// false for more.
func (o *changesOptions) takeRange(args []string) bool {
	o.rng, o.given = defaultRange, false
	if len(args) > 1 {
		return false
	}
	if len(args) == 1 {
		o.rng, o.given = args[0], true
	}
	return true
}

// plural returns n with its noun in singular or plural form.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
