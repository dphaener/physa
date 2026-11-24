package mapslice

import (
	"errors"
	"strings"

	"github.com/spf13/pflag"
)

// Flags is a parsed physa map slice command line: the request it makes and
// whether the slice is printed as JSON.
type Flags struct {
	Req  Request
	JSON bool
}

// Line is one physa map slice command line as its declared flags read it:
// capability names and --with and --format, or --entry and slugs, in any
// order, each flag as "--flag value" or "--flag=value". --entry takes no
// value of its own: every word after it is a slug, and --entry=slug names
// one; it may be repeated. --with may be repeated and adds up.
type Line struct {
	with   []string
	format string
	entry  entryFlag
}

// Declare declares the flags of physa map slice on fs and starts l afresh,
// so it runs once for every command line.
func (l *Line) Declare(fs *pflag.FlagSet) {
	*l = Line{entry: entryFlag{fs: fs}}
	fs.StringArrayVar(&l.with, "with", nil, "print decisions, questions or all in full")
	fs.StringVar(&l.format, "format", "md", "print the slice as md or json")
	f := fs.VarPF(&l.entry, "entry", "", "take the entries named by slug")
	f.NoOptDefVal = bareEntry
}

// Flags turns the line's positionals into the request it makes. It reports
// false for a bad --with or --format value, for --entry with capability
// names, with --with, or with no slug, and for no capability at all.
func (l *Line) Flags(args []string) (Flags, bool) {
	var o Flags
	switch l.format {
	case "md", "json":
		o.JSON = l.format == "json"
	default:
		return o, false
	}
	for _, w := range l.with {
		switch w {
		case "decisions":
			o.Req.WithDecisions = true
		case "questions":
			o.Req.WithQuestions = true
		case "all":
			o.Req.WithDecisions, o.Req.WithQuestions = true, true
		default:
			return o, false
		}
	}
	e := l.entry
	if !e.on {
		o.Req.Capabilities = args
		return o, len(args) > 0
	}
	if e.first > 0 || len(l.with) > 0 {
		return o, false
	}
	o.Req.Entries = e.merge(args)
	return o, len(o.Req.Entries) > 0
}

// bareEntry is the value pflag gives --entry when it is typed without one.
// No word on a command line can hold it.
const bareEntry = "\x00"

// entryFlag is --entry. Each time it is set it notes how many positionals
// pflag has read before it, so the slugs keep the order they were typed in
// and a word typed before the first --entry stays a capability name.
type entryFlag struct {
	fs    *pflag.FlagSet
	on    bool
	first int // positionals read before the first --entry
	slugs []slugAt
}

// slugAt is a slug named by --entry=slug, after the first at positionals.
type slugAt struct {
	at   int
	slug string
}

func (e *entryFlag) Type() string { return "entry" }

func (e *entryFlag) String() string {
	s := make([]string, len(e.slugs))
	for i, sl := range e.slugs {
		s[i] = sl.slug
	}
	return strings.Join(s, ",")
}

func (e *entryFlag) Set(v string) error {
	if v == "" {
		return errors.New("empty slug")
	}
	at := len(e.fs.Args())
	if !e.on {
		e.on, e.first = true, at
	}
	if v != bareEntry {
		e.slugs = append(e.slugs, slugAt{at, v})
	}
	return nil
}

// merge returns the slugs of --entry=slug among the positionals args, in the
// order they were typed.
func (e entryFlag) merge(args []string) []string {
	var out []string
	next := 0
	for i := 0; i <= len(args); i++ {
		for next < len(e.slugs) && e.slugs[next].at <= i {
			out = append(out, e.slugs[next].slug)
			next++
		}
		if i < len(args) {
			out = append(out, args[i])
		}
	}
	return out
}
