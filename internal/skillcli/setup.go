package skillcli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/adapter/claude"
	"github.com/dphaener/physa/internal/agentenv"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/skillagent"
	"github.com/dphaener/physa/internal/skillhook"
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

	// What the session command reaches outside itself for.
	current         CurrentFunc
	getwd           func() (string, error)
	getenv          func(string) string
	lookPath        func(string) (string, error)
	stdin           io.Reader
	stdinIsTerminal func() bool
	deadline        time.Duration
}

func defaultEnv(current CurrentFunc) env {
	return env{
		current:         current,
		getwd:           os.Getwd,
		getenv:          os.Getenv,
		lookPath:        exec.LookPath,
		stdin:           os.Stdin,
		stdinIsTerminal: func() bool { return isTerminal(os.Stdin) },
		deadline:        defaultDeadline,
		home:            os.UserHomeDir,
		interactive: func() bool {
			_, agent := agentenv.Detect(os.Getenv)
			return output.Interactive(os.Stdin, os.Stdout, agent)
		},
		// The question goes to stderr, so stdout stays for the result.
		confirm: func(q string) (bool, error) { return picker.Confirm(os.Stdin, os.Stderr, q) },
	}
}

// part is one thing setup writes for an agent: the stub or the hook. States
// are the words both packages use: missing, current, stale and other.
type part struct {
	name   string   // "skill" or "hook"
	path   string   // empty for a hook the agent cannot take
	all    []string // labels of the agents the part serves
	on     []string // those setup acts for: Claude Code always, the others when found
	trust  bool     // the agent runs the hook only once the person trusts it
	before string
	state  string
	action string
	other  string // how people read the other state
	read   func(path string) (string, error)
	write  func(path string) error
	remove func(path string) error
}

func stubPart(path string) *part {
	return &part{name: "skill", path: path, other: "not written by Physa",
		read:  func(p string) (string, error) { s, err := skillstub.Read(p); return s.String(), err },
		write: skillstub.Write, remove: skillstub.Remove}
}

func hookPart(path string) *part {
	return &part{name: "hook", path: path, other: "not editable by Physa",
		read:  func(p string) (string, error) { s, err := skillhook.Read(p); return s.String(), err },
		write: skillhook.Write, remove: skillhook.Remove}
}

// unsupported stands in for a hook Physa cannot install for an agent: it has
// no path, and setup and remove never act on it.
func unsupported() *part { return &part{name: "hook", state: "unsupported"} }

// entry is one agent's parts, the stub then the hook: where they are, what
// they were, and what the command did. Agents sharing a stub share its part.
type entry struct {
	a     skillagent.Agent
	parts []*part
}

type hookJSON struct {
	Path   string `json:"path,omitempty"`
	State  string `json:"state"`
	Action string `json:"action,omitempty"`
}

type agentJSON struct {
	Agent  string    `json:"agent"`
	Found  bool      `json:"found"`
	Path   string    `json:"path"`
	State  string    `json:"state"`
	Action string    `json:"action,omitempty"`
	Hook   *hookJSON `json:"hook,omitempty"`
}

