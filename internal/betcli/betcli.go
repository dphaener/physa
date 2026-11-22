// Package betcli is the bet commands: a person bets on an idea within an
// appetite, lists, shows and stops bets; inside an agent's session a bet is
// refused.
package betcli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/picker"
	"github.com/dphaener/physa/internal/pitch"
	"github.com/dphaener/physa/internal/workapi"
)

// Command returns the bet group, naming people through the forge newForge
// builds for a repo.
func Command(newForge func(dir string) forge.Forge) *cli.Command {
	return commandFor(ideacli.New(newForge))
}

type bets struct{ *ideacli.Env }

func commandFor(env *ideacli.Env) *cli.Command {
	b := bets{env}
	leaf := func(name, usage, summary string, run func([]string, io.Writer, io.Writer) int) *cli.Command {
		return &cli.Command{Name: name, Summary: summary, Usage: []string{usage}, Run: run}
	}
	return &cli.Command{
		Name: "bet",
		Summary: "Bet on an idea, and list, show and stop bets. Only a person bets or stops a bet: inside an agent's " +
			"session both are refused. The appetite is the one given, else the pitch's. A slug left out is chosen from a list on a terminal.",
		Usage: []string{
			"physa bet [<idea slug>] [--appetite <tokens>] [--json]",
			"physa bet list [--json]",
			"physa bet show [<slug>] [--json]",
			"physa bet stop [<slug>] [--reason <text>] [--yes] [--json]",
		},
		Run: b.start,
		Subs: []*cli.Command{
			leaf("list", "physa bet list [--json]", "List the bets, newest first, with their appetite and what is spent.", b.list),
			leaf("show", "physa bet show [<slug>] [--json]", "Show a bet: its facts, a summary of its pitch and why it stopped.", b.show),
			leaf("stop", "physa bet stop [<slug>] [--reason <text>] [--yes] [--json]",
				"Stop an active bet, asking first on a terminal unless --yes.", b.stop),
		},
	}
}

// refuse says a bet is only a person's to make or stop when Physa runs
// inside an agent's session, and reports whether it did.
func (b bets) refuse(method, name string, stderr io.Writer) bool {
	agent, ok := b.Agent()
	if ok {
		fmt.Fprintf(stderr, "physa %s: %s\n", name, workapi.Refusal(method, agent))
	}
	return ok
}

func (b bets) allBets(repo string) ([]workapi.BetOut, error) {
	var out []workapi.BetOut
	err := b.Call("bet.list", workapi.RepoIn{Repo: repo}, &out)
	return out, err
}

func (b bets) start(args []string, stdout, stderr io.Writer) int {
	if b.refuse("bet.start", "bet", stderr) {
		return 2
	}
	f, ok := ideacli.Parse(args, "appetite", "json")
	if !ok || len(f.Args) > 1 {
		return cli.BadUsage
	}
	if f.Appetite != "" {
		if _, err := pitch.ParseTokens(f.Appetite); err != nil {
			return ideacli.Failed(stderr, "bet", err)
		}
	}
	repo, err := b.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "bet", err)
	}
	who := b.Who(repo, stderr)
	if who == "" {
		return 2
	}
	ideas, err := b.Ideas(repo)
	if err != nil {
		return ideacli.Failed(stderr, "bet", err)
	}
	var open []workapi.IdeaOut
	for _, o := range ideas {
		if o.BetState != "active" {
			open = append(open, o)
		}
	}
	slug, err := b.ChooseSlug("idea", firstArg(f.Args), ideacli.IdeaChoices(open))
	if err != nil {
		return ideacli.Failed(stderr, "bet", err)
	}
	if f.Appetite == "" {
		if f.Appetite, err = b.appetite(repo, slug); err != nil {
			return ideacli.Failed(stderr, "bet", err)
		}
	}
	var out workapi.BetOut
	in := workapi.StartIn{Repo: repo, Slug: slug, Appetite: f.Appetite, Person: who}
	if err := b.Call("bet.start", in, &out); err != nil {
		return ideacli.Failed(stderr, "bet", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "bet", output.JSON(stdout, out))
	}
	b.View(stdout).Title(fmt.Sprintf("Bet %s on %q, appetite %s", out.Slug, out.IdeaText, pitch.Tokens(out.Appetite)))
	return 0
}

// appetite is what to bet with when --appetite is not given: nothing when
// the idea's pitch has one, else what a person at a terminal types.
func (b bets) appetite(repo, slug string) (string, error) {
	var idea workapi.IdeaOut
	if err := b.Call("idea.show", workapi.SlugIn{Repo: repo, Slug: slug}, &idea); err != nil {
		return "", err
	}
	if idea.Pitch != nil {
		return "", nil
	}
	if !b.Interactive() {
		return "", &ideacli.UsageError{Msg: "the idea has no pitch to take an appetite from; give --appetite <tokens>, such as 200k"}
	}
	return b.Ask("Appetite in tokens", "200k", func(s string) error {
		_, err := pitch.ParseTokens(strings.TrimSpace(s))
		return err
	})
}

