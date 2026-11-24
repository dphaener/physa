// Package command runs physa's commands on Cobra. Cobra holds the tree of
// grouped commands, routes a command line to the command it names and hands
// out help. A command runs in one of two modes until every command has moved:
// in raw mode it keeps its own arguments and parses them itself, so Cobra
// parses none of its flags; in declared mode it declares its flags once, on
// its own Cobra command, pflag parses them and its job gets the positionals.
// --help anywhere after a command's name prints its help to stdout; a bad
// line, a bad flag included, prints that command's help to stderr and exits
// 2, with no error text of its own. Names match exactly: there are no
// aliases, no short flags (-h included), no prefix matching and no
// suggestions. Shell completion comes from the declarations through a hidden
// completion command. Drift ties each declared command's usage lines to its
// flags.
package command

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func init() {
	// Help lists a group's commands in the order they were declared.
	cobra.EnableCommandSorting = false
}

// BadUsage is what a Run returns for arguments it cannot parse. The command's
// help is then printed to stderr and the exit code is 2.
const BadUsage = -1

// Run does a command's job with the arguments left after its name and returns
// its exit code, or BadUsage.
type Run func(args []string, stdout, stderr io.Writer) int

// usageKey is the annotation that holds a command's usage lines.
const usageKey = "usage"

// exitCode carries a command's non-zero exit code out of Cobra.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

// New builds a command named name with its summary, its usage lines (each a
// full command line such as "physa map index [dir]"), the job run does (nil
// for a group with no bare job) and its subcommands, in order.
func New(name, summary string, usage []string, run Run, subs ...*cobra.Command) *cobra.Command {
	c := &cobra.Command{
		Use:                name,
		Short:              summary,
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			// A declared command's --help was handled by Execute, before pflag.
			raw := declared(c) == nil
			if raw && slices.Contains(args, "--help") {
				Help(c, c.OutOrStdout())
				return nil
			}
			if !raw && emptyValue(c.Flags()) {
				Help(c, c.ErrOrStderr())
				return exitCode(2)
			}
			if run == nil {
				Help(c, c.ErrOrStderr())
				return exitCode(2)
			}
			code := run(args, c.OutOrStdout(), c.ErrOrStderr())
			if code == BadUsage {
				Help(c, c.ErrOrStderr())
				code = 2
			}
			if code != 0 {
				return exitCode(code)
			}
			return nil
		},
	}
	if len(usage) > 0 {
		c.Annotations = map[string]string{usageKey: strings.Join(usage, "\n")}
	}
	c.AddCommand(subs...)
	return c
}

// Execute runs one command line against root and returns the process exit
// code: the command's own, 0 for --help, 2 for a bad line. The root's own
// help subcommand, if it has one, is its help command; without one, help is
// a word like any other and Cobra adds none.
func Execute(root *cobra.Command, args []string, stdout, stderr io.Writer) int {
	root.SetArgs(append([]string{}, args...)) // never nil: nil makes Cobra read os.Args
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.DisableSuggestions = true
	root.CompletionOptions.HiddenDefaultCmd = true
	root.SetHelpCommand(helpCommand(root))
	root.SetHelpFunc(func(c *cobra.Command, _ []string) { Help(c, c.OutOrStdout()) })
	redeclareTree(root)
	if c, rest, err := root.Find(args); err == nil {
		if h := declared(c); h != nil {
			h.words = append([]string{}, rest...)
			if slices.Contains(rest, "--help") {
				Help(c, stdout)
				return 0
			}
		}
	}
	_, err := root.ExecuteC()
	var code exitCode
	switch {
	case err == nil:
		return 0
	case errors.As(err, &code):
		return int(code)
	default:
		fmt.Fprintln(stderr, err)
		return 2
	}
}

// Switch declares --name on fs as a switch bound to on, off by default: it
// is on when named bare and takes no value, so --name=false and --name=1 are
// bad flags, as they were before Cobra parsed physa's flags.
func Switch(fs *pflag.FlagSet, name string, on *bool, usage string) {
	*on = false
	fs.Var(&switchValue{on}, name, usage)
	fs.Lookup(name).NoOptDefVal = "true"
}