func newCommand(e env) *cobra.Command {
	var groupJSON, statusJSON, setupJSON, setupYes, removeJSON, removeYes bool
	jsonFlag := func(fs *pflag.FlagSet, on *bool) { command.Switch(fs, "json", on, "") }
	status := func(asJSON *bool) command.Run {
		return func(args []string, stdout, stderr io.Writer) int { return e.status(args, *asJSON, stdout, stderr) }
	}
	return command.Declare(command.New("skill",
		"Set up, check, print and remove Physa's skill, the guide a person's agent loads to use Physa's commands and terms. "+
			"setup writes a short stub into Claude Code's skills directory (~/.claude/skills/physa) that tells the agent to run `physa skill show`, "+
			"and adds a session hook to ~/.claude/settings.json that runs Physa when a session starts; "+
			"for Codex, OpenCode and Pi, each when found by its config directory or its binary on PATH, it writes the same stub once to ~/.agents/skills/physa, "+
			"and for Codex adds the hook to its hooks.json (Codex runs it once the person trusts it in /hooks; OpenCode and Pi have no hook Physa can install); "+
			"status lists the four agents, found or not, and says whether each stub and hook is current, stale, missing or unsupported; remove deletes the stubs and takes out only Physa's hook entries. "+
			"setup and remove ask on a terminal and need --yes off one, and never touch a SKILL.md Physa did not write or a hooks file Physa cannot edit. A bare skill is status.",
		[]string{groupUsage, setupUsage, statusUsage, "physa skill show", removeUsage}, status(&groupJSON),
		command.Declare(command.New("setup",
			"Write Physa's stub to ~/.claude/skills/physa/SKILL.md and its session hook to ~/.claude/settings.json, and, for the agents found among Codex, OpenCode and Pi, the stub to ~/.agents/skills/physa/SKILL.md and, for Codex, the hook to its hooks.json, replacing stale ones; a SKILL.md Physa did not write, or a hooks file Physa cannot edit, is left as it is.",
			[]string{setupUsage}, func(args []string, stdout, stderr io.Writer) int {
				return e.change(verbs[0], args, setupYes, setupJSON, stdout, stderr)
			}), func(fs *pflag.FlagSet) {
			command.Switch(fs, "yes", &setupYes, "")
			jsonFlag(fs, &setupJSON)
		}),
		command.Declare(command.New("status", "Say whether each of Claude Code, Codex, OpenCode and Pi is found, and whether Physa's stub and session hook are current, stale, missing or, for a hook Physa cannot install, unsupported.",
			[]string{statusUsage}, status(&statusJSON)), func(fs *pflag.FlagSet) { jsonFlag(fs, &statusJSON) }),
		command.New("show", "Print the guide, as it is, on any output.", []string{showUsage}, show),
		hidden(e.sessionCommand()),
		command.Declare(command.New("remove",
			"Delete Physa's stubs (~/.claude/skills/physa and ~/.agents/skills/physa) and take Physa's session hook out of ~/.claude/settings.json and Codex's hooks.json, leaving every other entry; a SKILL.md Physa did not write, or a hooks file Physa cannot edit, is left as it is.",
			[]string{removeUsage}, func(args []string, stdout, stderr io.Writer) int {
				return e.change(verbs[1], args, removeYes, removeJSON, stdout, stderr)
			}), func(fs *pflag.FlagSet) {
			command.Switch(fs, "yes", &removeYes, "")
			jsonFlag(fs, &removeJSON)
		}),
	), func(fs *pflag.FlagSet) { jsonFlag(fs, &groupJSON) })
}

// load reads every agent's stub and hook under the home directory. Agents
// sharing a stub share one part, read once.
func (e env) load(verb string, stderr io.Writer) ([]entry, bool) {
	home, err := e.home()
	if err == nil && home == "" {
		err = errors.New("no home directory")
	}
	var all []entry
	if err == nil {
		seen := map[string]*part{}
		get := func(a skillagent.Agent, path string, build func(string) *part) *part {
			p := seen[path]
			if p == nil {
				p = build(path)
				seen[path] = p
				if p.before, err = p.read(path); err == nil {
					p.state = p.before
				}
			}
			p.all = append(p.all, a.Label)
			if a.Name == claude.Name || a.Found {
				p.on = append(p.on, a.Label)
			}
			return p
		}
		for _, a := range skillagent.List(skillagent.Env{Home: home, Getenv: e.getenv, LookPath: e.lookPath}) {
			en := entry{a: a, parts: []*part{get(a, a.Stub, stubPart), unsupported()}}
			if a.Supported() && err == nil {
				en.parts[1] = get(a, a.Hook, hookPart)
				en.parts[1].trust = a.Name == "codex"
			}
			if err != nil {
				break
			}
			all = append(all, en)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "physa skill %s: %v\n", verb, err)
		return nil, false
	}
	return all, true
}

