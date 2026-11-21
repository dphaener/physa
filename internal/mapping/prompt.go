package mapping

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/fit"
)

// skill is the mapping skill: what the session is told before the sources it
// proposes from. It is embedded only; replacing it is not built.
//
//go:embed mapping.md
var skill string

// none stands in a part of the prompt that holds nothing.
const none = "none\n"

// headingAllowance is the tokens allowed for the prompt's own text that no
// item carries: its headings, the fences around changes and clusters, and
// what was cut. Each item's own labels and fences are charged by fit as its
// wrapping. It is a first guess, to be tuned from real runs
// (measure-before-budget).
const headingAllowance = 400

// Overhead is what the prompt costs beside the fitted sources: the skill's
// tokens plus a fixed allowance for headings. fit.Budget takes it off the
// appetite.
func Overhead() int { return fit.Tokens(skill) + headingAllowance }

// Prompt returns everything the session reads: the mapping skill, then the
// fitted sources under their own headings. All that others wrote (docs, PR
// titles, bodies and review comments, change subjects, paths) sits in fenced
// blocks nothing inside can close; the labels outside the fences are
// Physa's own. The same input always gives the same text.
func Prompt(in fit.Input) string {
	var w strings.Builder
	w.WriteString(strings.TrimRight(skill, "\n"))
	w.WriteString("\n\n## Sources\n\n### Docs\n\n")
	if len(in.Docs) == 0 {
		w.WriteString(none)
	}
	for i, d := range in.Docs {
		if i > 0 {
			w.WriteString("\n")
		}
		label := fmt.Sprintf("Doc %d of %d", i+1, len(in.Docs))
		if d.Cut {
			label += ", cut by section"
		}
		w.WriteString(label + ":\n\n")
		draft.Fence(&w, "path: "+draft.OneLine(d.Path)+"\nkind: "+draft.OneLine(d.Kind)+"\ntext:\n"+d.Body)
	}

	w.WriteString("\n### Pull requests\n\n")
	if len(in.PRs) == 0 {
		w.WriteString(none)
	}
	for i, p := range in.PRs {
		if i > 0 {
			w.WriteString("\n")
		}
		fmt.Fprintf(&w, "#%d, merged %s:\n\n", p.Number, p.MergedAt.UTC().Format(time.DateOnly))
		var t strings.Builder
		t.WriteString("title: " + draft.OneLine(p.Title))
		if len(p.Issues) > 0 {
			t.WriteString("\nissues:")
			for _, is := range p.Issues {
				t.WriteString("\n- " + draft.OneLine(is))
			}
		}
		if strings.TrimSpace(p.Body) != "" {
			t.WriteString("\nbody:\n" + p.Body)
		}
		draft.Fence(&w, t.String())
		for _, c := range p.Comments {
			mark := ""
			if c.RequestsChanges {
				mark = ", requests changes"
			}
			fmt.Fprintf(&w, "\nReview comment on #%d%s:\n\n", p.Number, mark)
			draft.Fence(&w, "author: "+draft.OneLine(c.Author)+"\ntext:\n"+c.Body)
		}
	}

	w.WriteString("\n### Changes\n\n")
	if len(in.Commits) == 0 {
		w.WriteString(none)
	} else {
		var t strings.Builder
		for _, c := range in.Commits {
			t.WriteString(draft.OneLine(c.Short) + " " + draft.OneLine(c.Subject) + "\n")
		}
		draft.Fence(&w, t.String())
	}

	w.WriteString("\n### Files that change together (hints)\n\n")
	if len(in.Clusters) == 0 {
		w.WriteString(none)
	} else {
		var t strings.Builder
		for i, c := range in.Clusters {
			fmt.Fprintf(&t, "cluster %d, %d changes:\n", i+1, c.Changes)
			for _, p := range c.Paths {
				t.WriteString("- " + draft.OneLine(p) + "\n")
			}
		}
		draft.Fence(&w, t.String())
	}

	w.WriteString("\n### What was cut\n\n")
	lines := in.Report.Lines()
	if len(lines) == 0 {
		w.WriteString(none)
	}
	for _, l := range lines {
		w.WriteString("- " + l + "\n")
	}
	return w.String()
}
