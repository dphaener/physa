// Package initrun is physa init as a command: it reads init's flags, runs the
// phases asked for against the repo's store, and prints what it did. Init is
// never bounded: it has no appetite and no circuit breaker, and is up front
// about its cost instead. Bare, it runs every phase in order, estimating the
// rest of the run in weighted tokens once it has gathered and asking before
// it goes on, and with --write it writes the map to physa/init through
// initbranch, then pushes it, opens its PR and installs the Session: hook
// when asked. The phases' own work is in gather, mapping, recovery,
// newcodebase and initbranch.
package initrun

import (
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/command"
)

// Usage is the command's synopsis lines.
var Usage = []string{
	"physa init [--yes] [--write [--push [--pr]] [--hooks]] [--agent NAME] [--answers FILE] [--since YYYY-MM-DD] [--max-prs N] [--json]",
	"physa init --gather [--since YYYY-MM-DD] [--max-prs N] [--agent NAME] [--json]",
	"physa init --propose [--agent NAME] [--json]",
	"physa init --recover [--only CAPABILITY]... [--agent NAME] [--write [--push [--pr]] [--hooks]] [--json]",
}

// Flags is a physa init command line as its declared flags read it. Since is
// the bound's start: the date given (SinceSet), or twelve months before now;
// MaxPRs is --max-prs, or 0 for no PR limit.
// Gather, Propose and Recover select one phase; none selects the full run.
// Agent is empty when not given; Only empty means every capability. Yes goes
// on past the full run's estimate without asking. Write, Push, PR and Hooks
// write physa/init, push it, open its PR and install the Session: hook;
// Answers is a new codebase's answers file.
type Flags struct {
	Gather, Propose, Recover bool
	Only                     []string
	Yes                      bool
	Agent                    string
	Since                    time.Time
	SinceSet                 bool
	MaxPRs                   int
	JSON                     bool
	Write, Push, PR, Hooks   bool
	Answers                  string

	only          []string // --only as typed, each value one or more comma-separated names
	since, maxPRs string
	fs            *pflag.FlagSet
}

// Full reports whether the flags select the full run: no phase named.
func (o Flags) Full() bool { return !o.Gather && !o.Propose && !o.Recover }

// Declare declares the flags of physa init on fs and starts o afresh, so it
// runs once for every command line: the switches, and the values --since
// (YYYY-MM-DD), --max-prs (at least 1), --agent, --answers and --only
// (repeatable or comma-separated).
func (o *Flags) Declare(fs *pflag.FlagSet) {
	*o = Flags{fs: fs}
	for name, on := range map[string]*bool{"gather": &o.Gather, "propose": &o.Propose, "recover": &o.Recover,
		"yes": &o.Yes, "json": &o.JSON, "write": &o.Write, "push": &o.Push, "pr": &o.PR, "hooks": &o.Hooks} {
		command.Switch(fs, name, on, "")
	}
	fs.StringArrayVar(&o.only, "only", nil, "recover only this capability")
	fs.StringVar(&o.Agent, "agent", "", "the agent to run")
	fs.StringVar(&o.Answers, "answers", "", "a new codebase's answers file")
	fs.StringVar(&o.since, "since", "", "gather from this day, YYYY-MM-DD")
	fs.StringVar(&o.maxPRs, "max-prs", "", "gather at most this many merged PRs (default: every PR of the bound)")
}

// read finishes the parsed line against now and reports whether it is a good
// one: no positionals, a real --since day, a --max-prs of at least 1, no
// empty --only name, at most one phase, --since and --max-prs with --gather
// or the full run, --only with --recover, --answers and --yes with the full
// run, --write with --recover or the full run, --push with --write, --pr
// with --push and --hooks with --write.
func (o *Flags) read(args []string, now time.Time) bool {
	o.Since, o.MaxPRs = now.AddDate(-1, 0, 0), 0 // no PR limit unless --max-prs gives one
	if o.SinceSet = o.fs.Changed("since"); o.SinceSet {
		t, err := time.Parse("2006-01-02", o.since)
		if err != nil {
			return false
		}
		o.Since = t
	}
	if o.fs.Changed("max-prs") {
		n, err := strconv.Atoi(o.maxPRs)
		if err != nil || n < 1 {
			return false
		}
		o.MaxPRs = n
	}
	o.Only = nil
	for _, v := range o.only {
		for _, c := range strings.Split(v, ",") {
			if c = strings.TrimSpace(c); c == "" {
				return false
			}
			o.Only = append(o.Only, c)
		}
	}
	gatherOnly := o.SinceSet || o.fs.Changed("max-prs")
	switch {
	case len(args) > 0,
		o.Gather && o.Propose, o.Gather && o.Recover, o.Propose && o.Recover,
		gatherOnly && (o.Propose || o.Recover),
		len(o.Only) > 0 && !o.Recover,
		(o.Answers != "" || o.Yes) && !o.Full(),
		o.Write && (o.Gather || o.Propose),
		o.Push && !o.Write,
		o.PR && !o.Push,
		o.Hooks && !o.Write:
		return false
	}
	return true
}
