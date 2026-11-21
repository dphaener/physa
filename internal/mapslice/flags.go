package mapslice

import "strings"

// Flags is a parsed physa map slice command line: the request it makes and
// whether the slice is printed as JSON.
type Flags struct {
	Req  Request
	JSON bool
}

// ParseFlags reads the arguments after "slice": capability names and the
// flags --with and --format, or --entry and slugs, in any order, each flag as
// "--flag value" or "--flag=value". --entry takes no value of its own: every
// bare word after it is a slug, and --entry=slug names one; it may be
// repeated. --format md|json works with both forms. It reports false for
// anything else, and for --entry with capability names, with --with, or with
// no slug.
func ParseFlags(args []string) (Flags, bool) {
	var o Flags
	entry, with := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			if entry {
				o.Req.Entries = append(o.Req.Entries, a)
			} else {
				o.Req.Capabilities = append(o.Req.Capabilities, a)
			}
			continue
		}
		name, val, hasVal := a[2:], "", false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name, val, hasVal = name[:eq], name[eq+1:], true
		}
		if name == "entry" {
			if hasVal {
				if val == "" {
					return o, false
				}
				o.Req.Entries = append(o.Req.Entries, val)
			}
			entry = true
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return o, false
			}
			i++
			val = args[i]
		}
		switch name {
		case "with":
			with = true
			switch val {
			case "decisions":
				o.Req.WithDecisions = true
			case "questions":
				o.Req.WithQuestions = true
			case "all":
				o.Req.WithDecisions, o.Req.WithQuestions = true, true
			default:
				return o, false
			}
		case "format":
			if val != "md" && val != "json" {
				return o, false
			}
			o.JSON = val == "json"
		default:
			return o, false
		}
	}
	if entry {
		return o, len(o.Req.Entries) > 0 && len(o.Req.Capabilities) == 0 && !with
	}
	return o, len(o.Req.Capabilities) > 0
}
