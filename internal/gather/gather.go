// Package gather collects the sources init reads, within a bound, and
// summarises what it found: a repo's docs (agent docs, contributing guides,
// readmes, decision records), its history through version control, and its
// merged PRs through the forge when there is one, page by page, waiting out
// the forge's rate limits (internal/gather/merged). A forge that is missing,
// cannot list merged PRs, or is unavailable or unauthenticated before it
// lists any PR makes PRs unavailable, which is reported and never a failure.
// Any other forge error, or one after PRs were listed, fails the gather with
// a Stopped error that still returns the gather with every PR listed. The
// bound is a date and, when given, a most number of PRs: when the PR limit is
// reached first, history starts where the PRs do, and each area the PRs miss
// gets a few PRs of its own (quiet).
// Gather reads the working tree and never writes to it; the caller keeps the
// result in the store.
package gather

import (
	"errors"
	"fmt"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather/merged"
	"github.com/dphaener/physa/internal/quiet"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Version is the summary's JSON version.
const Version = 3

// Summary says what a gather found.
type Summary struct {
	Version         int            `json:"version"`
	Store           string         `json:"store"`
	Bound           BoundSummary   `json:"bound"`
	Docs            DocsSummary    `json:"docs"`
	History         HistorySummary `json:"history"`
	PRs             PRsSummary     `json:"prs"`
	Discovery       Discovery      `json:"discovery"`
	Quiet           *quiet.Summary `json:"quiet,omitempty"`
	EstimatedTokens int            `json:"estimated_tokens"`
}

// Discovery says what the agent's search for more sources did. Why says why
// it was not started or added nothing, and is empty when it added sources.
type Discovery struct {
	Started bool           `json:"started"`
	Why     string         `json:"why,omitempty"`
	Named   int            `json:"named"`
	Added   int            `json:"added"`
	Model   string         `json:"model,omitempty"`
	Tokens  adapter.Tokens `json:"tokens"`
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
	// Shallow is true when the clone is shallow: history is only what was fetched.
	Shallow bool `json:"shallow"`
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

// Bounds is the bound of one gather: Since is its start, SinceSet is true
// when the caller gave it rather than defaulting it, MaxPRs is the most
// merged PRs to list (0 means no limit), and Wait is how the forge's rate
// limits are waited out (zero means its defaults).
type Bounds struct {
	Since    time.Time
	SinceSet bool
	MaxPRs   int
	Wait     merged.Wait
}

// Stopped is the error of a gather the forge stopped, returned with the
// gather holding every PR listed; see merged.Stopped.
type Stopped = merged.Stopped

// Run gathers the docs under root, the history of repo and, through f, the
// merged PRs, within the bound o sets, as of now. It returns the gather for
// the caller to store and its summary, whose Store is left for the caller to
// set. f may be nil. d asks for the agent's search for more sources, whose
// finds join the docs; it never makes Run fail. When the forge stops the
// list, Run still returns the gather and its summary, with every PR listed
// and the history of the whole bound, and a *Stopped error.
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

	listed, limited, reason, stop := merged.Read(f, o.Since, o.MaxPRs, o.Wait)
	if f != nil {
		g.Run.Forge = f.Name()
	}
	g.Run.ForgeReason = reason
	if s := (*Stopped)(nil); errors.As(stop, &s) { // marked in the store, so later phases say so
		g.Run.Stopped, g.Run.Reached, g.Run.StopReason = true, s.Reached, fmt.Sprint(s.Err)
	}
	for _, p := range listed {
		g.PRs = append(g.PRs, quiet.PR(p))
	}
	histSince := o.Since
	if limited && len(listed) > 0 {
		oldest := merged.Oldest(listed)
		histSince = oldest
		g.Run.PRsLimited = true
		s.Bound.PRLimitReached = true
		s.Bound.HistorySince = oldest.UTC().Format(time.DateOnly)
	}
	from := histSince
	if g.Run.PRsLimited {
		from = o.Since // to find the areas the PRs miss (quiet)
	}
	g.Changes, err = readHistory(repo, from)
	if err != nil {
		return store.Gather{}, Summary{}, err
	}
	if g.Run.PRsLimited {
		s.Quiet = quiet.Add(&g, histSince, f)
	}
	s.PRs = summarisePRs(g.PRs, reason)
	useLogins(f, g.Changes)
	s.History = summariseHistory(g.Changes)
	s.History.Shallow = repo.Partial()
	s.EstimatedTokens = reading.GatherTokens(g)
	return g, s, stop
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

// useLogins names each change's author by forge login where the forge can
// say, as PRs are named; otherwise the version-control names stay.
func useLogins(f forge.Forge, changes []store.GatheredChange) {
	l, ok := f.(forge.Logins)
	if !ok || len(changes) == 0 {
		return
	}
	ids := make([]string, len(changes))
	for i, c := range changes {
		ids[i] = c.ID
	}
	if logins, err := l.ChangeLogins(ids); err == nil {
		for i, c := range changes {
			if login := logins[c.ID]; login != "" {
				changes[i].Author = login
			}
		}
	}
}
