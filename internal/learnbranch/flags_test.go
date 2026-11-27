package learnbranch

import (
	"context"
	"io"
	"testing"

	"github.com/dphaener/physa/internal/command"
)

// parse runs args through a declared learn command, as physa map learn does,
// and returns the flags its job read, and whether the line was good.
func parse(args ...string) (Flags, bool) {
	var f Flags
	var got Flags
	c := command.Declare(command.New("learn", "", []string{"learn [--signals] [--write] [--push] [--pr] [--since x] [--appetite x] [--agent x] [--json]"},
		func(_ context.Context, pos []string, _, _ io.Writer) int {
			if len(pos) > 0 || !f.Valid() {
				return command.BadUsage
			}
			got = f
			return 0
		}), f.Declare)
	return got, command.Execute(context.Background(), c, args, io.Discard, io.Discard) == 0
}

func TestFlagsBadUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--since"}, {"--nosuch"}, {"x"}, {"--appetite", "0"}, {"--appetite=x"}, {"--appetite"},
		{"--appetite=-3"}, {"--agent"}, {"--agent="}, {"--since="}, {"--signals", "--appetite", "5"}, {"--signals", "--agent=claude"},
		{"--push"}, {"--pr"}, {"--write", "--pr"}, {"--signals", "--write"}, {"--write=x"}, {"-h"},
	} {
		if _, ok := parse(args...); ok {
			t.Errorf("parse(%v) ok, want not", args)
		}
	}
}

func TestFlagsValues(t *testing.T) {
	o, ok := parse("--signals", "--since", "abc", "--json")
	if !ok || o.Since != "abc" || !o.JSON || !o.Signals {
		t.Errorf("got %+v, %v", o, ok)
	}
	o, ok = parse("--write", "--push", "--pr", "--appetite=9", "--agent", "claude")
	if !ok || !o.Write || !o.Push || !o.PR || o.Appetite != 9 || o.Agent != "claude" {
		t.Errorf("got %+v, %v", o, ok)
	}
}

// TestFlagsStartAfresh runs two lines on one command: the second must not
// see the first's flags.
func TestFlagsStartAfresh(t *testing.T) {
	var f Flags
	var seen []Flags
	c := command.Declare(command.New("learn", "", []string{"learn"},
		func(_ context.Context, _ []string, _, _ io.Writer) int {
			seen = append(seen, f)
			return 0
		}), f.Declare)
	command.Execute(t.Context(), c, []string{"--appetite", "5", "--json"}, io.Discard, io.Discard)
	command.Execute(t.Context(), c, nil, io.Discard, io.Discard)
	if len(seen) != 2 || seen[1].Appetite != 0 || seen[1].JSON || seen[1].fs.Changed("appetite") {
		t.Errorf("second line saw %+v", seen)
	}
}