// unique lists each part once, in the order the agents list them.
func unique(all []entry) []*part {
	var out []*part
	seen := map[*part]bool{}
	for _, a := range all {
		for _, p := range a.parts {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// join words labels as "A", "A and B" or "A, B and C".
func join(labels []string) string {
	if n := len(labels); n > 1 {
		return strings.Join(labels[:n-1], ", ") + " and " + labels[n-1]
	}
	return strings.Join(labels, "")
}

func (p *part) shown() string {
	if p.state == "other" {
		return p.other
	}
	return p.state
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
	var rows [][]string
	for _, a := range all {
		found := "not found"
		if a.a.Found {
			found = "found"
		}
		for _, p := range a.parts {
			rows = append(rows, []string{a.a.Label, found, p.name, p.shown(), p.path})
		}
	}
	output.For(stdout).Table([]string{"agent", "found", "part", "state", "path"}, rows)
	return 0
}

func emit(verb string, all []entry, stdout, stderr io.Writer) int {
	out := struct {
		Agents []agentJSON `json:"agents"`
	}{Agents: []agentJSON{}}
	for _, a := range all {
		s, h := a.parts[0], a.parts[1]
		out.Agents = append(out.Agents, agentJSON{a.a.Name, a.a.Found, s.path, s.state, s.action, &hookJSON{h.path, h.state, h.action}})
	}
	if err := output.JSON(stdout, out); err != nil {
		fmt.Fprintf(stderr, "physa skill %s: %v\n", verb, err)
		return 1
	}
	return 0
}

// verb is what setup or remove does: the state it acts on and how it words
// itself.
type verb struct {
	name     string
	doneWord string // the action it reports
	question string
	noop     string // the state that needs nothing
	noopText string
	nothing  string // said when the person declines
}

var verbs = [2]verb{
	{"setup", "written", "Write Physa's skill?", "current", "already current", "nothing written"},
	{"remove", "removed", "Remove Physa's skill?", "missing", "nothing to remove", "nothing removed"},
}

// do writes or removes the part, as the verb says.
func (v verb) do(p *part) error {
	if v.name == "setup" {
		return p.write(p.path)
	}
	return p.remove(p.path)
}

// label names a part and the agents it serves; setup names only those it
// acts for.
func (p *part) label(v verb) string {
	names := p.all
	if v.name == "setup" {
		names = p.on
	}
	return join(names) + " " + p.name
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
	var todo []*part
	refused := false
	for _, p := range unique(all) {
		switch {
		case p.path == "":
		case v.name == "setup" && len(p.on) == 0:
			p.action = "skipped"
		case p.before == "other":
			p.action = "refused"
			refused = true
			fmt.Fprintf(stderr, "physa skill %s: %s is %s; left as it is\n", v.name, p.path, p.other)
		case p.before == v.noop:
			p.action = "none"
		default:
			todo = append(todo, p)
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
		for _, p := range todo {
			fmt.Fprintf(stdout, "%s: %s, %s -> %s\n", p.label(v), p.path, p.before, afterState(v))
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
	for _, p := range todo {
		if err := v.do(p); err != nil {
			fmt.Fprintf(stderr, "physa skill %s: %v\n", v.name, err)
			return 1
		}
		p.action = v.doneWord
		p.state, _ = p.read(p.path)
	}
	return finish(v, all, nil, code, asJSON, stdout, stderr)
}

func afterState(v verb) string {
	if v.name == "setup" {
		return "current"
	}
	return "missing"
}

// finish reports what each part came to. Parts left undone for want of a
// yes (undone) are reported as refused.
func finish(v verb, all []entry, undone []*part, code int, asJSON bool, stdout, stderr io.Writer) int {
	for _, p := range undone {
		p.action = "refused"
	}
	if asJSON {
		if c := emit(v.name, all, stdout, stderr); c != 0 {
			return c
		}
		return code
	}
	trusted := false
	for _, p := range unique(all) {
		switch p.action {
		case v.doneWord:
			fmt.Fprintf(stdout, "%s: %s %s\n", p.label(v), v.doneWord, p.path)
		case "none":
			fmt.Fprintf(stdout, "%s: %s (%s)\n", p.label(v), v.noopText, p.path)
		case "skipped":
			fmt.Fprintf(stdout, "%s: not found; skipped\n", join(p.all))
		}
		if v.name == "setup" && p.trust && p.state == "current" && (p.action == v.doneWord || p.action == "none") {
			trusted = true
		}
	}
	if trusted {
		fmt.Fprintln(stdout, "Codex runs a new or changed hook only once you trust it in /hooks.")
	}
	return code
}

// hidden keeps a command a hook runs out of help.
func hidden(c *cobra.Command) *cobra.Command {
	c.Hidden = true
	return c
}
