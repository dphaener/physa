// Package evidencecli is the evidence command: it lists the facts the engine
// observed in version control and the forge, newest first, for the repo or
// narrowed to a capability or a bet. It reads what the engine stored and
// starts no poll. Evidence is a top-level command because it spans
// capabilities and bets, so it belongs to neither the bet group nor map.
package evidencecli

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/workapi"
)

const usage = "physa evidence [--capability <slug>] [--bet <slug>] [--limit N] [--json]"

// Command returns the evidence command, naming people through the forge
// newForge builds for a repo.
func Command(newForge func(dir string) forge.Forge) *cli.Command {
	return commandFor(ideacli.New(newForge))
}

func commandFor(env *ideacli.Env) *cli.Command {
	return &cli.Command{
		Name: "evidence",
		Summary: "List the facts observed in version control and the forge, newest first: changes landed and reverted, a merged PR's lead time and its CI result. " +
			"It reads what the engine stored and polls nothing.",
		Usage: []string{usage},
		Run:   func(args []string, stdout, stderr io.Writer) int { return run(env, args, stdout, stderr) },
	}
}

// parse reads the flags: --capability, --bet and --limit with values, --json
// alone. ok is false for anything else, a repeated flag or a bad limit.
func parse(args []string) (in workapi.EvidenceIn, asJSON, ok bool) {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name, val, hasVal := strings.Cut(strings.TrimPrefix(args[i], "--"), "=")
		if !strings.HasPrefix(args[i], "--") || seen[name] || !slices.Contains([]string{"capability", "bet", "limit", "json"}, name) {
			return in, false, false
		}
		seen[name] = true
		if name == "json" {
			if hasVal {
				return in, false, false
			}
			asJSON = true
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return in, false, false
			}
			i++
			val = args[i]
		}
		if val == "" {
			return in, false, false
		}
		switch name {
		case "capability":
			in.Capability = val
		case "bet":
			in.Bet = val
		case "limit":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return in, false, false
			}
			in.Limit = n
		}
	}
	return in, asJSON, true
}

func run(env *ideacli.Env, args []string, stdout, stderr io.Writer) int {
	in, asJSON, ok := parse(args)
	if !ok {
		return cli.BadUsage
	}
	repo, err := env.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "evidence", err)
	}
	in.Repo = repo
	var facts []workapi.EvidenceOut
	if err := env.Call("evidence.list", in, &facts); err != nil {
		return ideacli.Failed(stderr, "evidence", err)
	}
	if asJSON {
		if facts == nil {
			facts = []workapi.EvidenceOut{}
		}
		return ideacli.Written(stderr, "evidence", output.JSON(stdout, facts))
	}
	v := env.View(stdout)
	if len(facts) == 0 {
		v.Line("No evidence yet")
		return 0
	}
	rows := make([][]string, len(facts))
	for i, f := range facts {
		rows[i] = Cells(f)
	}
	v.Table([]string{"Fact", "Change", "Capability", "When", "Detail"}, rows)
	return 0
}

// Cells is a fact in plain words: its kind, the short commit or #<pr>, the
// capability, when, and its detail (the lead time, or the revert).
func Cells(f workapi.EvidenceOut) []string {
	ref := f.Commit
	if f.PR > 0 {
		ref = fmt.Sprintf("#%d", f.PR)
	}
	detail := ""
	switch {
	case f.Seconds != nil:
		detail = "lead time " + Duration(*f.Seconds)
	case f.ByCommit != "":
		detail = "by " + f.ByCommit
	}
	return []string{kindWords(f.Kind), dash(ref), dash(f.Capability), ideacli.When(f.At), detail}
}

// Line is a fact as one line of plain words, for a bet's Evidence section.
func Line(f workapi.EvidenceOut) string {
	return strings.TrimSpace(strings.Join(Cells(f), "  "))
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func kindWords(kind string) string {
	switch kind {
	case "lead_time":
		return "lead time"
	case "ci_passed":
		return "CI passed"
	case "ci_failed":
		return "CI failed"
	case "ci_passed_after_failing":
		return "CI passed after failing"
	}
	return strings.ReplaceAll(kind, "_", " ")
}

// Duration shows seconds as a person reads a lead time: "45s", "12m",
// "3h 5m" or "2d 4h", to the two largest units.
func Duration(secs int64) string {
	d := time.Duration(secs) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", secs)
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}
