// Package evidencecli is the evidence command: it lists the facts the engine
// observed in version control and the forge, newest first, for the repo or
// narrowed to a capability or a pursuit. It reads what the engine stored and
// starts no poll. Evidence is a top-level command because it spans
// capabilities and pursuits, so it belongs to neither the pursuit group nor map.
package evidencecli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/workapi"
)

const usage = "physa evidence [--capability <slug>] [--pursuit <slug>] [--limit N] [--json]"

// Command returns the evidence command, naming people through the forge
// newForge builds for a repo.
func Command(newForge func(dir string) forge.Forge) *cobra.Command {
	return commandFor(ideacli.New(newForge))
}

func commandFor(env *ideacli.Env) *cobra.Command {
	var (
		in     workapi.EvidenceIn
		limit  string
		asJSON bool
	)
	return command.Declare(command.New("evidence",
		"List the facts observed in version control and the forge, newest first: changes landed and reverted, a merged PR's lead time and its CI result. "+
			"It reads what the engine stored and polls nothing.",
		[]string{usage}, func(args []string, stdout, stderr io.Writer) int {
			if len(args) > 0 {
				return command.BadUsage
			}
			if limit != "" {
				n, err := strconv.Atoi(limit)
				if err != nil || n < 1 {
					return command.BadUsage
				}
				in.Limit = n
			}
			return run(env, in, asJSON, stdout, stderr)
		}),
		func(fs *pflag.FlagSet) {
			in, limit = workapi.EvidenceIn{}, ""
			ideacli.String(fs, "capability", &in.Capability)
			ideacli.String(fs, "pursuit", &in.Pursuit)
			ideacli.String(fs, "limit", &limit)
			ideacli.JSON(fs, &asJSON)
		})
}

func run(env *ideacli.Env, in workapi.EvidenceIn, asJSON bool, stdout, stderr io.Writer) int {
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

// Line is a fact as one line of plain words, for a pursuit's Evidence section.
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
