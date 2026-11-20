// Package prcheck holds the result of the deterministic PR check: each commit
// with the capability it names and what it loaded, the trailer findings, and
// the map's problems as the PR leaves it. It renders the result as a short
// markdown summary and as the output of the physa check run, and reads that
// output back. Misses and excess wait for the reviewer agent.
package prcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/mapfile"
)

// Version is the version of the result's JSON shape.
const Version = 1

// MaxFieldBytes is GitHub's limit for a check run's summary and text.
const MaxFieldBytes = 65535

// How the map's PR anchors were checked.
const (
	PRAnchorsGitHub   = "github"
	PRAnchorsFormOnly = "form-only"
)

// Result is what the deterministic PR check found.
type Result struct {
	Version  int               `json:"version"`
	PR       int               `json:"pr,omitempty"`
	Range    string            `json:"range"`
	Commits  []history.Commit  `json:"commits"`
	Findings []history.Finding `json:"findings"`
	Map      MapResult         `json:"map"`
	Slice    SliceResult       `json:"slice"`
}

// MapResult is the map's check as the PR leaves it.
type MapResult struct {
	Problems  []Problem `json:"problems"`
	PRAnchors string    `json:"pr_anchors"` // PRAnchorsGitHub or PRAnchorsFormOnly
	Note      string    `json:"note,omitempty"`
}

// Problem is one problem the map check found.
type Problem struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// Problems converts the map reader's problems.
func Problems(ps []mapfile.Problem) []Problem {
	out := make([]Problem, 0, len(ps))
	for _, p := range ps {
		out = append(out, Problem{Path: p.Path, Line: p.Line, Message: p.Message})
	}
	return out
}

// SliceResult is the reviewer agent's part of the check. Each list is nil
// until the reviewer agent (plan step 2c) reports the capabilities the change
// touches; JSON null means not computed yet, never "none".
type SliceResult struct {
	Touched []string `json:"touched"`
	Misses  []string `json:"misses"`
	Excess  []string `json:"excess"`
}

// Output is the output object of a check run.
type Output struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Text    string `json:"text"`
}

// MarshalJSON encodes empty commits, findings and problems as [], never null.
func (r Result) MarshalJSON() ([]byte, error) {
	type plain Result
	p := plain(r)
	if p.Commits == nil {
		p.Commits = []history.Commit{}
	} else {
		p.Commits = append([]history.Commit(nil), p.Commits...)
		for i, c := range p.Commits {
			if c.Capabilities == nil {
				p.Commits[i].Capabilities = []string{}
			}
			if c.Loaded == nil {
				p.Commits[i].Loaded = []string{}
			}
		}
	}
	if p.Findings == nil {
		p.Findings = []history.Finding{}
	}
	if p.Map.Problems == nil {
		p.Map.Problems = []Problem{}
	}
	return json.Marshal(p)
}

func count(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Title is a one-line summary of the result.
func (r Result) Title() string {
	problems := "no map problems"
	if n := len(r.Map.Problems); n > 0 {
		problems = count(n, "map problem", "map problems")
	}
	return strings.Join([]string{
		count(len(r.Commits), "commit", "commits"),
		count(len(r.Findings), "trailer finding", "trailer findings"),
		problems,
	}, " · ")
}

func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

func list(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	return cell(strings.Join(s, ", "))
}

// WriteMarkdown writes the short summary of r.
func WriteMarkdown(w io.Writer, r Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", r.Title())
	if len(r.Commits) > 0 {
		b.WriteString("| Commit | Subject | Capability | Loaded |\n| --- | --- | --- | --- |\n")
		for _, c := range r.Commits {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(c.Short), cell(c.Subject), list(c.Capabilities), list(c.Loaded))
		}
		b.WriteString("\n")
	}
	if len(r.Findings) == 0 {
		b.WriteString("No trailer findings.\n\n")
	} else {
		b.WriteString("Trailer findings:\n\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "- %s: %s\n", f.Commit, f.Message)
		}
		b.WriteString("\n")
	}
	if len(r.Map.Problems) == 0 {
		b.WriteString("The map passes physa check.\n\n")
	} else {
		b.WriteString("Map problems:\n\n")
		for _, p := range r.Map.Problems {
			if p.Line == 0 {
				fmt.Fprintf(&b, "- %s: %s\n", p.Path, p.Message)
			} else {
				fmt.Fprintf(&b, "- %s:%d: %s\n", p.Path, p.Line, p.Message)
			}
		}
		b.WriteString("\n")
	}
	if r.Map.PRAnchors == PRAnchorsGitHub {
		b.WriteString("PR anchors: checked against GitHub.\n\n")
	} else {
		b.WriteString("PR anchors: checked for form only")
		if r.Map.Note != "" {
			b.WriteString(" (" + r.Map.Note + ")")
		}
		b.WriteString(".\n\n")
	}
	b.WriteString("Misses and excess: not computed yet; they need the reviewer agent's touched capabilities.\n")
	_, err := io.WriteString(w, b.String())
	return err
}

const fence = "```"

// CheckRunOutput builds the output of the physa check run: the summary is the
// markdown, the text is the result as JSON in a fenced block.
func CheckRunOutput(r Result) (Output, error) {
	var sum bytes.Buffer
	if err := WriteMarkdown(&sum, r); err != nil {
		return Output{}, err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return Output{}, err
	}
	o := Output{Title: r.Title(), Summary: sum.String(), Text: fence + "json\n" + string(data) + "\n" + fence + "\n"}
	if len(o.Summary) > MaxFieldBytes {
		return Output{}, fmt.Errorf("check run summary is %d bytes; GitHub allows %d", len(o.Summary), MaxFieldBytes)
	}
	if len(o.Text) > MaxFieldBytes {
		return Output{}, fmt.Errorf("check run text is %d bytes; GitHub allows %d", len(o.Text), MaxFieldBytes)
	}
	return o, nil
}

// Decode reads a result back from the text of a check run's output.
func Decode(text string) (Result, error) {
	start := strings.Index(text, fence+"json\n")
	if start < 0 {
		return Result{}, fmt.Errorf("no ```json block in the check run text")
	}
	body := text[start+len(fence)+len("json\n"):]
	end := strings.LastIndex(body, "\n"+fence)
	if end < 0 {
		return Result{}, fmt.Errorf("the ```json block in the check run text is not closed")
	}
	var r Result
	if err := json.Unmarshal([]byte(body[:end]), &r); err != nil {
		return Result{}, fmt.Errorf("decode check run result: %w", err)
	}
	if r.Version != Version {
		return Result{}, fmt.Errorf("check run result is version %d; this physa reads version %d", r.Version, Version)
	}
	return r, nil
}
