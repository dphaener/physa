package discovery

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/dphaener/physa/internal/draft"
)

// skill is the discovery skill: what the session is told before the repo it
// reads. It is embedded only.
//
//go:embed discovery.md
var skill string

// agentKind is the kind of the docs whose bodies the prompt carries.
const agentKind = "agent"

// Prompt returns everything the session starts from: the discovery skill,
// then the file tree and the deterministic finds. Paths and the agent docs'
// bodies are others' text and sit in fenced blocks nothing inside can close;
// the labels outside the fences are Physa's own.
func Prompt(in Input) string {
	var w strings.Builder
	w.WriteString(strings.TrimRight(skill, "\n"))
	w.WriteString("\n\n## The repo's files\n\n")
	if len(in.Tree) == 0 {
		w.WriteString("none\n")
	} else {
		var t strings.Builder
		for _, p := range in.Tree {
			t.WriteString(draft.OneLine(p) + "\n")
		}
		draft.Fence(&w, t.String())
	}
	if in.Truncated > 0 {
		fmt.Fprintf(&w, "\n%d more files are not listed; search for them.\n", in.Truncated)
	}
	w.WriteString("\n## Already found\n\nThese are found already; do not name them again.\n\n")
	if len(in.Found) == 0 {
		w.WriteString("none\n")
	} else {
		var t strings.Builder
		for _, d := range in.Found {
			t.WriteString(draft.OneLine(d.Path) + " (" + draft.OneLine(d.Kind) + ")\n")
		}
		draft.Fence(&w, t.String())
	}
	for _, d := range in.Found {
		if d.Kind != agentKind {
			continue
		}
		w.WriteString("\nAgent doc " + draft.OneLine(d.Path) + ":\n\n")
		draft.Fence(&w, d.Body)
	}
	return w.String()
}
