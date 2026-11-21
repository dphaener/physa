// Package fit cuts a gather to the proposer's input budget, deterministically,
// and says exactly what it cut. Sources are fitted in a fixed order, each
// within its share of the budget, and what one leaves unused passes to the
// next: docs (long ones cut by section, headings kept), then PRs (titles,
// then bodies, then review comments, those requesting changes first), then
// commit subjects, then clusters of files that change together, as hints.
// Tokens are estimated as bytes / 4. Fit does no I/O and calls no agent.
package fit

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/store"
)

// Input is the proposer's input: the kept sources, already cut, and the
// report of what was cut.
type Input struct {
	Docs     []Doc
	PRs      []PR
	Commits  []Commit
	Clusters []Cluster
	Report   Report
}

// Doc is a kept doc. Body is cut by section when Cut is true.
type Doc struct {
	Path string
	Kind string
	Body string
	Cut  bool
}

// PR is a kept PR: its number, title, merge date and linked issue titles,
// its body (cut, or empty when it did not fit) and the comments kept.
type PR struct {
	Number   int
	Title    string
	MergedAt time.Time
	Issues   []string
	Body     string
	Comments []Comment
}

// Comment is a kept review comment, its body cut to CommentTokens.
type Comment struct {
	Author          string
	RequestsChanges bool
	Body            string
}

// Commit is a kept change: its short id and subject.
type Commit struct {
	Short   string
	Subject string
}

// Cluster is a set of files that change together: its first ClusterPaths
// paths, sorted, and how many changes touch two or more of its files.
type Cluster struct {
	Paths   []string
	Changes int
}

// Fit cuts g to budget tokens. The same gather and budget always give the
// same Input.
func Fit(g store.Gather, budget int) Input {
	if budget < 0 {
		budget = 0
	}
	in := Input{Report: Report{Budget: budget}}
	share := func(pct int) int { return budget * pct / 100 }

	left := share(DocsShare)
	var docs SourceReport
	in.Docs, docs = fitDocs(g.Docs, left)
	left = share(PRsShare) + left - docs.KeptTokens

	var titles, bodies, comments SourceReport
	in.PRs, titles, bodies, comments = fitPRs(g.PRs, left)
	left = share(CommitsShare) + left - titles.KeptTokens - bodies.KeptTokens - comments.KeptTokens

	var commits SourceReport
	in.Commits, commits = fitCommits(g.Changes, left)
	left = share(ClustersShare) + left - commits.KeptTokens

	var clusters SourceReport
	in.Clusters, clusters = fitClusters(g.Changes, left)

	in.Report.Sources = []SourceReport{docs, titles, bodies, comments, commits, clusters}
	for _, s := range in.Report.Sources {
		in.Report.Used += s.KeptTokens
	}
	return in
}

func headerTokens(p store.GatheredPR) int {
	n := Tokens(fmt.Sprintf("#%d %s %s", p.Number, p.Title, p.MergedAt.UTC().Format(time.DateOnly)))
	for _, is := range p.Issues {
		n += Tokens(is)
	}
	return n
}

func commentTokens(author, body string) int { return Tokens(author) + Tokens(body) }

