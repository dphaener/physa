package learnbranch

import (
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/command"
)

// Flags is a physa map learn command line as its declared flags read it.
type Flags struct {
	Signals  bool
	Since    string
	JSON     bool
	Write    bool   // commit the kept proposals to physa/learn
	Push     bool   // push physa/learn; needs Write
	PR       bool   // open or refresh the PR; needs Push
	Appetite int64  // most tokens a draft session may use; zero is the default
	Agent    string // agent to draft with; empty is the default

	fs *pflag.FlagSet
}

// Declare declares the flags of physa map learn on fs and starts f afresh, so
// it runs once for every command line.
func (f *Flags) Declare(fs *pflag.FlagSet) {
	*f = Flags{fs: fs}
	command.Switch(fs, "signals", &f.Signals, "stop at the signals")
	fs.StringVar(&f.Since, "since", "", "learn from the changes after this change")
	command.Switch(fs, "json", &f.JSON, "print JSON")
	command.Switch(fs, "write", &f.Write, "commit the kept proposals to physa/learn")
	command.Switch(fs, "push", &f.Push, "push physa/learn")
	command.Switch(fs, "pr", &f.PR, "open or refresh the PR")
	fs.Int64Var(&f.Appetite, "appetite", 0, "most tokens a draft session may use")
	fs.StringVar(&f.Agent, "agent", "", "agent to draft with")
}

// Valid reports whether the parsed line is a good one: --appetite a positive
// integer, no --appetite, --agent or --write with --signals, no --push
// without --write and no --pr without --push.
func (f *Flags) Valid() bool {
	if f.fs.Changed("appetite") && f.Appetite <= 0 {
		return false
	}
	ok := !f.Signals || (!f.fs.Changed("appetite") && f.Agent == "" && !f.Write)
	return ok && (!f.Push || f.Write) && (!f.PR || f.Push)
}
