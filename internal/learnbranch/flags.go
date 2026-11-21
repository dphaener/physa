package learnbranch

import (
	"strconv"
	"strings"
)

// Flags is a parsed physa map learn command line.
type Flags struct {
	Signals  bool
	Since    string
	JSON     bool
	Write    bool   // commit the kept proposals to physa/learn
	Push     bool   // push physa/learn; needs Write
	PR       bool   // open or refresh the PR; needs Push
	Appetite int64  // most tokens a draft session may use; zero is the default
	Agent    string // agent to draft with; empty is the default
}

// ParseFlags reads the arguments after "learn": the flags --signals, --json,
// --write, --push, --pr and the values --since, --appetite and --agent (each
// also as --name=value). It reports false for anything else, --appetite,
// --agent or --write with --signals, --push without --write, --pr without
// --push, and an appetite that is not a positive integer.
func ParseFlags(args []string) (Flags, bool) {
	var o Flags
	flags := map[string]*bool{"--signals": &o.Signals, "--json": &o.JSON, "--write": &o.Write, "--push": &o.Push, "--pr": &o.PR}
	strs := map[string]*string{"--since": &o.Since, "--agent": &o.Agent}
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(args[i], "=")
		if b, ok := flags[name]; ok && !hasVal {
			*b = true
			continue
		}
		if !hasVal && i+1 < len(args) {
			i++
			val = args[i]
		}
		if val == "" {
			return o, false
		}
		if p, ok := strs[name]; ok {
			*p = val
		} else if n, err := strconv.ParseInt(val, 10, 64); name == "--appetite" && err == nil && n > 0 {
			o.Appetite = n
		} else {
			return o, false
		}
	}
	valid := !o.Signals || (o.Appetite == 0 && o.Agent == "" && !o.Write)
	return o, valid && (!o.Push || o.Write) && (!o.PR || o.Push)
}
