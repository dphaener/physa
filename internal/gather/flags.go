package gather

import (
	"strconv"
	"strings"
	"time"
)

// Usage is the command's synopsis lines.
var Usage = []string{
	"physa init --gather [--since YYYY-MM-DD] [--max-prs N] [--json]",
	"physa init --propose [--appetite N] [--agent NAME] [--json]",
}

// DefaultMaxPRs is the most PRs gathered unless --max-prs says otherwise.
const DefaultMaxPRs = 1000

// Flags is a parsed physa init command line. Since is the bound's start:
// the date given, or twelve months before now. SinceSet is true when it was
// given. Propose selects init's proposing phase; Appetite is its most tokens
// (zero when not given) and Agent the agent it names (empty when not given).
type Flags struct {
	Gather   bool
	Propose  bool
	Appetite int64
	Agent    string
	Since    time.Time
	SinceSet bool
	MaxPRs   int
	JSON     bool
}

// ParseFlags reads the arguments after "init": --gather, --propose, --json,
// and the values --since (YYYY-MM-DD), --max-prs (at least 1), --appetite
// (at least 1) and --agent, each value also as --name=value. --propose cannot
// be combined with --gather, --since or --max-prs, and --appetite and --agent
// need --propose. It reports false for anything else.
func ParseFlags(args []string, now time.Time) (Flags, bool) {
	o := Flags{Since: now.AddDate(-1, 0, 0), MaxPRs: DefaultMaxPRs}
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
		case name != "--since" && name != "--max-prs" && name != "--appetite" && name != "--agent":
			return o, false
		}
		if !hasVal && i+1 < len(args) {
			i++
			val = args[i]
		}
		switch name {
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
	if o.Propose && (o.Gather || gatherOnly) {
		return o, false
	}
	if !o.Propose && (o.Appetite != 0 || o.Agent != "") {
		return o, false
	}
	return o, true
}
