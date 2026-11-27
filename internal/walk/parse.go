package walk

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/draft"
)

// Capability is one capability as the session names it: the source files it
// owns and the tests and docs that bear on it.
type Capability struct {
	Name    string   `json:"name"`
	Promise string   `json:"promise"`
	Sources []string `json:"sources"`
	Tests   []string `json:"tests"`
	Docs    []string `json:"docs"`
}

// Shared is a source file, or a folder ending with a slash, kept by many
// capabilities or by none, with the reason.
type Shared struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Part is one part of a split area.
type Part struct {
	Name    string   `json:"name"`
	Promise string   `json:"promise"`
	Sources []string `json:"sources"`
}

// Answer is a walk session's answer: the capabilities and shared files of
// its area, or the parts it splits into.
type Answer struct {
	Capabilities []Capability `json:"capabilities"`
	Shared       []Shared     `json:"shared"`
	Split        []Part       `json:"split"`
}

// Fix is the answer to FixPrompt: only the changes. Add moves paths to a
// capability (or a part) from every other claim, Shared marks files shared,
// New adds capabilities, Drop takes paths back from every claim, and Remove
// takes capabilities out by name.
type Fix struct {
	Add    map[string][]string `json:"add"`
	Shared []Shared            `json:"shared"`
	New    []Capability        `json:"new"`
	Drop   []string            `json:"drop"`
	Remove []string            `json:"remove"`
}

// Parse reads the answer in the last fenced json block of text. An answer
// with neither capabilities nor a split is an error, worded to follow "the
// walk answer: ".
func Parse(text string) (Answer, error) {
	var a Answer
	if err := decode(text, &a); err != nil {
		return Answer{}, err
	}
	if len(a.Capabilities) == 0 && len(a.Split) == 0 {
		return Answer{}, errors.New(`its json block holds neither "capabilities" nor "split"`)
	}
	for i := range a.Capabilities {
		a.Capabilities[i].Promise = draft.OneLine(a.Capabilities[i].Promise)
	}
	return a, nil
}

// ParseFix reads the answer to FixPrompt from the last fenced json block.
func ParseFix(text string) (Fix, error) {
	var f Fix
	if err := decode(text, &f); err != nil {
		return Fix{}, err
	}
	return f, nil
}

// Apply returns ans with fix applied: removals, drops and moves, then adds,
// shared files and new capabilities (in a split answer Add names parts).
func Apply(ans Answer, fix Fix) Answer {
	out, moved := Answer{Shared: slices.Clone(ans.Shared)}, slices.Clone(fix.Drop)
	for _, paths := range fix.Add {
		moved = append(moved, paths...)
	}
	for _, c := range ans.Capabilities {
		if !slices.Contains(fix.Remove, c.Name) {
			c.Sources, c.Tests, c.Docs = without(c.Sources, moved), without(c.Tests, fix.Drop), without(c.Docs, fix.Drop)
			out.Capabilities = append(out.Capabilities, c)
		}
	}
	for _, p := range ans.Split {
		p.Sources = without(p.Sources, moved)
		out.Split = append(out.Split, p)
	}
	out.Shared = slices.DeleteFunc(out.Shared, func(s Shared) bool { return slices.Contains(moved, s.Path) })
	for name, paths := range fix.Add {
		for i := range out.Capabilities {
			if out.Capabilities[i].Name == name {
				out.Capabilities[i].Sources = append(out.Capabilities[i].Sources, paths...)
			}
		}
		for i := range out.Split {
			if out.Split[i].Name == name {
				out.Split[i].Sources = append(out.Split[i].Sources, paths...)
			}
		}
	}
	out.Shared = append(out.Shared, fix.Shared...)
	out.Capabilities = append(out.Capabilities, fix.New...)
	return out
}

func without(list, drop []string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(s string) bool { return slices.Contains(drop, s) })
}

func decode(text string, v any) error {
	block, ok := lastJSONBlock(text)
	if !ok {
		return errors.New("it has no fenced json block")
	}
	if err := json.Unmarshal([]byte(block), v); err != nil {
		return errors.New("its json block does not decode (" + err.Error() + ")")
	}
	return nil
}

// lastJSONBlock returns the content of the last closed fence opened by a
// line of three or more backticks followed by json.
func lastJSONBlock(text string) (string, bool) {
	var found, open string
	var ok, isJSON bool
	var start int
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if open == "" {
			if ticks := len(t) - len(strings.TrimLeft(t, "`")); ticks >= 3 {
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
