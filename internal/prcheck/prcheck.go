// Package prcheck holds the result of the deterministic PR check: each commit
// with the capability it names and what it loaded, the trailer findings, and
// the map's problems as the PR leaves it. It renders the result as a short
// markdown summary and as the output of the physa check run, and reads that
// output back. Misses and excess come from the capabilities the reviewer
// agent says the change touches, set against what the commits loaded; when the
// reviewer did not run, the result says so and why.
package prcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/review"
)

// Version is the version of the result's JSON shape. Version 1 results, which
// have no reviewer block, still decode.
const Version = 2

// The versions Decode reads.
const oldestVersion = 1

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
	Reviewer ReviewerResult    `json:"reviewer"`
}

// The values of ReviewerResult.Status.
const (
	ReviewerRan     = "ran"
	ReviewerSkipped = "skipped"
	ReviewerFailed  = "failed"
)

// ReviewerResult is what became of the reviewer agent: it ran and its review
// was valid, it was skipped, or it failed. A failure is reported with its
// reason, never dropped.
type ReviewerResult struct {
	Status string          `json:"status"` // ReviewerRan, ReviewerSkipped or ReviewerFailed
	Reason string          `json:"reason,omitempty"`
	Agent  string          `json:"agent,omitempty"`
	Model  string          `json:"model,omitempty"`
	Tokens *adapter.Tokens `json:"tokens,omitempty"`
	Review *review.Review  `json:"review,omitempty"`
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

// SliceResult is the reviewer agent's part of the check: the capabilities the
// change touches, the misses (touched but not loaded) and the excess (loaded
// but not touched). Each list is nil when the reviewer did not run; JSON null
// means not computed, while [] means computed and none.
type SliceResult struct {
	Touched []string `json:"touched"`
	Misses  []string `json:"misses"`
	Excess  []string `json:"excess"`
}

// Measure sets what the commits loaded against the capabilities the reviewer
// says the change touches. Every list is sorted and never nil.
func Measure(commits []history.Commit, touched []string) SliceResult {
	loaded := map[string]bool{}
	for _, c := range commits {
		for _, l := range c.Loaded {
			loaded[l] = true
		}
	}
	t := map[string]bool{}
	res := SliceResult{Touched: []string{}, Misses: []string{}, Excess: []string{}}
	for _, name := range touched {
		if t[name] {
			continue
		}
		t[name] = true
		res.Touched = append(res.Touched, name)
		if !loaded[name] {
			res.Misses = append(res.Misses, name)
		}
	}
	for name := range loaded {
		if !t[name] {
			res.Excess = append(res.Excess, name)
		}
	}
	slices.Sort(res.Touched)
	slices.Sort(res.Misses)
	slices.Sort(res.Excess)
	return res
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
	parts := []string{
		count(len(r.Commits), "commit", "commits"),
		count(len(r.Findings), "trailer finding", "trailer findings"),
		problems,
	}
	switch r.Reviewer.Status {
	case ReviewerSkipped:
		parts = append(parts, "reviewer skipped")
	case ReviewerFailed:
		parts = append(parts, "reviewer failed")
	case ReviewerRan:
		parts = append(parts, fmt.Sprintf("%d touched · %s · %s", len(r.Slice.Touched),
			count(len(r.Slice.Misses), "miss", "misses"), count(len(r.Slice.Excess), "excess", "excess")))
	}
	return strings.Join(parts, " · ")
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
	writeReviewer(&b, r)
	_, err := io.WriteString(w, b.String())
	return err
}

func writeReviewer(b *strings.Builder, r Result) {
	rv := r.Reviewer
	switch rv.Status {
	case ReviewerSkipped:
		fmt.Fprintf(b, "Reviewer: skipped (%s).\n", rv.Reason)
		return
	case ReviewerFailed:
		fmt.Fprintf(b, "Reviewer: failed (%s).\n", rv.Reason)
		return
	case ReviewerRan:
	default:
		b.WriteString("Reviewer: not recorded.\n")
		return
	}
	fmt.Fprintf(b, "Reviewer: %s", rv.Agent)
	if rv.Model != "" {
		fmt.Fprintf(b, ", %s", rv.Model)
	}
	if rv.Tokens != nil {
		fmt.Fprintf(b, ", %s", rv.Tokens)
	}
	b.WriteString(".\n\n")
	fmt.Fprintf(b, "Touched: %s\n\nMisses: %s\n\nExcess: %s\n\n", list(r.Slice.Touched), list(r.Slice.Misses), list(r.Slice.Excess))
	rv2 := rv.Review
	if rv2 == nil {
		return
	}
	if len(rv2.Rules) > 0 {
		b.WriteString("Rules at stake:\n\n| Capability | Rule | Respected | Why |\n| --- | --- | --- | --- |\n")
		for _, x := range rv2.Rules {
			fmt.Fprintf(b, "| %s | %s | %s | %s |\n", cell(x.Capability), cell(x.Rule), cell(x.Respected), cell(x.Why))
		}
		b.WriteString("\n")
	}
	if len(rv2.Contradictions) > 0 {
		b.WriteString("Contradictions with decisions:\n\n")
		for _, x := range rv2.Contradictions {
			fmt.Fprintf(b, "- %s, %s: %s\n", x.Capability, x.Decision, oneLine(x.Why))
		}
		b.WriteString("\n")
	}
	if len(rv2.Edits) > 0 {
		b.WriteString("Suggested map edits:\n\n")
		for _, x := range rv2.Edits {
			fmt.Fprintf(b, "- %s, %s: %s\n", x.Capability, x.Section, oneLine(x.Why))
		}
		b.WriteString("\n")
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

const fence = "```"

// CheckRunOutput builds the output of the physa check run: the summary is the
// markdown, the text is the result as JSON in a fenced block.
//
// When the review makes either field too big for GitHub, the output is rebuilt
// once with the reviewer failed and the reason saying so.
func CheckRunOutput(r Result) (Output, error) {
	o, err := checkRunOutput(r)
	var big *tooBigError
	if !errors.As(err, &big) || r.Reviewer.Status != ReviewerRan {
		return o, err
	}
	r.Reviewer = ReviewerResult{
		Status: ReviewerFailed,
		Reason: fmt.Sprintf("the review is %d bytes; the check run holds %d", big.size, MaxFieldBytes),
		Agent:  r.Reviewer.Agent, Model: r.Reviewer.Model, Tokens: r.Reviewer.Tokens,
	}
	r.Slice = SliceResult{}
	return checkRunOutput(r)
}

type tooBigError struct {
	field string
	size  int
}

func (e *tooBigError) Error() string {
	return fmt.Sprintf("check run %s is %d bytes; GitHub allows %d", e.field, e.size, MaxFieldBytes)
}

func checkRunOutput(r Result) (Output, error) {
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
		return Output{}, &tooBigError{"summary", len(o.Summary)}
	}
	if len(o.Text) > MaxFieldBytes {
		return Output{}, &tooBigError{"text", len(o.Text)}
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
	if r.Version < oldestVersion || r.Version > Version {
		return Result{}, fmt.Errorf("check run result is version %d; this physa reads versions %d and %d", r.Version, oldestVersion, Version)
	}
	return r, nil
}
