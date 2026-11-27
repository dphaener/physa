package discovery

import (
	"encoding/json"
	"strings"

	"github.com/dphaener/physa/internal/jsonblock"
)

// Parse reads the sources from the last fenced json block of a reply. An
// entry with no path or no reason is dropped and counted in malformed; a
// reply with no readable block counts as one malformed and names nothing.
func Parse(text string) (sources []Source, malformed int) {
	block, ok := jsonblock.Last(text)
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
