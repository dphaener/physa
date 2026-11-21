package mapslice

import (
	"strconv"
	"strings"
)

// Flags is a parsed physa map slice command line: the request it makes and
// whether the slice is printed as JSON.
type Flags struct {
	Req  Request
	JSON bool
}

// ParseFlags reads the arguments after "slice": capability names and the flags
// --with, --budget and --format, in any order, as "--flag value" or
// "--flag=value". It reports false for anything else.
func ParseFlags(args []string) (Flags, bool) {
	var o Flags
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			o.Req.Capabilities = append(o.Req.Capabilities, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		if !hasVal {
			if i+1 >= len(args) {
				return o, false
			}
			i++
			val = args[i]
		}
		switch name {
		case "with":
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
		case "budget":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return o, false
			}
			o.Req.Budget = n
		case "format":
			if val != "md" && val != "json" {
				return o, false
			}
			o.JSON = val == "json"
		default:
			return o, false
		}
	}
	return o, len(o.Req.Capabilities) > 0
}
