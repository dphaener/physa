// Package newcodebase is how Physa starts a map when there is nothing to
// recover (design 02 §2.8). It detects a new codebase from the gather, then
// asks a person a short round of numbered questions, each with a suggested
// answer, to write the product's promise and first terms, or reads the
// answers from a file. It never guesses for the person: a suggestion is
// used only when the person accepts it. It calls no agent and writes no file.
//
// The answers file read by ReadAnswers has one answer per line, as
// `1: <answer>` to `3: <answer>`, numbered as Questions numbers them. Blank
// lines and lines starting with `#` are ignored. A number left out takes the
// suggestion of the questions passed in; with none to take, the name and the
// promise are errors and the terms are none. The first colon ends the number,
// so a term's own colon stays in the answer, as in `3: Cut list: The parts`.
package newcodebase

import (
	"strings"
	"unicode"

	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/store"
)

// FewChanges is the most changes a new codebase may have had. It is a first
// guess, to be tuned against real repos.
const FewChanges = 5

// Question is one numbered question with the answer Physa suggests.
type Question struct {
	Number    int
	Text      string
	Suggested string
}

// New reports whether the gather is of a new codebase: no agent doc,
// contributing guide, overview doc or decision record (a README alone does not count,
// since generators write one), no PRs, and at most FewChanges changes.
func New(g store.Gather) bool {
	for _, d := range g.Docs {
		switch d.Kind {
		case gather.KindAgent, gather.KindContributing, gather.KindDecisions, gather.KindOverview:
			return false
		}
	}
	return len(g.PRs) == 0 && len(g.Changes) <= FewChanges
}

// Questions returns the three questions, numbered 1 to 3, with suggestions
// drawn from the repo's directory name and its README.
func Questions(g store.Gather, repoName string) []Question {
	name := TitleCase(repoName)
	promise := name + " lets its users …"
	for _, d := range g.Docs {
		if d.Kind == gather.KindReadme {
			if s := firstSentence(d.Body); s != "" {
				promise = s
				break
			}
		}
	}
	return []Question{
		{1, "What is the product called?", name},
		{2, "What does it promise its users, in one sentence?", promise},
		{3, "What are its first terms? Write each as `Term: meaning`, separated by `;`.", noTerms},
	}
}

// noTerms is the suggested answer to the third question, meaning no terms.
const noTerms = "none for now"

// TitleCase makes a product name from a directory name: words split at
// dashes, underscores, dots and spaces, each capitalised.
func TitleCase(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r) })
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// maxSuggestion is the longest README sentence offered as a promise.
const maxSuggestion = 200

// firstSentence is the first sentence of the README's first paragraph of
// prose, skipping headings, images, badges, tables, lists, quotes, fences
// and markup. It is empty when there is none worth suggesting.
func firstSentence(body string) string {
	var para []string
	fence := false
	for _, l := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "```") || strings.HasPrefix(l, "~~~") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if l == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		if len(para) == 0 && strings.ContainsAny(l[:1], "#![<|>-*+=") {
			continue
		}
		para = append(para, l)
	}
	text := strings.Join(para, " ")
	if i := strings.Index(text, ". "); i >= 0 {
		text = text[:i+1]
	}
	if len(text) > maxSuggestion || strings.Contains(text, "…") {
		return ""
	}
	return text
}