// switchValue is the value of a Switch.
type switchValue struct{ on *bool }

func (v *switchValue) String() string { return strconv.FormatBool(*v.on) }
func (v *switchValue) Type() string   { return "bool" }

func (v *switchValue) Set(s string) error {
	if s != "true" {
		return fmt.Errorf("%q: a switch takes no value", s)
	}
	*v.on = true
	return nil
}

// Resetter is a flag value that keeps state its declaration does not build
// afresh. Its Reset puts it back to its default before each command line.
type Resetter interface {
	Reset()
}

// Declare switches c, built by New, to declared mode: flags declares c's
// flags on fs, and pflag parses them, so c's job gets only its positionals.
// flags runs again before every command line Execute runs, so each line
// starts from the defaults; a value it does not build afresh implements
// Resetter. A bad flag, a missing or invalid value, a shorthand such as -h
// and an empty string value print c's help to stderr and exit 2. flags may
// be nil for a command that takes none; it must not declare help.
func Declare(c *cobra.Command, flags func(fs *pflag.FlagSet)) *cobra.Command {
	c.DisableFlagParsing = false
	c.SetFlagErrorFunc(func(c *cobra.Command, _ error) error {
		Help(c, c.ErrOrStderr())
		return exitCode(2)
	})
	redeclare(c, &helpFlag{declare: flags})
	return c
}

// Words returns the words typed after c's name on the line Execute is
// running, in order and unparsed, or nil for a command in raw mode.
func Words(c *cobra.Command) []string {
	if h := declared(c); h != nil {
		return append([]string{}, h.words...)
	}
	return nil
}

// helpFlag is a declared command's --help, hidden and without a shorthand.
// It also keeps the command's declaration and the words of the line being
// run, so a declared command carries its own state.
type helpFlag struct {
	on      bool
	declare func(*pflag.FlagSet)
	words   []string
}

func (h *helpFlag) String() string { return strconv.FormatBool(h.on) }
func (h *helpFlag) Type() string   { return "bool" }

// Set takes only true, as a Switch does: --help=false is a bad flag.
func (h *helpFlag) Set(s string) error {
	if s != "true" {
		return fmt.Errorf("%q: --help takes no value", s)
	}
	h.on = true
	return nil
}

// declared returns c's help flag when c is in declared mode, else nil.
func declared(c *cobra.Command) *helpFlag {
	if c.DisableFlagParsing {
		return nil
	}
	if f := c.Flags().Lookup("help"); f != nil {
		if h, ok := f.Value.(*helpFlag); ok {
			return h
		}
	}
	return nil
}

// redeclareTree gives every declared command under root fresh flags.
func redeclareTree(root *cobra.Command) {
	if h := declared(root); h != nil {
		redeclare(root, h)
	}
	for _, s := range root.Commands() {
		redeclareTree(s)
	}
}

// redeclare replaces c's flags with a fresh declaration, so every value is
// back to its default and no flag is changed. The help flag takes no
// optional value, so routing reads --help as it does in raw mode, and
// Execute prints help for a bare --help before pflag sees it.
func redeclare(c *cobra.Command, h *helpFlag) {
	c.ResetFlags()
	fs := c.Flags()
	if h.declare != nil {
		h.declare(fs)
	}
	h.on, h.words = false, nil
	fs.AddFlag(&pflag.Flag{Name: "help", Usage: "print this command's help", Value: h, DefValue: "false", Hidden: true})
	fs.VisitAll(func(f *pflag.Flag) {
		if r, ok := f.Value.(Resetter); ok {
			r.Reset()
		}
	})
}

