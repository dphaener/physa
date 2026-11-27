package areas

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/jsonblock"
)

// Fix is a session's answer to FixPrompt: exact claims for files that were
// missing or claimed twice, and any areas it adds.
type Fix struct {
	Add      map[string][]string
	NewAreas []Area
}

// Joint is one merge of areas: From joins Into, which takes Promise.
type Joint struct {
	Into    string
	From    []string
	Promise string
	Why     string
}

// ParseSplit reads the split session's answer: the tests, docs and other
// patterns in the last fenced json block. A list the answer leaves out is
// empty. The error, worded to follow "the split answer: ", is for a reply
// with no such block or one that does not decode.
func ParseSplit(text string) (tests, docs, other []string, err error) {
	var w struct {
		Tests []string `json:"tests"`
		Docs  []string `json:"docs"`
		Other []string `json:"other"`
	}
	if err := decode(text, `an object with the lists "tests", "docs" and "other"`, &w); err != nil {
		return nil, nil, nil, err
	}
	return w.Tests, w.Docs, w.Other, nil
}

// answerArea is an area as the session writes it.
type answerArea struct {
	Name    string   `json:"name"`
	Promise string   `json:"promise"`
	Sources []string `json:"sources"`
}

func (a answerArea) area() Area {
	return Area{Name: kebab(a.Name), Promise: draft.OneLine(a.Promise), Sources: a.Sources}
}

// ParseAreas reads the grouping session's answer: the areas in the last
// fenced json block. Names are kebab-cased, an empty one becoming "unnamed",
// and promises are put on one line. The error follows "the areas answer: ".
func ParseAreas(text string) ([]Area, error) {
	var w struct {
		Areas *[]answerArea `json:"areas"`
	}
	const want = `an object with the list "areas", each with "name", "promise" and "sources"`
	if err := decode(text, want, &w); err != nil {
		return nil, err
	}
	if w.Areas == nil {
		return nil, errors.New("its json block has no \"areas\" list; expected " + want)
	}
	return areasOf(*w.Areas), nil
}

func areasOf(in []answerArea) []Area {
	out := make([]Area, len(in))
	for i, a := range in {
		out[i] = a.area()
	}
	return out
}

// ParseFix reads the answer to FixPrompt: "add" and "new_areas" in the last
// fenced json block. The error follows "the fix answer: ".
func ParseFix(text string) (Fix, error) {
	var w struct {
		Add      map[string][]string `json:"add"`
		NewAreas []answerArea        `json:"new_areas"`
	}
	const want = `an object with "add" (area name to paths) and "new_areas" (each with "name", "promise" and "sources")`
	if err := decode(text, want, &w); err != nil {
		return Fix{}, err
	}
	f := Fix{Add: map[string][]string{}, NewAreas: areasOf(w.NewAreas)}
	for name, paths := range w.Add {
		f.Add[kebab(name)] = append(f.Add[kebab(name)], paths...)
	}
	return f, nil
}

// ParseMerge reads the merge session's answer: the "merge" list in the last
// fenced json block, [] for no merge. The error follows "the merge answer: ".
func ParseMerge(text string) ([]Joint, error) {
	var w struct {
		Merge *[]struct {
			Into    string   `json:"into"`
			From    []string `json:"from"`
			Promise string   `json:"promise"`
			Why     string   `json:"why"`
		} `json:"merge"`
	}
	const want = `an object with the list "merge", each with "into", "from", "promise" and "why"`
	if err := decode(text, want, &w); err != nil {
		return nil, err
	}
	if w.Merge == nil {
		return nil, errors.New("its json block has no \"merge\" list; expected " + want)
	}
	out := make([]Joint, len(*w.Merge))
	for i, m := range *w.Merge {
		out[i] = Joint{Into: kebab(m.Into), Promise: draft.OneLine(m.Promise), Why: draft.OneLine(m.Why)}
		for _, f := range m.From {
			out[i].From = append(out[i].From, kebab(f))
		}
	}
	return out, nil
}

// decode reads the last fenced json block of text into v. want names the
// shape the error says was expected.
func decode(text, want string, v any) error {
	block, ok := jsonblock.Last(text)
	if !ok {
		return errors.New("it has no fenced json block; expected " + want)
	}
	if err := json.Unmarshal([]byte(block), v); err != nil {
		return errors.New("its json block does not decode (" + err.Error() + "); expected " + want)
	}
	return nil
}

var nonName = regexp.MustCompile(`[^a-z0-9]+`)

// kebab turns a name into kebab-case; one with nothing left is "unnamed".
func kebab(s string) string {
	s = strings.Trim(nonName.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "unnamed"
	}
	return s
}