// fitPRs keeps PRs newest merge first in three layers, each filling what is
// left: headers, then bodies, then comments, requesting changes first.
func fitPRs(gathered []store.GatheredPR, left int) ([]PR, SourceReport, SourceReport, SourceReport) {
	titles := SourceReport{Name: "pr titles"}
	bodies := SourceReport{Name: "pr bodies"}
	comments := SourceReport{Name: "review comments"}
	prs := append([]store.GatheredPR(nil), gathered...)
	sort.SliceStable(prs, func(i, j int) bool {
		if !prs[i].MergedAt.Equal(prs[j].MergedAt) {
			return prs[i].MergedAt.After(prs[j].MergedAt)
		}
		return prs[i].Number > prs[j].Number
	})
	for _, p := range prs {
		titles.Items++
		titles.Tokens += headerTokens(p)
		if p.Body != "" {
			bodies.Items++
			bodies.Tokens += Tokens(p.Body)
		}
		for _, c := range p.Comments {
			comments.Items++
			comments.Tokens += commentTokens(c.Author, c.Body)
		}
	}

	var kept []PR
	for _, p := range prs {
		n := headerTokens(p)
		if n > left {
			break
		}
		left -= n
		titles.Kept++
		titles.KeptTokens += n
		kept = append(kept, PR{Number: p.Number, Title: p.Title, MergedAt: p.MergedAt, Issues: append([]string{}, p.Issues...)})
	}
	prs = prs[:len(kept)]

	for i, p := range prs {
		if p.Body == "" {
			continue
		}
		body, cut, ok := capText(p.Body, min(PRBodyTokens, left))
		if !ok {
			continue
		}
		n := Tokens(body)
		left -= n
		kept[i].Body = body
		bodies.Kept++
		bodies.KeptTokens += n
		if cut {
			bodies.Partial++
		}
	}

	type pick struct{ pr, c int }
	var order []pick
	for _, wantRC := range []bool{true, false} {
		for i, p := range prs {
			for j, c := range p.Comments {
				if c.RequestsChanges == wantRC {
					order = append(order, pick{i, j})
				}
			}
		}
	}
	chosen := map[pick]Comment{}
	for _, k := range order {
		c := prs[k.pr].Comments[k.c]
		room := min(CommentTokens, left-Tokens(c.Author))
		if room < 0 {
			continue
		}
		body, cut, ok := capText(c.Body, room)
		if !ok {
			continue
		}
		n := commentTokens(c.Author, body)
		left -= n
		chosen[k] = Comment{Author: c.Author, RequestsChanges: c.RequestsChanges, Body: body}
		comments.Kept++
		comments.KeptTokens += n
		if cut {
			comments.Partial++
		}
	}
	for i, p := range prs {
		for j := range p.Comments {
			if c, ok := chosen[pick{i, j}]; ok {
				kept[i].Comments = append(kept[i].Comments, c)
			}
		}
	}
	return kept, titles, bodies, comments
}

// fitCommits keeps short ids and subjects, newest first.
func fitCommits(changes []store.GatheredChange, left int) ([]Commit, SourceReport) {
	r := SourceReport{Name: "commits"}
	cs := append([]store.GatheredChange(nil), changes...)
	sort.SliceStable(cs, func(i, j int) bool {
		if !cs[i].At.Equal(cs[j].At) {
			return cs[i].At.After(cs[j].At)
		}
		return cs[i].ID < cs[j].ID
	})
	var kept []Commit
	full := true
	for _, c := range cs {
		n := Tokens(c.Short + " " + c.Subject)
		r.Items++
		r.Tokens += n
		if !full || n > left {
			full = false
			continue
		}
		left -= n
		r.Kept++
		r.KeptTokens += n
		kept = append(kept, Commit{Short: c.Short, Subject: c.Subject})
	}
	return kept, r
}

func marker(tokens int) string { return fmt.Sprintf("[… about %d tokens cut]\n", tokens) }

// markerBytes bounds the length of any marker for text of s's size.
func markerBytes(s string) int { return len(marker(Tokens(s) + 1)) }

// cutLines keeps s's first whole lines within limit bytes, marker included,
// and ends them with the marker. It needs len(s) > limit; given limit at
// least markerBytes(s), the result fits. lines is how many lines it kept.
func cutLines(s string, limit int) (out string, lines int) {
	room := limit - markerBytes(s)
	kept := 0
	for kept < len(s) {
		end := strings.IndexByte(s[kept:], '\n')
		if end < 0 || kept+end+1 > room {
			break
		}
		kept += end + 1
		lines++
	}
	return s[:kept] + marker(max(1, (len(s)-kept+3)/4)), lines
}

// capText fits s to tokens, cutting it at a line boundary with the marker.
// ok is false when not even one line fits; cut is true when text was lost.
func capText(s string, tokens int) (out string, cut, ok bool) {
	if Tokens(s) <= tokens {
		return s, false, true
	}
	if tokens <= 0 {
		return "", true, false
	}
	out, lines := cutLines(s, 4*tokens)
	if lines == 0 {
		return "", true, false
	}
	return out, true, true
}
