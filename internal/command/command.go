// Package command runs physa's commands on Cobra. Cobra holds the tree of
// grouped commands, routes a command line to the command it names and hands
// out help; each command keeps its own arguments and parses them itself, so
// Cobra parses no flag. --help anywhere after a command's name prints its help
// to stdout; a bad line prints that command's help to stderr and exits 2.
// Names match exactly: there are no aliases, no short flags, no prefix
// matching, no suggestions and no completion command.
package command

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
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
			for _, a := range args {
				if a == "--help" {
					Help(c, c.OutOrStdout())
					return nil
				}
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
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpCommand(helpCommand(root))
	root.SetHelpFunc(func(c *cobra.Command, _ []string) { Help(c, c.OutOrStdout()) })
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
	usage := []string{c.Name()}
	if u, ok := c.Annotations[usageKey]; ok {
		usage = strings.Split(u, "\n")
	}
	for i, u := range usage {
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
