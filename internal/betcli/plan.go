package betcli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/pitch"
	"github.com/dphaener/physa/internal/plan"
	"github.com/dphaener/physa/internal/workapi"
)

// planGroup is the `physa bet plan` group: set and show a bet's plan.
func (b bets) planGroup() *cli.Command {
	return &cli.Command{
		Name:    "plan",
		Summary: "Set and show a bet's plan: the changes it will land, each in one capability, with what to do and how to tell it is done.",
		Usage: []string{
			"physa bet plan set [<bet>] [--reason <text>] [--from <file>|-] [--json]",
			"physa bet plan show [<bet>] [--history] [--json]",
		},
		Subs: []*cli.Command{
			{
				Name:  "set",
				Usage: []string{"physa bet plan set [<bet>] [--reason <text>] [--from <file>|-] [--json]"},
				Summary: "Set a bet's plan as its next revision, reading the plan's JSON from --from, or from stdin off a terminal. " +
					"A revision after the first needs --reason: what shaping missed. Agents may set a plan.",
				Run: b.planSet,
			},
			{
				Name:    "show",
				Usage:   []string{"physa bet plan show [<bet>] [--history] [--json]"},
				Summary: "Show a bet's current plan and its changes; --history lists every revision, oldest first, with its reason.",
				Run:     b.planShow,
			},
		},
	}
}

// planInput reads the plan's JSON: from --from (a file, or - for stdin),
// else from stdin when no person is at a terminal.
func (b bets) planInput(from string) ([]byte, error) {
	var data []byte
	var err error
	switch {
	case from == "" && b.Interactive():
		return nil, &ideacli.UsageError{Msg: "on a terminal the plan comes from a file; pass --from <file>"}
	case from == "" || from == "-":
		data, err = io.ReadAll(b.Stdin)
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

func (b bets) planSet(args []string, stdout, stderr io.Writer) int {
	f, ok := ideacli.Parse(args, "reason", "from", "json")
	if !ok || len(f.Args) > 1 {
		return cli.BadUsage
	}
	if f.From == "" && b.Interactive() {
		return ideacli.Failed(stderr, "bet plan set", &ideacli.UsageError{Msg: "on a terminal the plan comes from a file; pass --from <file>"})
	}
	repo, err := b.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "bet plan set", err)
	}
	who := b.Who(repo, stderr)
	if who == "" {
		return 2
	}
	slug, err := b.chooseBet(repo, firstArg(f.Args), func(o workapi.BetOut) bool { return o.State == "active" })
	if err != nil {
		return ideacli.Failed(stderr, "bet plan set", err)
	}
	data, err := b.planInput(f.From)
	if err != nil {
		return ideacli.Failed(stderr, "bet plan set", err)
	}
	var out workapi.PlanOut
	in := workapi.PlanSetIn{Repo: repo, Bet: slug, Reason: f.Reason, Person: who, Plan: json.RawMessage(data)}
	if err := b.Call("plan.set", in, &out); err != nil {
		return ideacli.Failed(stderr, "bet plan set", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "bet plan set", output.JSON(stdout, out))
	}
	line := fmt.Sprintf("Plan for %s set: revision %d, %s", out.Bet, out.N, plural(len(out.Changes), "change", "changes"))
	if out.Replans > 0 {
		line += ", " + plural(out.Replans, "replan", "replans")
	}
	b.View(stdout).Title(line)
	return 0
}

func (b bets) planShow(args []string, stdout, stderr io.Writer) int {
	f, ok := ideacli.Parse(args, "history", "json")
	if !ok || len(f.Args) > 1 {
		return cli.BadUsage
	}
	repo, err := b.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "bet plan show", err)
	}
	slug, err := b.chooseBet(repo, firstArg(f.Args), func(workapi.BetOut) bool { return true })
	if err != nil {
		return ideacli.Failed(stderr, "bet plan show", err)
	}
	var out workapi.PlanOut
	if err := b.Call("plan.show", workapi.PlanShowIn{Repo: repo, Bet: slug, History: f.History}, &out); err != nil {
		return ideacli.Failed(stderr, "bet plan show", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "bet plan show", output.JSON(stdout, out))
	}
	v := b.View(stdout)
	v.Title(fmt.Sprintf("Plan for %s", out.Bet))
	pairs := [][2]string{{"revision", fmt.Sprint(out.N)}}
	if out.Reason != "" {
		pairs = append(pairs, [2]string{"reason", out.Reason})
	}
	pairs = append(pairs, [2]string{"set by", out.Person}, [2]string{"set", ideacli.When(out.At)}, [2]string{"replans", fmt.Sprint(out.Replans)})
	v.Fields(pairs...)
	for _, c := range out.Changes {
		v.Section(c.Name, changeText(c))
	}
	if f.History {
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

// showProgress prints a bet's progress as facts, never a percentage, then
// what needs its person. A bet with no plan says only that nothing is planned.
func showProgress(v *ideacli.View, o workapi.BetOut) {
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
				fmt.Sprintf("spent         %s of %s", pitch.Tokens(p.Spent), pitch.Tokens(p.Appetite)),
				fmt.Sprintf("replans       %d", p.Replans),
			}, "\n"))
			if len(p.PerChange) > 0 {
				rows := make([]string, len(p.PerChange))
				for i, c := range p.PerChange {
					name := c.Change
					if name == "" {
						name = "(bet)"
					}
					rows[i] = fmt.Sprintf("%s  %s", name, pitch.Tokens(c.Tokens))
				}
				v.Section("Spend per change", strings.Join(rows, "\n"))
			}
		}
	}
	if len(o.NeedsYou) > 0 {
		texts := make([]string, len(o.NeedsYou))
		for i, n := range o.NeedsYou {
			texts[i] = n.Text
		}
		v.Section("Needs you", strings.Join(texts, "\n"))
	}
}
