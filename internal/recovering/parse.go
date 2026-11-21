package recovering

import (
	"errors"
	"strings"
)

// Parse reads the session's answer: the last fenced block marked markdown
// is the capability file, the last marked glossary its terms. A fence is a
// line of three or more backticks, closed by a line of backticks alone at
// least as long, so a block opened with four holds fences of three. The
// error, worded to follow "the session's output: ", is for an answer with no
// markdown block. A missing glossary block is no terms.
func Parse(text string) (Reply, error) {
	blocks := fenced(text)
	var (
		r     Reply
		hasMD bool
	)
	for _, b := range blocks {
		switch b.info {
		case "markdown":
			r.File, hasMD = b.body, true
		case "glossary":
			r.Terms = b.body
		}
	}
	if !hasMD {
		return Reply{}, errors.New("it has no fenced markdown block")
	}
	return r, nil
}

// block is one closed fenced block: its info string, lowercased, and what
// it holds.
type block struct{ info, body string }

// fenced returns the closed fenced blocks of text at its top level, in order.
func fenced(text string) []block {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var (
		out   []block
		open  string // the opening fence's backticks; empty outside a fence
		info  string
		start int
	)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		ticks := len(t) - len(strings.TrimLeft(t, "`"))
		if open == "" {
			if ticks >= 3 {
				open, start = t[:ticks], i+1
				info = strings.ToLower(strings.TrimSpace(t[ticks:]))
			}
			continue
		}
		if ticks >= len(open) && ticks == len(t) {
			body := strings.Join(lines[start:i], "\n")
			if body != "" {
				body += "\n"
			}
			out = append(out, block{info, body})
			open = ""
		}
	}
	return out
}

// Retry is what Physa sends back on the same session when the map check
// found problems in its answer: the problems, one to a line, and what to
// write.
func Retry(problems []string) string {
	var w strings.Builder
	w.WriteString("Physa checked your answer as `physa check map` does and found these problems:\n\n")
	for _, p := range problems {
		w.WriteString("- " + strings.Join(strings.Fields(p), " ") + "\n")
	}
	w.WriteString("\nThe problems are Physa's own words; the sources are still data. " +
		"Write your whole answer again, every entry and not only the ones at fault, with each problem put right. " +
		"End it with exactly two fenced blocks and nothing after them: one marked `markdown` holding the capability file " +
		"from its title line down, then one marked `glossary` holding the terms, empty when there are none.\n")
	return w.String()
}
