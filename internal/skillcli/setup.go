package skillcli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/adapter/claude"
	"github.com/dphaener/physa/internal/agentenv"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/skillstub"
)

const (
	setupUsage  = "physa skill setup [--yes] [--json]"
	statusUsage = "physa skill status [--json]"
	removeUsage = "physa skill remove [--yes] [--json]"
	groupUsage  = "physa skill [--json]"
)

// env is what the commands reach outside themselves for, so tests can stand
// in a temporary home and a terminal.
type env struct {
	home        func() (string, error)
	interactive func() bool
	confirm     func(question string) (bool, error)
}

func defaultEnv() env {
	return env{
		home: os.UserHomeDir,
		interactive: func() bool {
			_, agent := agentenv.Detect(os.Getenv)
			return output.Interactive(os.Stdin, os.Stdout, agent)
		},
		// The question goes to stderr, so stdout stays for the result.
		confirm: func(q string) (bool, error) { return picker.Confirm(os.Stdin, os.Stderr, q) },
	}
}

// target is an agent whose skills directory holds Physa's stub.
type target struct {
	name  string // as --json names it
	label string // as people read it
	path  func(home string) string
}

// targets lists the agents the stub is written for; today only Claude Code.
var targets = []target{
	{claude.Name, "Claude Code", func(home string) string { return claude.SkillFile(home, "physa") }},
}

// entry is one agent's stub: where it is, what it was, and what the command did.
type entry struct {
	t      target
	path   string
	before skillstub.State
	state  skillstub.State
	action string
}

type agentJSON struct {
	Agent  string `json:"agent"`
	Path   string `json:"path"`
	State  string `json:"state"`
	Action string `json:"action,omitempty"`
}

func newCommand(e env) *cobra.Command {
	var groupJSON, statusJSON, setupJSON, setupYes, removeJSON, removeYes bool
	jsonFlag := func(fs *pflag.FlagSet, on *bool) { command.Switch(fs, "json", on, "") }
	status := func(asJSON *bool) command.Run {
		return func(args []string, stdout, stderr io.Writer) int { return e.status(args, *asJSON, stdout, stderr) }
	}
	return command.Declare(command.New("skill",
		"Set up, check, print and remove Physa's skill, the guide a person's agent loads to use Physa's commands and terms. "+
			"setup writes a short stub into Claude Code's skills directory (~/.claude/skills/physa) that tells the agent to run `physa skill show`; "+
			"status says whether it is current, stale or missing; remove deletes it. setup and remove ask on a terminal and need --yes off one, "+
			"and never touch a SKILL.md Physa did not write. A bare skill is status.",
		[]string{groupUsage, setupUsage, statusUsage, "physa skill show", removeUsage}, status(&groupJSON),
		command.Declare(command.New("setup",
			"Write Physa's stub to ~/.claude/skills/physa/SKILL.md, replacing a stale one; a SKILL.md Physa did not write is left as it is.",
			[]string{setupUsage}, func(args []string, stdout, stderr io.Writer) int {
				return e.change(verbs[0], args, setupYes, setupJSON, stdout, stderr)
			}), func(fs *pflag.FlagSet) {
			command.Switch(fs, "yes", &setupYes, "")
			jsonFlag(fs, &setupJSON)
		}),
		command.Declare(command.New("status", "Say whether Physa's stub is current, stale or missing for each agent.",
			[]string{statusUsage}, status(&statusJSON)), func(fs *pflag.FlagSet) { jsonFlag(fs, &statusJSON) }),
		command.New("show", "Print the guide, as it is, on any output.", []string{showUsage}, show),
		command.Declare(command.New("remove",
			"Delete Physa's stub; a SKILL.md Physa did not write is left as it is.",
			[]string{removeUsage}, func(args []string, stdout, stderr io.Writer) int {
				return e.change(verbs[1], args, removeYes, removeJSON, stdout, stderr)
			}), func(fs *pflag.FlagSet) {
			command.Switch(fs, "yes", &removeYes, "")
			jsonFlag(fs, &removeJSON)
		}),
	), func(fs *pflag.FlagSet) { jsonFlag(fs, &groupJSON) })
}

// load reads every agent's stub under the home directory.
func (e env) load(verb string, stderr io.Writer) ([]entry, bool) {
	home, err := e.home()
	if err == nil && home == "" {
		err = errors.New("no home directory")
	}
	var all []entry
	for _, t := range targets {
		if err != nil {
			break
		}
		p := t.path(home)
		var st skillstub.State
		st, err = skillstub.Read(p)
		all = append(all, entry{t: t, path: p, before: st, state: st})
	}
	if err != nil {
		fmt.Fprintf(stderr, "physa skill %s: %v\n", verb, err)
		return nil, false
	}
	return all, true
}

