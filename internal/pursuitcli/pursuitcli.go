// Package pursuitcli is the pursuit commands: a person pursues an idea
// against a signal, with an appetite or none, and lists, shows and stops
// pursuits; inside an agent's session starting or stopping one is refused.
package pursuitcli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/tokens"
	"github.com/dphaener/physa/internal/workapi"
)

// Commands returns `physa pursue` and the pursuit group, naming people
// through the forge newForge builds for a repo.
func Commands(newForge func(dir string) forge.Forge) []*cobra.Command {
	return commandsFor(ideacli.New(newForge))
}

type pursuits struct{ *ideacli.Env }

// noSignal is how a pursuit started before signals were recorded shows its
// signal.
const noSignal = "none (started before signals)"

func commandsFor(env *ideacli.Env) []*cobra.Command {
	p := pursuits{env}
	leaf := func(name, usage, summary string, run func([]string, io.Writer, io.Writer) int) *cobra.Command {
		return command.New(name, summary, []string{usage}, run)
	}
	pursue := command.New("pursue",
		"Pursue an idea: put your name on building it, against a signal of four parts (claim, direction, window, resolver). "+
			"On a terminal the signal is written in $VISUAL or $EDITOR; --signal-from reads it from a file, or stdin for -, and off a terminal it is required. "+
			"--appetite sets a budget in tokens; with none there is no circuit breaker, and spend is still counted. "+
			"Only a person pursues: inside an agent's session it is refused. A slug left out is chosen from a list on a terminal.",
		[]string{usagePursue}, p.start)
	group := command.New("pursuit",
		"List, show and stop pursuits, set and show a pursuit's plan, and say which change a session works on. Only a person stops a pursuit: "+
			"inside an agent's session it is refused. A slug left out is chosen from a list on a terminal.",
		[]string{
			"physa pursuit [list] [--json]",
			"physa pursuit show [<slug>] [--json]",
			"physa pursuit stop [<slug>] [--reason <text>] [--yes] [--json]",
			"physa pursuit plan set [<pursuit>] [--reason <text>] [--from <file>|-] [--json]",
			"physa pursuit plan show [<pursuit>] [--history] [--json]",
			"physa pursuit work <pursuit>/<change> [--json]",
		},
		p.list,
		leaf("list", "physa pursuit list [--json]", "List the pursuits, newest first, with their appetite and what is spent.", p.list),
		leaf("show", "physa pursuit show [<slug>] [--json]", "Show a pursuit: its facts, its signal, its progress as facts and what needs its person.", p.show),
		leaf("stop", "physa pursuit stop [<slug>] [--reason <text>] [--yes] [--json]",
			"Stop an active pursuit, asking first on a terminal unless --yes.", p.stop),
		p.planGroup(),
		workCommand(p))
	return []*cobra.Command{pursue, group}
}

// refuse says a pursuit is only a person's to start or stop when Physa runs
// inside an agent's session, and reports whether it did.
func (p pursuits) refuse(method, name string, stderr io.Writer) bool {
	agent, ok := p.Agent()
	if ok {
		fmt.Fprintf(stderr, "physa %s: %s\n", name, workapi.Refusal(method, agent))
	}
	return ok
}

func (p pursuits) all(repo string) ([]workapi.PursuitOut, error) {
	var out []workapi.PursuitOut
	err := p.Call("pursuit.list", workapi.RepoIn{Repo: repo}, &out)
	return out, err
}

func (p pursuits) list(args []string, stdout, stderr io.Writer) int {
	f, ok := ideacli.Parse(args, "json")
	if !ok || len(f.Args) > 0 {
		return command.BadUsage
	}
	repo, err := p.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "pursuit list", err)
	}
	all, err := p.all(repo)
	if err != nil {
		return ideacli.Failed(stderr, "pursuit list", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "pursuit list", output.JSON(stdout, all))
	}
	v := p.View(stdout)
	if len(all) == 0 {
		v.Line("No pursuits yet. Start one with physa pursue <idea slug>.")
		return 0
	}
	rows := make([][]string, len(all))
	for i, o := range all {
		rows[i] = []string{o.Slug, o.IdeaText, ideacli.Appetite(o.Appetite), tokens.Format(o.Spent), o.State, o.Person}
	}
	v.Table([]string{"Slug", "Idea", "Appetite", "Spent", "State", "Person"}, rows)
	return 0
}

func firstArg(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return ""
}

