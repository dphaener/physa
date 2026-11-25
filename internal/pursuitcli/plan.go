package pursuitcli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/evidencecli"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/plan"
	"github.com/dphaener/physa/internal/tokens"
)

// planGroup is the `physa pursuit plan` group: set and show a pursuit's plan.
func (p pursuits) planGroup() *cobra.Command {
	const set = "physa pursuit plan set [<pursuit>] [--reason <text>] [--from <file>|-] [--json]"
	const show = "physa pursuit plan show [<pursuit>] [--history] [--json]"
	var setReason, setFrom string
	var setJSON, showHistory, showJSON bool
	return command.New("plan",
		"Set and show a pursuit's plan: the changes it will land, each in one capability, with what to do and how to tell it is done.",
		[]string{set, show}, nil,
		command.Declare(command.New("set",
			"Set a pursuit's plan as its next revision, reading the plan's JSON from --from, or from stdin off a terminal. "+
				"A revision after the first needs --reason: what the plan missed. Agents may set a plan.",
			[]string{set}, func(args []string, stdout, stderr io.Writer) int {
				return p.planSet(args, setReason, setFrom, setJSON, stdout, stderr)
			}), func(fs *pflag.FlagSet) {
			ideacli.String(fs, "reason", &setReason)
			ideacli.String(fs, "from", &setFrom)
			ideacli.JSON(fs, &setJSON)
		}),
		command.Declare(command.New("show",
			"Show a pursuit's current plan and its changes; --history lists every revision, oldest first, with its reason.",
			[]string{show}, func(args []string, stdout, stderr io.Writer) int {
				return p.planShow(args, showHistory, showJSON, stdout, stderr)
			}), func(fs *pflag.FlagSet) {
			command.Switch(fs, "history", &showHistory, "")
			ideacli.JSON(fs, &showJSON)
		}))
}

// planInput reads the plan's JSON: from --from (a file, or - for stdin),
// else from stdin when no person is at a terminal.
func (p pursuits) planInput(from string) ([]byte, error) {
	var data []byte
	var err error
	switch {
	case from == "" && p.Interactive():
		return nil, &ideacli.UsageError{Msg: "on a terminal the plan comes from a file; pass --from <file>"}
	case from == "" || from == "-":
		data, err = io.ReadAll(p.Stdin)
	default:
		data, err = os.ReadFile(from)
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, &ideacli.UsageError{Msg: "the plan is empty; give its JSON with --from <file>, or on stdin"}
	}
	return data, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (p pursuits) planSet(args []string, reason, from string, asJSON bool, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		return command.BadUsage
	}
	if from == "" && p.Interactive() {
		return ideacli.Failed(stderr, "pursuit plan set", &ideacli.UsageError{Msg: "on a terminal the plan comes from a file; pass --from <file>"})
	}
	repo, err := p.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "pursuit plan set", err)
	}
	who := p.Who(repo, stderr)
	if who == "" {
		return 2
	}
	slug, err := p.choose(repo, firstArg(args), func(o client.PursuitOut) bool { return o.State == "active" })
	if err != nil {
		return ideacli.Failed(stderr, "pursuit plan set", err)
	}
	data, err := p.planInput(from)
	if err != nil {
		return ideacli.Failed(stderr, "pursuit plan set", err)
	}
	var out client.PlanOut
	in := client.PlanSetIn{Repo: repo, Pursuit: slug, Reason: reason, Person: who, Plan: json.RawMessage(data)}
	if err := p.Call("plan.set", in, &out); err != nil {
		return ideacli.Failed(stderr, "pursuit plan set", err)
	}
	if asJSON {
		return ideacli.Written(stderr, "pursuit plan set", output.JSON(stdout, out))
	}
	line := fmt.Sprintf("Plan for %s set: revision %d, %s", out.Pursuit, out.N, plural(len(out.Changes), "change", "changes"))
	if out.Replans > 0 {
		line += ", " + plural(out.Replans, "replan", "replans")
	}
	p.View(stdout).Title(line)
	return 0
}

