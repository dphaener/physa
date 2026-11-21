package gather

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"

	"github.com/dphaener/physa/internal/fit"
)

// WriteJSON writes the summary as indented JSON.
func WriteJSON(w io.Writer, s Summary) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// WriteText writes the summary for a person.
func WriteText(w io.Writer, s Summary) error {
	var err error
	p := func(format string, args ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format+"\n", args...)
		}
	}
	p("Gathered into %s, replacing the previous gather.", s.Store)
	from := "the last 12 months"
	if s.Bound.FromFlag {
		from = "from --since"
	}
	p("Bound: since %s (%s), at most %d PRs.", s.Bound.Since, from, s.Bound.MaxPRs)
	if s.Bound.PRLimitReached {
		p("The PR limit was reached first; history starts at %s.", s.Bound.HistorySince)
	}
	p("Docs: %s", plural(s.Docs.Count, "file", "files"))
	groups := map[string]int{}
	for _, f := range s.Docs.Files {
		if d := path.Dir(f.Path); f.Kind == KindDecisions && d != "." && d != "docs" && d != "doc" {
			groups[d+"/"]++
			continue
		}
		p("  %s (%s)", f.Path, f.Kind)
	}
	dirs := make([]string, 0, len(groups))
	for d := range groups {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		p("  %s (%s)", d, plural(groups[d], "file", "files"))
	}
	for _, f := range s.Docs.Skipped {
		p("  skipped %s (over %d KB)", f, MaxDocBytes/1024)
	}
	h := s.History
	p("History: %s, %d with trailers, %s, %d with altered files, %s.",
		plural(h.Changes, "change", "changes"), h.WithTrailers, plural(h.Reverts, "revert", "reverts"),
		h.WithAltered, plural(h.DistinctFiles, "distinct file", "distinct files"))
	if r := s.PRs; r.Available {
		p("PRs: %d, %s (%d requesting changes), %s.", r.Count,
			plural(r.ReviewComments, "review comment", "review comments"), r.RequestingChanges,
			plural(r.LinkedIssues, "linked issue", "linked issues"))
	} else {
		p("PRs: unavailable (%s)", r.Reason)
	}
	p("About %s to read it all (an estimate: bytes / %s).", plural(s.EstimatedTokens, "token", "tokens"), fit.Figure())
	return err
}

// plural words n with one or many.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