// emptyValue reports whether a string flag on the line was given an empty
// value, or a string list flag an empty item.
func emptyValue(fs *pflag.FlagSet) bool {
	empty := false
	fs.Visit(func(f *pflag.Flag) {
		switch v := f.Value.(type) {
		case pflag.SliceValue:
			if f.Value.Type() == "stringArray" || f.Value.Type() == "stringSlice" {
				empty = empty || slices.Contains(v.GetSlice(), "")
			}
		default:
			empty = empty || f.Value.Type() == "string" && f.Value.String() == ""
		}
	})
	return empty
}

// Drift returns one line per mismatch between a declared command's usage
// lines and its flags: a visible flag its usage lines don't name, or a
// --flag they name that it doesn't declare. A usage line that names one of
// the command's subcommands belongs to that subcommand. Commands in raw mode
// are skipped.
func Drift(root *cobra.Command) []string {
	var problems []string
	if declared(root) != nil {
		problems = append(problems, drift(root)...)
	}
	for _, s := range root.Commands() {
		problems = append(problems, Drift(s)...)
	}
	return problems
}

// flagName matches a --flag named in a usage line.
var flagName = regexp.MustCompile(`--([A-Za-z0-9][A-Za-z0-9-]*)`)

// drift checks one declared command.
func drift(c *cobra.Command) []string {
	path := strings.Fields(c.CommandPath())
	var named []string
	for _, line := range usageLines(c) {
		words := strings.Fields(line)
		if len(words) > len(path) && slices.Equal(words[:len(path)], path) && isSub(c, words[len(path)]) {
			continue
		}
		for _, m := range flagName.FindAllStringSubmatch(line, -1) {
			if !slices.Contains(named, m[1]) {
				named = append(named, m[1])
			}
		}
	}
	var problems []string
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if !f.Hidden && f.Name != "help" && !slices.Contains(named, f.Name) {
			problems = append(problems, fmt.Sprintf("%s: declares --%s, which its usage lines don't name", c.CommandPath(), f.Name))
		}
	})
	for _, name := range named {
		if c.Flags().Lookup(name) == nil {
			problems = append(problems, fmt.Sprintf("%s: its usage lines name --%s, which it doesn't declare", c.CommandPath(), name))
		}
	}
	return problems
}

// isSub reports whether c has a subcommand named name.
func isSub(c *cobra.Command, name string) bool {
	for _, s := range c.Commands() {
		if s.Name() == name {
			return true
		}
	}
	return false
}

// usageLines returns c's usage lines, or its name when it has none.
func usageLines(c *cobra.Command) []string {
	if u, ok := c.Annotations[usageKey]; ok {
		return strings.Split(u, "\n")
	}
	return []string{c.Name()}
}

// helpCommand returns root's own help subcommand or, when it has none, a
// hidden one that hands the line to root's own job, as an unknown word would.
func helpCommand(root *cobra.Command) *cobra.Command {
	for _, s := range root.Commands() {
		if s.Name() == "help" {
			return s
		}
	}
	h := New("help", "", nil, nil)
	h.Hidden = true
	h.RunE = func(_ *cobra.Command, args []string) error {
		return root.RunE(root, append([]string{"help"}, args...))
	}
	return h
}

// Help prints c's usage lines, its summary and its visible subcommands in the
// order they were declared. The output depends only on the tree.
func Help(c *cobra.Command, w io.Writer) {
	for i, u := range usageLines(c) {
		if i == 0 {
			fmt.Fprintf(w, "usage: %s\n", u)
		} else {
			fmt.Fprintf(w, "       %s\n", u)
		}
	}
	if c.Short != "" {
		fmt.Fprintf(w, "\n%s\n", c.Short)
	}
	var subs []*cobra.Command
	width := 0
	for _, s := range c.Commands() {
		if !s.Hidden {
			subs = append(subs, s)
			width = max(width, len(s.Name()))
		}
	}
	if len(subs) == 0 {
		return
	}
	fmt.Fprint(w, "\nCommands:\n")
	for _, s := range subs {
		fmt.Fprintf(w, "  %s  %s\n", s.Name()+strings.Repeat(" ", width-len(s.Name())), s.Short)
	}
}