func (p pursuits) planShow(args []string, history, asJSON bool, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		return command.BadUsage
	}
	repo, err := p.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "pursuit plan show", err)
	}
	slug, err := p.choose(repo, firstArg(args), func(client.PursuitOut) bool { return true })
	if err != nil {
		return ideacli.Failed(stderr, "pursuit plan show", err)
	}
	var out client.PlanOut
	if err := p.Call("plan.show", client.PlanShowIn{Repo: repo, Pursuit: slug, History: history}, &out); err != nil {
		return ideacli.Failed(stderr, "pursuit plan show", err)
	}
	if asJSON {
		return ideacli.Written(stderr, "pursuit plan show", output.JSON(stdout, out))
	}
	v := p.View(stdout)
	v.Title(fmt.Sprintf("Plan for %s", out.Pursuit))
	pairs := [][2]string{{"revision", fmt.Sprint(out.N)}}
	if out.Reason != "" {
		pairs = append(pairs, [2]string{"reason", out.Reason})
	}
	pairs = append(pairs, [2]string{"set by", out.Person}, [2]string{"set", ideacli.When(out.At)}, [2]string{"replans", fmt.Sprint(out.Replans)})
	v.Fields(pairs...)
	for _, c := range out.Changes {
		v.Section(c.Name, changeText(c))
	}
	if history {
		var lines []string
		for _, r := range out.History {
			line := fmt.Sprintf("%d  %s by %s: %s", r.N, ideacli.When(r.At), r.Person, strings.Join(r.Changes, ", "))
			if r.Reason != "" {
				line += "\n   reason: " + r.Reason
			}
			lines = append(lines, line)
		}
		v.Section("History", strings.Join(lines, "\n"))
	}
	return 0
}

func changeText(c plan.Change) string {
	list := func(items []string) string {
		if len(items) == 0 {
			return "none"
		}
		return strings.Join(items, "; ")
	}
	return strings.Join([]string{
		"capability  " + c.Capability,
		"what        " + c.What,
		"acceptance  " + list(c.Acceptance),
		"must not    " + list(c.MustNot),
		"after       " + list(c.After),
	}, "\n")
}

// showProgress prints a pursuit's progress as facts, never a percentage, then
// what needs its person. A pursuit with no plan says only that nothing is planned.
func showProgress(v *ideacli.View, o client.PursuitOut) {
	p := o.Progress
	if p != nil {
		if len(p.Planned) == 0 {
			v.Section("Progress", "planned       none yet")
		} else {
			names := func(ns []string) string {
				if len(ns) == 0 {
					return "none"
				}
				return strings.Join(ns, ", ")
			}
			landed := make([]string, len(p.Landed))
			for i, l := range p.Landed {
				landed[i] = l.Change
			}
			asks := "none yet"
			if p.AsksOpen > 0 {
				asks = fmt.Sprint(p.AsksOpen)
			}
			v.Section("Progress", strings.Join([]string{
				"planned       " + plural(len(p.Planned), "change", "changes"),
				fmt.Sprintf("landed        %d of %d: %s", len(p.Landed), len(p.Planned), names(landed)),
				"in work       " + names(p.InWork),
				"asks open     " + asks,
				"spent         " + spentOf(p.Spent, p.Appetite),
				fmt.Sprintf("replans       %d", p.Replans),
			}, "\n"))
			if len(p.PerChange) > 0 {
				rows := make([]string, len(p.PerChange))
				for i, c := range p.PerChange {
					name := c.Change
					if name == "" {
						name = "(pursuit)"
					}
					rows[i] = fmt.Sprintf("%s  %s", name, tokens.Format(c.Tokens))
				}
				v.Section("Spend per change", strings.Join(rows, "\n"))
			}
		}
	}
	if len(o.Evidence) > 0 {
		lines := make([]string, len(o.Evidence))
		for i, f := range o.Evidence {
			lines[i] = evidencecli.Line(f)
		}
		v.Section("Evidence", strings.Join(lines, "\n"))
	}
	if len(o.NeedsYou) > 0 {
		texts := make([]string, len(o.NeedsYou))
		for i, n := range o.NeedsYou {
			texts[i] = n.Text
		}
		v.Section("Needs you", strings.Join(texts, "\n"))
	}
}

// spentOf writes spend against the appetite, "N of M", or "N, no appetite"
// when the pursuit has none.
func spentOf(spent, appetite int64) string {
	if appetite <= 0 {
		return tokens.Format(spent) + ", no appetite"
	}
	return tokens.Format(spent) + " of " + tokens.Format(appetite)
}
