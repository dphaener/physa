// Package prcheck runs the PR check of design 07 section 5 on a range: every
// change names exactly one capability whose slug is in the map as that change
// leaves it, and the map passes physa check map. It holds the result, each
// commit with the capability it names, the trailer findings and the map's
// problems, and renders it as a short markdown summary. It reports; whether a
// finding blocks is the caller's policy.
package prcheck

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/history"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/vcs"
)

// Version is the version of the result's JSON shape.
const Version = 3

// How the map's PR anchors were checked.
const (
	PRAnchorsGitHub   = mapcheck.PRAnchorsGitHub
	PRAnchorsFormOnly = mapcheck.PRAnchorsFormOnly
)

// Result is what the deterministic PR check found.
type Result struct {
	Version  int               `json:"version"`
	Range    string            `json:"range"`
	Commits  []history.Commit  `json:"commits"`
	Findings []history.Finding `json:"findings"`
	Map      MapResult         `json:"map"`
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

// Run checks the range rng of repo against the map under dir, the repo's
// working directory: the commits' trailers against the map as each commit
// leaves it, and the map's own check, with commit anchors resolved in repo and
// #N anchors through prExists (nil checks them for form only). The error is
// for a map, a history or a range that cannot be read.
func Run(dir string, repo vcs.Repo, rng string, prExists func(n int) (bool, error)) (Result, error) {
	res, err := mapcheck.CheckWith(dir, mapcheck.Options{
		PRExists: prExists,
		OpenRepo: func(string) (vcs.Repo, error) { return repo, nil },
	})
	if err != nil {
		return Result{}, err
	}
	commits, err := history.Read(repo, rng)
	if err != nil {
		return Result{}, err
	}
	findings, err := history.Check(repo, commits)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Version:  Version,
		Range:    rng,
		Commits:  commits,
		Findings: findings,
		Map: MapResult{
			Problems:  Problems(res.Problems),
			PRAnchors: res.PRAnchors,
			Note:      res.PRNote,
		},
	}, nil
}

// Clean reports whether the check found nothing: no trailer findings and no
// map problems.
func (r Result) Clean() bool {
	return len(r.Findings) == 0 && len(r.Map.Problems) == 0
}

// Problems converts the map reader's problems.
func Problems(ps []mapfile.Problem) []Problem {
	out := make([]Problem, 0, len(ps))
	for _, p := range ps {
		out = append(out, Problem{Path: p.Path, Line: p.Line, Message: p.Message})
	}
	return out
}

// MarshalJSON encodes empty commits, findings and problems as [], never null.
func (r Result) MarshalJSON() ([]byte, error) {
	type plain Result
	p := plain(r)
	if p.Commits == nil {
		p.Commits = []history.Commit{}
	} else {
		p.Commits = append([]history.Commit{}, p.Commits...)
		for i, c := range p.Commits {
			if c.Capabilities == nil {
				p.Commits[i].Capabilities = []string{}
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
		b.WriteString("| Commit | Subject | Capability |\n| --- | --- | --- |\n")
		for _, c := range r.Commits {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", cell(c.Short), cell(c.Subject), list(c.Capabilities))
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
		b.WriteString("The map passes physa check map.\n\n")
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
	_, err := io.WriteString(w, b.String())
	return err
}
