// Package cli runs a command line against a tree of grouped commands: each
// group names a glossary noun, each subcommand a verb, a bare group does its
// most common job, and every command prints its help with --help. Names match
// exactly: there are no aliases, no short flags and no prefix matching.
package cli

import (
	"fmt"
	"io"
	"strings"
)

// BadUsage is what a Run returns for arguments it cannot parse. Dispatch then
// prints the command's help to stderr and returns 2.
const BadUsage = -1

// Command is one node of the tree.
type Command struct {
	Name    string
	Summary string
	// Usage holds synopsis lines, each a full command line such as
	// "physa map index [dir]".
	Usage []string
	// Run does the command's job with the arguments left after its name.
	// It is nil for a group with no bare job.
	Run  func(args []string, stdout, stderr io.Writer) int
	Subs []*Command
}

// Dispatch walks args down the tree from root and runs the command it
// reaches. It returns the exit code: Run's own, 0 for --help, 2 for usage
// errors.
func Dispatch(root *Command, args []string, stdout, stderr io.Writer) int {
	cur := root
	for len(args) > 0 {
		next := cur.sub(args[0])
		if next == nil {
			break
		}
		cur, args = next, args[1:]
	}
	for _, a := range args {
		if a == "--help" {
			Help(cur, stdout)
			return 0
		}
	}
	if cur.Run == nil {
		Help(cur, stderr)
		return 2
	}
	code := cur.Run(args, stdout, stderr)
	if code == BadUsage {
		Help(cur, stderr)
		return 2
	}
	return code
}

func (c *Command) sub(name string) *Command {
	for _, s := range c.Subs {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Help prints c's usage lines, its summary and its subcommands. The output
// depends only on the tree.
func Help(c *Command, w io.Writer) {
	usage := c.Usage
	if len(usage) == 0 {
		usage = []string{c.Name}
	}
	for i, u := range usage {
		if i == 0 {
			fmt.Fprintf(w, "usage: %s\n", u)
		} else {
			fmt.Fprintf(w, "       %s\n", u)
		}
	}
	if c.Summary != "" {
		fmt.Fprintf(w, "\n%s\n", c.Summary)
	}
	if len(c.Subs) == 0 {
		return
	}
	width := 0
	for _, s := range c.Subs {
		width = max(width, len(s.Name))
	}
	fmt.Fprint(w, "\nCommands:\n")
	for _, s := range c.Subs {
		fmt.Fprintf(w, "  %s  %s\n", s.Name+strings.Repeat(" ", width-len(s.Name)), s.Summary)
	}
}