func (b bets) list(args []string, stdout, stderr io.Writer) int {
	f, ok := ideacli.Parse(args, "json")
	if !ok || len(f.Args) > 0 {
		return cli.BadUsage
	}
	repo, err := b.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "bet list", err)
	}
	all, err := b.allBets(repo)
	if err != nil {
		return ideacli.Failed(stderr, "bet list", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "bet list", output.JSON(stdout, all))
	}
	v := b.View(stdout)
	if len(all) == 0 {
		v.Line("No bets yet. Make one with physa bet <idea slug>.")
		return 0
	}
	rows := make([][]string, len(all))
	for i, o := range all {
		rows[i] = []string{o.Slug, o.IdeaText, pitch.Tokens(o.Appetite), pitch.Tokens(o.Spent), o.State, o.Person}
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

// chooseBet returns the bet slug given, or the one chosen from the bets
// that keep(state) accepts.
func (b bets) chooseBet(repo string, given string, keep func(workapi.BetOut) bool) (string, error) {
	if given != "" {
		return given, nil
	}
	all, err := b.allBets(repo)
	if err != nil {
		return "", err
	}
	var cs []ideacli.Choice
	for _, o := range all {
		if keep(o) {
			cs = append(cs, ideacli.Choice{Slug: o.Slug, Text: o.IdeaText, Detail: o.State + ", " + pitch.Tokens(o.Appetite)})
		}
	}
	return b.ChooseSlug("bet", "", cs)
}

func (b bets) show(args []string, stdout, stderr io.Writer) int {
	f, ok := ideacli.Parse(args, "json")
	if !ok || len(f.Args) > 1 {
		return cli.BadUsage
	}
	repo, err := b.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "bet show", err)
	}
	slug, err := b.chooseBet(repo, firstArg(f.Args), func(workapi.BetOut) bool { return true })
	if err != nil {
		return ideacli.Failed(stderr, "bet show", err)
	}
	var o workapi.BetOut
	if err := b.Call("bet.show", workapi.SlugIn{Repo: repo, Slug: slug}, &o); err != nil {
		return ideacli.Failed(stderr, "bet show", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "bet show", output.JSON(stdout, o))
	}
	v := b.View(stdout)
	v.Title(o.IdeaText)
	pairs := [][2]string{
		{"slug", o.Slug}, {"idea", o.Idea}, {"person", o.Person}, {"state", o.State},
		{"appetite", pitch.Tokens(o.Appetite)}, {"spent", pitch.Tokens(o.Spent)}, {"made", ideacli.When(o.CreatedAt)},
	}
	for _, p := range [][2]string{{"started", o.StartedAt}, {"ended", o.EndedAt}} {
		if p[1] != "" {
			pairs = append(pairs, [2]string{p[0], ideacli.When(p[1])})
		}
	}
	if o.StopReason != "" {
		pairs = append(pairs, [2]string{"stop reason", o.StopReason})
	}
	v.Fields(pairs...)
	if o.Pitch != nil {
		v.Section("Pitch", summary(*o.Pitch))
	}
	return 0
}

// summary is a line for each part of p: its name and the start of its text.
func summary(p workapi.PitchOut) string {
	var lines []string
	for _, part := range [][2]string{
		{"Problem", p.Problem}, {"Appetite", pitch.Tokens(p.Appetite)}, {"Solution", p.Solution},
		{"Scope", p.Scope}, {"Signal", p.Signal}, {"Rabbit holes", p.RabbitHoles}, {"No-gos", p.NoGos},
	} {
		text, _, _ := strings.Cut(strings.TrimSpace(part[1]), "\n")
		if r := []rune(text); len(r) > 60 {
			text = string(r[:59]) + "…"
		}
		lines = append(lines, fmt.Sprintf("%-12s  %s", part[0], text))
	}
	return strings.Join(lines, "\n")
}

func (b bets) stop(args []string, stdout, stderr io.Writer) int {
	if b.refuse("bet.stop", "bet stop", stderr) {
		return 2
	}
	f, ok := ideacli.Parse(args, "reason", "yes", "json")
	if !ok || len(f.Args) > 1 {
		return cli.BadUsage
	}
	repo, err := b.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "bet stop", err)
	}
	who := b.Who(repo, stderr)
	if who == "" {
		return 2
	}
	slug, err := b.chooseBet(repo, firstArg(f.Args), func(o workapi.BetOut) bool { return o.State == "active" })
	if err != nil {
		return ideacli.Failed(stderr, "bet stop", err)
	}
	if b.Interactive() && !f.Yes {
		yes, err := b.Confirm("Stop bet " + slug + "?")
		if err != nil && !errors.Is(err, picker.ErrAborted) {
			return ideacli.Failed(stderr, "bet stop", err)
		}
		if !yes {
			fmt.Fprintln(stderr, "physa bet stop: not stopped")
			return 2
		}
	}
	var o workapi.BetOut
	if err := b.Call("bet.stop", workapi.StopIn{Repo: repo, Slug: slug, Reason: f.Reason, Person: who}, &o); err != nil {
		return ideacli.Failed(stderr, "bet stop", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "bet stop", output.JSON(stdout, o))
	}
	b.View(stdout).Title("Stopped bet " + o.Slug)
	return 0
}