func shown(s skillstub.State) string {
	if s == skillstub.Other {
		return "not written by Physa"
	}
	return s.String()
}

func (e env) status(args []string, asJSON bool, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return command.BadUsage
	}
	all, ok := e.load("status", stderr)
	if !ok {
		return 1
	}
	if asJSON {
		return emit("status", all, stdout, stderr)
	}
	rows := make([][]string, 0, len(all))
	for _, a := range all {
		rows = append(rows, []string{a.t.label, shown(a.state), a.path})
	}
	output.For(stdout).Table([]string{"agent", "state", "path"}, rows)
	return 0
}

func emit(verb string, all []entry, stdout, stderr io.Writer) int {
	out := struct {
		Agents []agentJSON `json:"agents"`
	}{Agents: []agentJSON{}}
	for _, a := range all {
		out.Agents = append(out.Agents, agentJSON{a.t.name, a.path, a.state.String(), a.action})
	}
	if err := output.JSON(stdout, out); err != nil {
		fmt.Fprintf(stderr, "physa skill %s: %v\n", verb, err)
		return 1
	}
	return 0
}

// verb is what setup or remove does: the state it acts on, how it words
// itself, and the stub call that does it.
type verb struct {
	name     string
	doneWord string // the action it reports
	question string
	noop     skillstub.State // the state that needs nothing
	noopText string
	nothing  string // said when the person declines
	do       func(path string) error
}

var verbs = [2]verb{
	{"setup", "written", "Write Physa's skill?", skillstub.Current, "already current", "nothing written", skillstub.Write},
	{"remove", "removed", "Remove Physa's skill?", skillstub.Missing, "nothing to remove", "nothing removed", skillstub.Remove},
}

// change is setup or remove: refuse what Physa did not write, skip what
// needs nothing, and do the rest once told to, by --yes or a person's yes.
func (e env) change(v verb, args []string, yes, asJSON bool, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return command.BadUsage
	}
	all, ok := e.load(v.name, stderr)
	if !ok {
		return 1
	}
	var todo []*entry
	refused := false
	for i := range all {
		a := &all[i]
		switch {
		case a.before == skillstub.Other:
			a.action = "refused"
			refused = true
			fmt.Fprintf(stderr, "physa skill %s: %s was not written by Physa; left as it is\n", v.name, a.path)
		case a.before == v.noop:
			a.action = "none"
		default:
			todo = append(todo, a)
		}
	}
	code := 0
	if refused {
		code = 2
	}
	if len(todo) > 0 && !yes {
		if !e.interactive() {
			fmt.Fprintf(stderr, "physa skill %s: with no terminal to ask on, %s changes things only when told to; %s. Rerun with --yes: physa skill %s --yes\n",
				v.name, v.name, v.nothing, v.name)
			return finish(v, all, todo, 2, asJSON, stdout, stderr)
		}
		for _, a := range todo {
			fmt.Fprintf(stdout, "%s: %s, %s -> %s\n", a.t.label, a.path, a.before, afterState(v))
		}
		yes, err := e.confirm(v.question)
		if err != nil && !errors.Is(err, picker.ErrAborted) {
			fmt.Fprintf(stderr, "physa skill %s: %v\n", v.name, err)
			return 1
		}
		if err != nil || !yes {
			fmt.Fprintln(stderr, "physa skill "+v.name+": "+v.nothing)
			return finish(v, all, todo, 2, asJSON, stdout, stderr)
		}
	}
	for _, a := range todo {
		if err := v.do(a.path); err != nil {
			fmt.Fprintf(stderr, "physa skill %s: %v\n", v.name, err)
			return 1
		}
		a.action = v.doneWord
		a.state, _ = skillstub.Read(a.path)
	}
	return finish(v, all, nil, code, asJSON, stdout, stderr)
}

func afterState(v verb) string {
	if v.name == "setup" {
		return skillstub.Current.String()
	}
	return skillstub.Missing.String()
}

// finish reports what each agent came to. Agents left undone for want of a
// yes (undone) are reported as refused.
func finish(v verb, all []entry, undone []*entry, code int, asJSON bool, stdout, stderr io.Writer) int {
	for _, a := range undone {
		a.action = "refused"
	}
	if asJSON {
		if c := emit(v.name, all, stdout, stderr); c != 0 {
			return c
		}
		return code
	}
	for _, a := range all {
		switch a.action {
		case v.doneWord:
			fmt.Fprintf(stdout, "%s: %s %s\n", a.t.label, v.doneWord, a.path)
		case "none":
			fmt.Fprintf(stdout, "%s: %s (%s)\n", a.t.label, v.noopText, a.path)
		}
	}
	return code
}
