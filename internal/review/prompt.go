package review

import (
	_ "embed"
	"errors"
	"regexp"
	"strings"

	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/mapindex"
	"github.com/dphaener/physa/internal/mapslice"
)

// skill is the reviewing skill: what the reviewer is told before the
// change it judges.
//
//go:embed reviewing.md
var skill string

// Input is one change as the reviewer receives it.
type Input struct {
	Title, Body, Diff string
	Capabilities      []string // the capability names the commits' trailers name
}

// noSlice stands in the Slices section when the trailers name no capability
// the map holds.
const noSlice = "The commits name no capability that exists in the map.\n"

// loadedLine matches a Loaded: trailer line, so that what the change loaded
// never reaches the reviewer: misses and excess compare it with a touched
// list the reviewer judged without it.
var loadedLine = regexp.MustCompile(`(?im)^[ \t]*loaded[ \t]*:.*(\n|$)`)

// Prompt returns everything the reviewer reads for one change: the
// reviewing skill, then the PR title, the PR body, the index, the slices for
// the capabilities in.Capabilities that m holds, and the diff, each under
// its own heading. Names m does not hold are left out. Loaded: lines are
// cut from the title and body. The same map and input always give the same
// text.
func Prompt(m *mapfile.Map, in Input) (string, error) {
	if m == nil {
		return "", errors.New("no map to review against")
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(skill, "\n"))
	b.WriteString("\n\n## PR title\n\n")
	b.WriteString(orEmpty(strings.TrimSpace(loadedLine.ReplaceAllString(in.Title, ""))))
	b.WriteString("\n\n## PR body\n\n")
	if body := strings.TrimSpace(loadedLine.ReplaceAllString(in.Body, "")); body == "" {
		b.WriteString("(empty)\n")
	} else {
		fenced(&b, "text", body)
	}
	b.WriteString("\n## Index\n\n")
	if err := mapindex.Write(&b, mapindex.Build(m)); err != nil {
		return "", err
	}
	b.WriteString("\n## Slices\n\n")
	if names := known(m, in.Capabilities); len(names) == 0 {
		b.WriteString(noSlice)
	} else {
		s, err := mapslice.Build(m, mapslice.Request{Capabilities: names, WithDecisions: true, WithQuestions: true})
		if err != nil {
			return "", err
		}
		s.Trailers = nil // how to anchor is for the agent that writes the change
		if err := mapslice.WriteMD(&b, s); err != nil {
			return "", err
		}
	}
	b.WriteString("\n## Diff\n\n")
	fenced(&b, "diff", strings.TrimRight(in.Diff, "\n"))
	return b.String(), nil
}

// known returns the names m holds as capabilities, each once, in the order
// given.
func known(m *mapfile.Map, names []string) []string {
	has := map[string]bool{}
	for _, c := range m.Capabilities {
		has[c.Name] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if has[n] && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func orEmpty(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}

// fenced writes text in a fence longer than any run of backticks in it, so
// that nothing inside can close it.
func fenced(b *strings.Builder, info, text string) {
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
	b.WriteString(fence + info + "\n")
	if text != "" {
		b.WriteString(text + "\n")
	}
	b.WriteString(fence + "\n")
}
