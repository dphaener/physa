// Package jsonblock finds the last closed json fence of an agent's answer, so
// every phase that asks an agent for json reads its reply the same way.
package jsonblock

import "strings"

// Last returns the content of the last closed fence opened by a line of three
// or more backticks followed by json. A fence left unclosed is ignored, and a
// fence of another kind is skipped.
func Last(text string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var (
		found  string
		ok     bool
		open   string // the opening fence's backticks; empty outside a fence
		isJSON bool
		start  int
	)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if open == "" {
			ticks := len(t) - len(strings.TrimLeft(t, "`"))
			if ticks >= 3 {
				open, start = t[:ticks], i+1
				isJSON = strings.EqualFold(strings.TrimSpace(t[ticks:]), "json")
			}
			continue
		}
		if strings.HasPrefix(t, open) && strings.Trim(t, "`") == "" {
			if isJSON {
				found, ok = strings.Join(lines[start:i], "\n"), true
			}
			open = ""
		}
	}
	return found, ok
}
