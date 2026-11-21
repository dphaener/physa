package draft

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapindex"
	"github.com/dphaener/physa/internal/mapslice"
)

// skill is the learning skill: what the session is told before the batch it
// drafts from.
//
//go:embed learning.md
var skill string

// none stands in a section of the prompt that holds nothing.
const none = "none\n"

// Prompt returns everything the session reads for one batch: the learning
// skill, then the index, the slices for the batch's capabilities with their
// decisions and questions, and the signals, each under its own heading. All
// that others wrote (change messages, capability names from trailers,
// evidence) sits in fenced blocks nothing inside can close. The same map and
// batch always give the same text.
func Prompt(m *mapfile.Map, b Batch) (string, error) {
	if m == nil {
		return "", errors.New("no map to draft against")
	}
	var w strings.Builder
	w.WriteString(strings.TrimRight(skill, "\n"))
	w.WriteString("\n\n## Index\n\n")
	if err := mapindex.Write(&w, mapindex.Build(m)); err != nil {
		return "", err
	}
	w.WriteString("\n## Slices\n\n")
	if len(b.Capabilities) == 0 {
		w.WriteString(none)
	} else {
		s, err := mapslice.Build(m, mapslice.Request{Capabilities: b.Capabilities, WithDecisions: true, WithQuestions: true})
		if err != nil {
			return "", err
		}
		s.Trailers = nil // how to anchor a commit is for the agent that writes a change
		if err := mapslice.WriteMD(&w, s); err != nil {
			return "", err
		}
	}
	w.WriteString("\n## Signals\n\n")
	if b.Empty() {
		w.WriteString(none)
	}
	for i, c := range b.Changes {
		if i > 0 {
			w.WriteString("\n")
		}
		fmt.Fprintf(&w, "### Change %d of %d\n\n", i+1, len(b.Changes))
		fenced(&w, "change: "+oneLine(c.Short)+"\nsubject: "+oneLine(c.Subject)+"\nmessage:\n"+strings.TrimSpace(c.Message))
		for j, s := range c.Signals {
			fmt.Fprintf(&w, "\nSignal %d of %d on this change:\n\n", j+1, len(c.Signals))
			var t strings.Builder
			t.WriteString("kind: " + oneLine(s.Kind) + "\n")
			t.WriteString("rule: " + oneLine(s.Rule) + "\n")
			t.WriteString("changes: " + oneLine(strings.Join(s.Changes, " ")) + "\n")
			if s.Capability != "" {
				t.WriteString("capability: " + oneLine(s.Capability) + "\n")
			}
			t.WriteString("evidence:")
			for _, e := range s.Evidence {
				t.WriteString("\n- " + strings.TrimSpace(e))
			}
			fenced(&w, t.String())
		}
	}
	return w.String(), nil
}

// oneLine returns s with every run of white space as one space, so a value
// keeps to its own line of a fenced block.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// fenced writes text in a fence longer than any run of backticks in it, so
// that nothing inside can close it.
func fenced(w *strings.Builder, text string) {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	w.WriteString(fence + "text\n")
	if text = strings.TrimRight(text, "\n"); text != "" {
		w.WriteString(text + "\n")
	}
	w.WriteString(fence + "\n")
}