// choose returns the pursuit slug given, or the one chosen from the
// pursuits keep accepts.
func (p pursuits) choose(repo string, given string, keep func(workapi.PursuitOut) bool) (string, error) {
	if given != "" {
		return given, nil
	}
	all, err := p.all(repo)
	if err != nil {
		return "", err
	}
	var cs []ideacli.Choice
	for _, o := range all {
		if keep(o) {
			cs = append(cs, ideacli.Choice{Slug: o.Slug, Text: o.IdeaText, Detail: o.State + ", appetite " + ideacli.Appetite(o.Appetite)})
		}
	}
	return p.ChooseSlug("pursuit", "", cs)
}

func (p pursuits) show(args []string, stdout, stderr io.Writer) int {
	f, ok := ideacli.Parse(args, "json")
	if !ok || len(f.Args) > 1 {
		return command.BadUsage
	}
	repo, err := p.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "pursuit show", err)
	}
	slug, err := p.choose(repo, firstArg(f.Args), func(workapi.PursuitOut) bool { return true })
	if err != nil {
		return ideacli.Failed(stderr, "pursuit show", err)
	}
	var o workapi.PursuitOut
	if err := p.Call("pursuit.show", workapi.SlugIn{Repo: repo, Slug: slug}, &o); err != nil {
		return ideacli.Failed(stderr, "pursuit show", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "pursuit show", output.JSON(stdout, o))
	}
	showPursuit(p.View(stdout), o)
	return 0
}

// showPursuit prints a pursuit: its facts, its signal, or that it has none,
// then its progress.
func showPursuit(v *ideacli.View, o workapi.PursuitOut) {
	v.Title(o.IdeaText)
	pairs := [][2]string{
		{"slug", o.Slug}, {"idea", o.Idea}, {"person", o.Person}, {"state", o.State},
		{"appetite", ideacli.Appetite(o.Appetite)}, {"spent", tokens.Format(o.Spent)}, {"started", ideacli.When(o.CreatedAt)},
	}
	if o.EndedAt != "" {
		pairs = append(pairs, [2]string{"ended", ideacli.When(o.EndedAt)})
	}
	if o.StopReason != "" {
		pairs = append(pairs, [2]string{"stop reason", o.StopReason})
	}
	if o.Signal == nil {
		pairs = append(pairs, [2]string{"signal", noSignal})
	}
	v.Fields(pairs...)
	if o.Signal != nil {
		v.Section("Signal", signalText(*o.Signal))
	}
	showProgress(v, o)
}

// signalText is a line or more for each part of a signal.
func signalText(s workapi.SignalOut) string {
	var lines []string
	for _, part := range [][2]string{{"claim", s.Claim}, {"for", s.For}, {"against", s.Against}, {"window", s.Window}, {"resolver", s.Resolver}} {
		text := strings.ReplaceAll(strings.TrimSpace(part[1]), "\n", "\n"+strings.Repeat(" ", 10))
		lines = append(lines, fmt.Sprintf("%-8s  %s", part[0], text))
	}
	return strings.Join(lines, "\n")
}

func (p pursuits) stop(args []string, stdout, stderr io.Writer) int {
	if p.refuse("pursuit.stop", "pursuit stop", stderr) {
		return 2
	}
	f, ok := ideacli.Parse(args, "reason", "yes", "json")
	if !ok || len(f.Args) > 1 {
		return command.BadUsage
	}
	repo, err := p.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "pursuit stop", err)
	}
	who := p.Who(repo, stderr)
	if who == "" {
		return 2
	}
	slug, err := p.choose(repo, firstArg(f.Args), func(o workapi.PursuitOut) bool { return o.State == "active" })
	if err != nil {
		return ideacli.Failed(stderr, "pursuit stop", err)
	}
	if p.Interactive() && !f.Yes {
		yes, err := p.Confirm("Stop pursuit " + slug + "?")
		if err != nil && !errors.Is(err, picker.ErrAborted) {
			return ideacli.Failed(stderr, "pursuit stop", err)
		}
		if !yes {
			fmt.Fprintln(stderr, "physa pursuit stop: not stopped")
			return 2
		}
	}
	var o workapi.PursuitOut
	if err := p.Call("pursuit.stop", workapi.StopIn{Repo: repo, Slug: slug, Reason: f.Reason, Person: who}, &o); err != nil {
		return ideacli.Failed(stderr, "pursuit stop", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "pursuit stop", output.JSON(stdout, o))
	}
	p.View(stdout).Title("Stopped pursuit " + o.Slug)
	return 0
}
