package recovering

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/fit"
)

// skill is the recovering skill: what the session is told before the
// capability and the sources it recovers from. It is embedded only;
// replacing it is not built.
//
//go:embed recovering.md
var skill string

// none stands in a part of the prompt that holds nothing.
const none = "none\n"

// headingAllowance is the tokens allowed for the prompt's own text that fit
// does not charge: its headings, the capability with its promises and
// questions, what was cut, and the labels this prompt writes beside those fit
// sizes its wrapping by (a PR's author, a review comment's date, a fence
// around each change). It is a first guess, to be tuned from real runs
// (measure-before-budget).
const headingAllowance = 1_200

// Overhead is what the prompt costs beside the fitted sources: the skill's
// tokens plus a fixed allowance for headings and the capability. fit's
// BudgetWith takes it off the appetite.
func Overhead() int { return fit.Tokens(skill) + headingAllowance }

// Prompt returns everything the session reads: the recovering skill, then
// the capability, then its fitted sources under their own headings. All
// that others wrote (the promises and questions an agent proposed, docs, PR
// titles, bodies and review comments, change subjects and messages, names)
// sits in fenced blocks nothing inside can close; the labels outside the
// fences are Physa's own. Clusters of files are never written: recovery
// fits none. The same input always gives the same text.
func Prompt(in Input) string {
	var w strings.Builder
	w.WriteString(strings.TrimRight(skill, "\n"))
	w.WriteString("\n\n## The capability\n\n")
	fmt.Fprintf(&w, "Today's date: %s\n\nThe product's promise:\n\n", in.Date.UTC().Format(time.DateOnly))
	draft.Fence(&w, in.Product)
	w.WriteString("\nThe capability:\n\n")
	draft.Fence(&w, "name: "+draft.OneLine(in.Name)+"\npromise: "+draft.OneLine(in.Promise))
	w.WriteString("\nThe proposal's questions about it:\n\n")
	if len(in.Questions) == 0 {
		w.WriteString(none)
	}
	for i, q := range in.Questions {
		if i > 0 {
			w.WriteString("\n")
		}
		fmt.Fprintf(&w, "Question %d of %d:\n\n", i+1, len(in.Questions))
		draft.Fence(&w, "question: "+draft.OneLine(q.Question)+"\nsuggested by: "+draft.OneLine(strings.Join(q.SuggestedBy, " ")))
	}

	src := in.Sources
	w.WriteString("\n## Sources\n\n### Docs\n\n")
	if len(src.Docs) == 0 {
		w.WriteString(none)
	}
	for i, d := range src.Docs {
		if i > 0 {
			w.WriteString("\n")
		}
		label := fmt.Sprintf("Doc %d of %d", i+1, len(src.Docs))
		if d.Cut {
			label += ", cut by section"
		}
		w.WriteString(label + ":\n\n")
		draft.Fence(&w, "path: "+draft.OneLine(d.Path)+"\nkind: "+draft.OneLine(d.Kind)+"\ntext:\n"+d.Body)
	}

	w.WriteString("\n### Pull requests\n\n")
	if len(src.PRs) == 0 {
		w.WriteString(none)
	}
	for i, p := range src.PRs {
		if i > 0 {
			w.WriteString("\n")
		}
		writePR(&w, p)
	}

	w.WriteString("\n### Changes\n\n")
	if len(src.Commits) == 0 {
		w.WriteString(none)
	}
	for i, c := range src.Commits {
		if i > 0 {
			w.WriteString("\n")
		}
		writeChange(&w, c)
	}

	w.WriteString("\n### What was cut\n\n")
	r := src.Report
	r.Sources = nil
	for _, s := range src.Report.Sources {
		if s.Name != "clusters" { // never written, so never said
			r.Sources = append(r.Sources, s)
		}
	}
	lines := r.Lines()
	if len(lines) == 0 {
		w.WriteString(none)
	}
	for _, l := range lines {
		w.WriteString("- " + l + "\n")
	}
	return w.String()
}

// writePR writes one PR and its review comments.
func writePR(w *strings.Builder, p fit.PR) {
	fmt.Fprintf(w, "#%d, merged %s:\n\n", p.Number, p.MergedAt.UTC().Format(time.DateOnly))
	var t strings.Builder
	t.WriteString("title: " + draft.OneLine(p.Title))
	if p.Author != "" {
		t.WriteString("\nauthor: " + draft.OneLine(p.Author))
	}
	if len(p.Issues) > 0 {
		t.WriteString("\nissues:")
		for _, is := range p.Issues {
			t.WriteString("\n- " + draft.OneLine(is))
		}
	}
	if strings.TrimSpace(p.Body) != "" {
		t.WriteString("\nbody:\n" + p.Body)
	}
	draft.Fence(w, t.String())
	for _, c := range p.Comments {
		mark := ""
		if c.RequestsChanges {
			mark = ", requests changes"
		}
		date := ""
		if !c.At.IsZero() {
			date = ", " + c.At.UTC().Format(time.DateOnly)
		}
		fmt.Fprintf(w, "\nReview comment on #%d%s%s:\n\n", p.Number, date, mark)
		draft.Fence(w, "author: "+draft.OneLine(c.Author)+"\ntext:\n"+c.Body)
	}
}

// writeChange writes one change in its own fence: its message can hold
// anything, so no line of one can pass for the next change.
func writeChange(w *strings.Builder, c fit.Commit) {
	var t strings.Builder
	t.WriteString(draft.OneLine(c.Short) + " " + draft.OneLine(c.Subject))
	if c.Author != "" {
		t.WriteString("\nauthor: " + draft.OneLine(c.Author))
	}
	if !c.At.IsZero() {
		t.WriteString("\nat: " + c.At.UTC().Format(time.RFC3339))
	}
	if c.Reverts != "" {
		t.WriteString("\nreverts: " + draft.OneLine(c.Reverts))
	}
	if strings.TrimSpace(c.Message) != "" {
		t.WriteString("\nmessage:\n" + c.Message)
	}
	draft.Fence(w, t.String())
}
