// Package initrun is physa init as a command: it reads init's flags, runs the
// phase asked for against the repo's store, and prints what it did. The
// phases' own work is in gather and mapping.
package initrun

import (
	"github.com/dphaener/physa/internal/gather"
	"strconv"
	"strings"
	"time"
)

// Usage is the command's synopsis lines.
var Usage = []string{
	"physa init --gather [--since YYYY-MM-DD] [--max-prs N] [--json]",
	"physa init --propose [--appetite N] [--agent NAME] [--json]",
	"physa init --recover [--only CAPABILITY]... [--appetite N] [--agent NAME] [--json]",
}

// Flags is a parsed physa init command line. Since is the bound's start:
// the date given, or twelve months before now. SinceSet is true when it was
// given. Propose selects init's proposing phase; Appetite is its most tokens
// (zero when not given) and Agent the agent it names (empty when not given).
// Recover selects init's recovering phase, and Only the capabilities it is
// limited to (none means all).
type Flags struct {
	Gather   bool
	Propose  bool
	Recover  bool
	Only     []string
	Appetite int64
	Agent    string
	Since    time.Time
	SinceSet bool
	MaxPRs   int
	JSON     bool
}

// ParseFlags reads the arguments after "init": --gather, --propose, --json,
// --recover, and the values --since (YYYY-MM-DD), --max-prs (at least 1),
// --appetite (at least 1), --agent and --only (a capability name; repeatable
// or comma-separated), each value also as --name=value. --propose and
// --recover cannot be combined with each other, with --gather, --since or
// --max-prs; --appetite and --agent need one of them, and --only needs
// --recover. It reports false for anything else.
func ParseFlags(args []string, now time.Time) (Flags, bool) {
	o := Flags{Since: now.AddDate(-1, 0, 0), MaxPRs: gather.DefaultMaxPRs}
	gatherOnly := false
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		switch {
		case name == "--gather" && !hasVal:
			o.Gather = true
			continue
		case name == "--json" && !hasVal:
			o.JSON = true
			continue
		case name == "--propose" && !hasVal:
			o.Propose = true
			continue
		case name == "--recover" && !hasVal:
			o.Recover = true
			continue
		case name != "--since" && name != "--max-prs" && name != "--appetite" && name != "--agent" && name != "--only":
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
			continue
		case "--agent":
			if val == "" {
				return o, false
			}
			o.Agent = val
			continue
		case "--appetite":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || n < 1 {
				return o, false
			}
			o.Appetite = n
			continue
		}
		gatherOnly = true
		if name == "--since" {
			t, err := time.Parse("2006-01-02", val)
			if err != nil {
				return o, false
			}
			o.Since, o.SinceSet = t, true
			continue
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 {
			return o, false
		}
		o.MaxPRs = n
	}
	if (o.Propose || o.Recover) && (o.Gather || gatherOnly || (o.Propose && o.Recover)) {
		return o, false
	}
	if !o.Propose && !o.Recover && (o.Appetite != 0 || o.Agent != "") {
		return o, false
	}
	if len(o.Only) > 0 && !o.Recover {
		return o, false
	}
	return o, true
}
