// Package initrun is physa init as a command: it reads init's flags, runs the
// phases asked for against the repo's store, and prints what it did. Bare, it
// runs every phase in order under one appetite, shown before any token is
// spent, and with --write it writes the map to physa/init through
// initbranch, then pushes it, opens its PR and installs the Session: hook
// when asked. The phases' own work is in gather, mapping, recovery,
// newcodebase and initbranch.
package initrun

import (
	"strconv"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/gather"
)

// Usage is the command's synopsis lines.
var Usage = []string{
	"physa init [--write [--push [--pr]] [--hooks]] [--appetite N] [--agent NAME] [--answers FILE] [--since YYYY-MM-DD] [--max-prs N] [--json]",
	"physa init --gather [--since YYYY-MM-DD] [--max-prs N] [--json]",
	"physa init --propose [--appetite N] [--agent NAME] [--json]",
	"physa init --recover [--only CAPABILITY]... [--appetite N] [--agent NAME] [--write [--push [--pr]] [--hooks]] [--json]",
}

// Flags is a parsed physa init command line. Since is the bound's start:
// the date given (SinceSet), or twelve months before now. Gather, Propose
// and Recover select one phase; none selects the full run. Appetite and
// Agent are zero when not given; Only empty means every capability. Write,
// Push, PR and Hooks write physa/init, push it, open its PR and install the
// Session: hook; Answers is a new codebase's answers file.
type Flags struct {
	Gather, Propose, Recover bool
	Only                     []string
	Appetite                 int64
	Agent                    string
	Since                    time.Time
	SinceSet                 bool
	MaxPRs                   int
	JSON                     bool
	Write, Push, PR, Hooks   bool
	Answers                  string
}

// Full reports whether the flags select the full run: no phase named.
func (o Flags) Full() bool { return !o.Gather && !o.Propose && !o.Recover }

// switches are the flags that take no value, and what each sets.
var switches = map[string]func(*Flags){
	"--gather":  func(o *Flags) { o.Gather = true },
	"--json":    func(o *Flags) { o.JSON = true },
	"--propose": func(o *Flags) { o.Propose = true },
	"--recover": func(o *Flags) { o.Recover = true },
	"--write":   func(o *Flags) { o.Write = true },
	"--push":    func(o *Flags) { o.Push = true },
	"--pr":      func(o *Flags) { o.PR = true },
	"--hooks":   func(o *Flags) { o.Hooks = true },
}

// ParseFlags reads the arguments after "init": the switches above, and the
// values --since (YYYY-MM-DD), --max-prs and --appetite (at least 1),
// --agent, --answers and --only (repeatable or comma-separated), each also
// as --name=value. At most one phase is named. --since and --max-prs go with
// --gather or the full run; --appetite and --agent with any but --gather;
// --only with --recover; --answers with the full run; --write with
// --recover or the full run. --push needs --write, --pr --push and --hooks
// --write. It reports false for anything else.
func ParseFlags(args []string, now time.Time) (Flags, bool) {
	o := Flags{Since: now.AddDate(-1, 0, 0), MaxPRs: gather.DefaultMaxPRs}
	gatherOnly := false
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		if set, ok := switches[name]; ok && !hasVal {
			set(&o)
			continue
		}
		switch name {
		case "--since", "--max-prs", "--appetite", "--agent", "--only", "--answers":
		default:
			return o, false
		}
		if !hasVal && i+1 < len(args) {
			i++
			val = args[i]
		}
		switch name {
		case "--only":
			for _, c := range strings.Split(val, ",") {
				if c = strings.TrimSpace(c); c == "" {
					return o, false
				}
				o.Only = append(o.Only, c)
			}
		case "--agent", "--answers":
			if val == "" {
				return o, false
			}
			if name == "--agent" {
				o.Agent = val
			} else {
				o.Answers = val
			}
		case "--appetite":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || n < 1 {
				return o, false
			}
			o.Appetite = n
		case "--since":
			t, err := time.Parse("2006-01-02", val)
			if err != nil {
				return o, false
			}
			o.Since, o.SinceSet, gatherOnly = t, true, true
		default:
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return o, false
			}
			o.MaxPRs, gatherOnly = n, true
		}
	}
	return o, o.valid(gatherOnly)
}

// valid reports whether the flags go together; gatherOnly says --since or
// --max-prs was given.
func (o Flags) valid(gatherOnly bool) bool {
	switch {
	case o.Gather && o.Propose, o.Gather && o.Recover, o.Propose && o.Recover,
		gatherOnly && (o.Propose || o.Recover),
		o.Gather && (o.Appetite != 0 || o.Agent != ""),
		len(o.Only) > 0 && !o.Recover,
		o.Answers != "" && !o.Full(),
		o.Write && (o.Gather || o.Propose),
		o.Push && !o.Write,
		o.PR && !o.Push,
		o.Hooks && !o.Write:
		return false
	}
	return true
}
