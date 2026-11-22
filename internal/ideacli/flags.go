package ideacli

import (
	"slices"
	"strings"
)

// Flags are a command line's flags and the arguments left.
type Flags struct {
	JSON, Yes                    bool
	Slug, From, Appetite, Reason string
	Args                         []string
}

// valued are the flags that take a value.
var valued = []string{"slug", "from", "appetite", "reason"}

// Parse reads args, taking only the flags named in allowed: --json and
// --yes alone, and --slug, --from, --appetite and --reason each with a
// non-empty value (--name value or --name=value). A lone "-" is an
// argument. ok is false for any other flag, a short flag, a flag given
// twice, or a missing value.
func Parse(args []string, allowed ...string) (f Flags, ok bool) {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-" || !strings.HasPrefix(a, "-") {
			f.Args = append(f.Args, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !strings.HasPrefix(a, "--") || seen[name] || !slices.Contains(allowed, name) {
			return f, false
		}
		seen[name] = true
		if !slices.Contains(valued, name) {
			if hasVal {
				return f, false
			}
			f.JSON = f.JSON || name == "json"
			f.Yes = f.Yes || name == "yes"
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return f, false
			}
			i++
			val = args[i]
		}
		if val == "" {
			return f, false
		}
		switch name {
		case "slug":
			f.Slug = val
		case "from":
			f.From = val
		case "appetite":
			f.Appetite = val
		case "reason":
			f.Reason = val
		}
	}
	return f, true
}
