// Package gather collects the sources init reads, within a bound, and
// summarises what it found: a repo's docs (agent docs, contributing guides,
// readmes, decision records), its history through version control, and its
// merged PRs through the forge when there is one. A forge that is missing,
// cannot list merged PRs or cannot answer makes PRs unavailable, which is
// reported and never a failure. The bound is a date and a most number of
// PRs: when the PR limit is reached first, history starts where the PRs do.
// Gather reads the working tree and never writes to it; the caller keeps the
// result in the store.
package gather

import (
	"fmt"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Version is the summary's JSON version.
const Version = 2

// Summary says what a gather found.
type Summary struct {
	Version         int            `json:"version"`
	Store           string         `json:"store"`
	Bound           BoundSummary   `json:"bound"`
	Docs            DocsSummary    `json:"docs"`
	History         HistorySummary `json:"history"`
	PRs             PRsSummary     `json:"prs"`
	Discovery       Discovery      `json:"discovery"`
	EstimatedTokens int            `json:"estimated_tokens"`
}

// Discovery says what the agent's search for more sources did. Why says why
// it was not started or added nothing, and is empty when it added sources.
// Cap is the most tokens it may use.
type Discovery struct {
	Started bool           `json:"started"`
	Why     string         `json:"why,omitempty"`
	Named   int            `json:"named"`
	Added   int            `json:"added"`
	Model   string         `json:"model,omitempty"`
	Tokens  adapter.Tokens `json:"tokens"`
	Cap     int64          `json:"cap"`
}

// BoundSummary is the bound used. Since is a date; FromFlag is false when it
// is the 12-month default. PRLimitReached is true when the PR limit stopped
// the PRs before the date did, and HistorySince is then the date history
// starts at.
type BoundSummary struct {
	Since          string `json:"since"`
	FromFlag       bool   `json:"from_flag"`
	MaxPRs         int    `json:"max_prs"`
	PRLimitReached bool   `json:"pr_limit_reached"`
	HistorySince   string `json:"history_since,omitempty"`
}

// DocFile is one doc found.
type DocFile struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// DocsSummary counts the docs.
type DocsSummary struct {
	Count   int       `json:"count"`
	Bytes   int       `json:"bytes"`
	Files   []DocFile `json:"files"`
	Skipped []Skip    `json:"skipped"`
}

// HistorySummary counts the changes.
type HistorySummary struct {
	Changes       int `json:"changes"`
	WithTrailers  int `json:"with_trailers"`
	Reverts       int `json:"reverts"`
	WithAltered   int `json:"with_altered_files"`
	DistinctFiles int `json:"distinct_files"`
	Bytes         int `json:"bytes"`
}

// PRsSummary counts the PRs, or says why there are none to count.
type PRsSummary struct {
	Available         bool   `json:"available"`
	Reason            string `json:"reason,omitempty"`
	Count             int    `json:"count"`
	ReviewComments    int    `json:"review_comments"`
	RequestingChanges int    `json:"requesting_changes"`
	LinkedIssues      int    `json:"linked_issues"`
	Bytes             int    `json:"bytes"`
}

// DefaultMaxPRs is the most PRs gathered unless the bound says otherwise.
const DefaultMaxPRs = 1000

// Bounds is the bound of one gather: Since is its start, SinceSet is true
// when the caller gave it rather than defaulting it, and MaxPRs is the most
// merged PRs to list.
type Bounds struct {
	Since    time.Time
	SinceSet bool
	MaxPRs   int
}

// Run gathers the docs under root, the history of repo and, through f, the
// merged PRs, within the bound o sets, as of now. It returns the gather for
// the caller to store and its summary, whose Store is left for the caller to
// set. f may be nil. d asks for the agent's search for more sources, whose
// finds join the docs; it never makes Run fail.
func Run(root string, repo vcs.Repo, f forge.Forge, o Bounds, d Discover, now time.Time) (store.Gather, Summary, error) {
	docs, skipped, err := Docs(root)
	if err != nil {
		return store.Gather{}, Summary{}, fmt.Errorf("cannot read the docs: %w", err)
	}
	added, more, info := discover(root, repo, d, docs)
	docs = append(docs, added...)
	skipped = append(skipped, more...)
	g := store.Gather{Docs: docs, Run: store.GatherRun{At: now, Since: o.Since, MaxPRs: o.MaxPRs}}
	s := Summary{Version: Version, Bound: BoundSummary{
		Since: o.Since.UTC().Format(time.DateOnly), FromFlag: o.SinceSet, MaxPRs: o.MaxPRs,
	}}
	s.Discovery = info
	s.Docs = DocsSummary{Count: len(docs), Files: []DocFile{}, Skipped: skipped}
	if skipped == nil {
		s.Docs.Skipped = []Skip{}
	}
	for _, d := range docs {
		s.Docs.Bytes += len(d.Body)
		s.Docs.Files = append(s.Docs.Files, DocFile{Path: d.Path, Kind: d.Kind, Reason: d.Reason})
	}

	prs, limited, reason := readPRs(f, o)
	if f != nil {
		g.Run.Forge = f.Name()
	}
	g.Run.ForgeReason = reason
	g.PRs = prs
	histSince := o.Since
	if limited && len(prs) > 0 {
		oldest := prs[0].MergedAt
		for _, p := range prs {
			if p.MergedAt.Before(oldest) {
				oldest = p.MergedAt
			}
		}
		histSince = oldest
		g.Run.PRsLimited = true
		s.Bound.PRLimitReached = true
		s.Bound.HistorySince = oldest.UTC().Format(time.DateOnly)
	}
	s.PRs = summarisePRs(prs, reason)

	g.Changes, err = readHistory(repo, histSince)
	if err != nil {
		return store.Gather{}, Summary{}, err
	}
	s.History = summariseHistory(g.Changes)
	s.EstimatedTokens = reading.BytesTokens(s.Docs.Bytes + s.History.Bytes + s.PRs.Bytes)
	return g, s, nil
}

// readPRs lists the merged PRs within the bound. reason is empty when the
// forge answered, and otherwise says why PRs are unavailable.
func readPRs(f forge.Forge, o Bounds) (prs []store.GatheredPR, more bool, reason string) {
	if f == nil {
		return nil, false, "no forge"
	}
	m, ok := f.(forge.Merged)
	if !ok {
		return nil, false, "the forge cannot list merged PRs"
	}
	listed, more, err := m.MergedPRs(o.Since, o.MaxPRs)
	if err != nil {
		return nil, false, forge.Reason(err)
	}
	for _, p := range listed {
		g := store.GatheredPR{Number: p.Number, Title: p.Title, Body: p.Body, Author: p.Author, MergedAt: p.MergedAt, Issues: []string{}}
		for _, is := range p.Issues {
			text := fmt.Sprintf("#%d %s", is.Number, is.Title)
			if is.Body != "" {
				text += "\n\n" + is.Body
			}
			g.Issues = append(g.Issues, text)
		}
		for _, c := range p.Comments {
			g.Comments = append(g.Comments, store.GatheredComment{
				Author: c.Author, At: c.At, Kind: c.Kind, RequestsChanges: c.RequestsChanges, Body: c.Body,
			})
		}
		prs = append(prs, g)
	}
	return prs, more, ""
}

func summarisePRs(prs []store.GatheredPR, reason string) PRsSummary {
	s := PRsSummary{Available: reason == "", Reason: reason, Count: len(prs)}
	for _, p := range prs {
		s.Bytes += len(p.Title) + len(p.Body)
		s.LinkedIssues += len(p.Issues)
		for _, is := range p.Issues {
			s.Bytes += len(is)
		}
		for _, c := range p.Comments {
			s.ReviewComments++
			s.Bytes += len(c.Body)
			if c.RequestsChanges {
				s.RequestingChanges++
			}
		}
	}
	return s
}

func summariseHistory(changes []store.GatheredChange) HistorySummary {
	s := HistorySummary{Changes: len(changes)}
	files := map[string]bool{}
	for _, c := range changes {
		s.Bytes += len(c.Message)
		if len(c.Trailers) > 0 {
			s.WithTrailers++
		}
		if c.Reverts != "" {
			s.Reverts++
		}
		if len(c.Files) > 0 {
			s.WithAltered++
		}
		for _, p := range c.Files {
			files[p] = true
			s.Bytes += len(p)
		}
	}
	s.DistinctFiles = len(files)
	return s
}
