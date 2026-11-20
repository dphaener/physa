// Package mapindex builds the index: every capability with its one-line
// promise and its counts of rules, decisions and questions. It is derived
// from the map on every call and never stored.
package mapindex

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/mapfile"
)

// Line is one capability's line of the index.
type Line struct {
	Name, Promise               string
	Rules, Decisions, Questions int
}

// Build returns one Line per capability, in the order the map holds them
// (sorted by name). product.md is not a capability and is left out.
func Build(m *mapfile.Map) []Line {
	lines := make([]Line, 0, len(m.Capabilities))
	for _, c := range m.Capabilities {
		l := Line{Name: c.Name, Promise: c.Promise}
		for _, e := range c.Entries {
			switch e.Kind {
			case mapfile.Rule:
				l.Rules++
			case mapfile.Decision:
				l.Decisions++
			case mapfile.Question:
				l.Questions++
			}
		}
		lines = append(lines, l)
	}
	return lines
}

// Write prints one line per capability: the name padded to the longest name
// plus two spaces, the promise, then its counts. It writes nothing for no
// lines.
func Write(w io.Writer, lines []Line) error {
	width := 0
	for _, l := range lines {
		width = max(width, len(l.Name))
	}
	bw := bufio.NewWriter(w)
	for _, l := range lines {
		promise := strings.TrimSpace(l.Promise)
		if promise == "" {
			promise = "(no promise)"
		}
		s := fmt.Sprintf("%-*s  %s", width, l.Name, promise)
		if c := counts(l); c != "" {
			s += "  " + c
		}
		if _, err := bw.WriteString(strings.TrimRight(s, " ") + "\n"); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func counts(l Line) string {
	var parts []string
	add := func(n int, one, many string) {
		switch {
		case n == 1:
			parts = append(parts, "1 "+one)
		case n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	add(l.Rules, "rule", "rules")
	add(l.Decisions, "decision", "decisions")
	add(l.Questions, "question", "questions")
	return strings.Join(parts, " · ")
}
