package gather

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
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
	p("Docs: %d files", s.Docs.Count)
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
		p("  %s (%d files)", d, groups[d])
	}
	for _, f := range s.Docs.Skipped {
		p("  skipped %s (over %d KB)", f, MaxDocBytes/1024)
	}
	h := s.History
	p("History: %d changes, %d with trailers, %d reverts, %d with altered files, %d distinct files.",
		h.Changes, h.WithTrailers, h.Reverts, h.WithAltered, h.DistinctFiles)
	if r := s.PRs; r.Available {
		p("PRs: %d, %d review comments (%d requesting changes), %d linked issues.", r.Count, r.ReviewComments, r.RequestingChanges, r.LinkedIssues)
	} else {
		p("PRs: unavailable (%s)", r.Reason)
	}
	p("About %d tokens to read it all (an estimate: bytes / 4).", s.EstimatedTokens)
	return err
}
