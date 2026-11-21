package gather

import (
	"strconv"
	"strings"
	"time"
)

// Usage is the command's synopsis lines.
var Usage = []string{"physa init --gather [--since YYYY-MM-DD] [--max-prs N] [--json]"}

// DefaultMaxPRs is the most PRs gathered unless --max-prs says otherwise.
const DefaultMaxPRs = 1000

// Flags is a parsed physa init command line. Since is the bound's start:
// the date given, or twelve months before now. SinceSet is true when it was
// given.
type Flags struct {
	Gather   bool
	Since    time.Time
	SinceSet bool
	MaxPRs   int
	JSON     bool
}

// ParseFlags reads the arguments after "init": --gather, --json, and the
// values --since (YYYY-MM-DD) and --max-prs (at least 1), each value also as
// --name=value. It reports false for anything else.
func ParseFlags(args []string, now time.Time) (Flags, bool) {
	o := Flags{Since: now.AddDate(-1, 0, 0), MaxPRs: DefaultMaxPRs}
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		switch {
		case name == "--gather" && !hasVal:
			o.Gather = true
			continue
		case name == "--json" && !hasVal:
			o.JSON = true
			continue
		case name != "--since" && name != "--max-prs":
			return o, false
		}
		if !hasVal && i+1 < len(args) {
			i++
			val = args[i]
		}
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
	return o, true
}
