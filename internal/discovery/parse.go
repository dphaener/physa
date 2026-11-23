package discovery

import (
	"encoding/json"
	"strings"
)

// Parse reads the sources from the last fenced json block of a reply. An
// entry with no path or no reason is dropped and counted in malformed; a
// reply with no readable block counts as one malformed and names nothing.
func Parse(text string) (sources []Source, malformed int) {
	block, ok := lastJSONBlock(text)
	if !ok {
		return nil, 1
	}
	var w struct {
		Sources []json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal([]byte(block), &w); err != nil {
		return nil, 1
	}
	for _, raw := range w.Sources {
		var e struct {
			Path   string `json:"path"`
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(raw, &e); err != nil || strings.TrimSpace(e.Path) == "" || strings.TrimSpace(e.Reason) == "" {
			malformed++
			continue
		}
		sources = append(sources, Source{Path: strings.TrimSpace(e.Path), Kind: strings.TrimSpace(e.Kind), Reason: strings.Join(strings.Fields(e.Reason), " ")})
	}
	return sources, malformed
}

// lastJSONBlock returns the content of the last closed fence opened by a line
// of three or more backticks followed by json.
func lastJSONBlock(text string) (string, bool) {
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
