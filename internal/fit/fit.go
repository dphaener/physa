// Package fit cuts a gather to a session's input budget, deterministically,
// and says exactly what it cut. Sources are fitted in a fixed order, each
// within its share of the budget, and what one leaves unused passes to the
// next: docs (long ones cut by section, headings kept), then PRs (titles,
// then bodies, then review comments, those requesting changes first, with a
// share of the PRs' budget kept for comments), then changes, then clusters
// of files that change together, as hints. Tokens are estimated at one
// measured figure, BytesPerTokenTenths, and each item is charged its own text
// plus what the prompt writes around it. Limits set the budget, the shares
// and the caps; Propose is propose's. Fit does no I/O and calls no agent.
package fit

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/store"
)

// Input is the session's input: the kept sources, already cut, and the
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

// PR is a kept PR: its number, title, author, merge date and linked issue
// titles, its body (cut, or empty when it did not fit) and the comments kept.
type PR struct {
	Number   int
	Title    string
	Author   string
	MergedAt time.Time
	Issues   []string
	Body     string
	Comments []Comment
}

// Comment is a kept review comment, its body cut to the CommentTokens limit.
type Comment struct {
	Author          string
	At              time.Time
	RequestsChanges bool
	Body            string
}

// Commit is a kept change: its short id and subject and, when the limits
// keep messages, its message without the subject line (cut), author, time
// and the change it reverts.
type Commit struct {
	Short   string
	Subject string
	Message string
	Author  string
	At      time.Time
	Reverts string
}

// Cluster is a set of files that change together: its first ClusterPaths
// paths, sorted, and how many changes touch two or more of its files.
type Cluster struct {
	Paths   []string
	Changes int
}

// Fit is FitWith under the Propose limits.
func Fit(g store.Gather, budget int) Input { return FitWith(g, budget, Propose) }

// FitWith cuts g to budget tokens under l. The same gather, budget and
// limits always give the same Input.
func FitWith(g store.Gather, budget int, l Limits) Input {
	if budget < 0 {
		budget = 0
	}
	in := Input{Report: Report{Budget: budget}}
	share := func(pct int) int { return budget * pct / 100 }

	left := share(l.DocsShare)
	var docs SourceReport
	in.Docs, docs = fitDocs(g.Docs, left)
	left = share(l.PRsShare) + left - docs.KeptTokens

	var titles, bodies, comments SourceReport
	in.PRs, titles, bodies, comments = fitPRs(g.PRs, left, l)
	left = share(l.CommitsShare) + left - titles.KeptTokens - bodies.KeptTokens - comments.KeptTokens

	var commits SourceReport
	in.Commits, commits = fitCommits(g.Changes, left, l.CommitMessageTokens)
	left = share(l.ClustersShare) + left - commits.KeptTokens

	var clusters SourceReport
	in.Clusters, clusters = fitClusters(g.Changes, left)

	in.Report.Sources = []SourceReport{docs, titles, bodies, comments, commits, clusters}
	for _, s := range in.Report.Sources {
		in.Report.Used += s.KeptTokens
	}
	return in
}

func headerTokens(p store.GatheredPR) int {
	n := prWrap + len(strconv.Itoa(p.Number)) + len(p.Title)
	if len(p.Issues) > 0 {
		n += issuesWrap
	}
	for _, is := range p.Issues {
		n += issueWrap + len(is)
	}
	return BytesTokens(n)
}

var bodyWrapTokens = BytesTokens(bodyWrap)

// commentWrapTokens is what a comment on PR pr costs beside its body.
func commentWrapTokens(pr int, c store.GatheredComment) int {
	n := commentWrap + len(strconv.Itoa(pr)) + len(c.Author)
	if c.RequestsChanges {
		n += rcWrap
	}
	return BytesTokens(n)
}

// fitPRs keeps PRs newest merge first in three layers, each filling what is
// left: headers, then bodies, then comments, requesting changes first.
// Headers and bodies may not spend the CommentsShare of left.
func fitPRs(gathered []store.GatheredPR, left int, l Limits) ([]PR, SourceReport, SourceReport, SourceReport) {
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
			bodies.Tokens += Tokens(p.Body) + bodyWrapTokens
		}
		for _, c := range p.Comments {
			comments.Items++
			comments.Tokens += commentWrapTokens(p.Number, c) + Tokens(c.Body)
		}
	}

	reserve := max(left, 0) * l.CommentsShare / 100
	left -= reserve
	var kept []PR
	for _, p := range prs {
		n := headerTokens(p)
		if n > left {
			break
		}
		left -= n
		titles.Kept++
		titles.KeptTokens += n
		kept = append(kept, PR{Number: p.Number, Title: p.Title, Author: p.Author, MergedAt: p.MergedAt, Issues: append([]string{}, p.Issues...)})
	}
	prs = prs[:len(kept)]

	for i, p := range prs {
		if p.Body == "" {
			continue
		}
		body, cut, ok := capText(p.Body, min(l.PRBodyTokens, left-bodyWrapTokens))
		if !ok {
			continue
		}
		n := Tokens(body) + bodyWrapTokens
		left -= n
		kept[i].Body = body
		bodies.Kept++
		bodies.KeptTokens += n
		if cut {
			bodies.Partial++
		}
	}
	left += reserve

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
		wrap := commentWrapTokens(prs[k.pr].Number, c)
		body, cut, ok := capText(c.Body, min(l.CommentTokens, left-wrap))
		if !ok {
			continue
		}
		n := wrap + Tokens(body)
		left -= n
		chosen[k] = Comment{Author: c.Author, At: c.At, RequestsChanges: c.RequestsChanges, Body: body}
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

// commit is c as kept, with what it costs and what it would cost uncut. With
// messageTokens zero it keeps the short id and subject only.
func commit(c store.GatheredChange, messageTokens int) (Commit, int, int) {
	k := Commit{Short: c.Short, Subject: c.Subject}
	n := BytesTokens(len(c.Short) + len(c.Subject) + changeWrap)
	if messageTokens <= 0 {
		return k, n, n
	}
	msg := ""
	if i := strings.IndexByte(c.Message, '\n'); i >= 0 {
		msg = strings.TrimLeft(c.Message[i+1:], "\n")
	}
	n += BytesTokens(len(c.Author) + len(c.Reverts) + detailWrap)
	full := n + Tokens(msg)
	msg, _, _ = capText(msg, messageTokens)
	k.Message, k.Author, k.At, k.Reverts = msg, c.Author, c.At, c.Reverts
	return k, n + Tokens(msg), full
}

// fitCommits keeps changes newest first, each whole or not at all, until
// one does not fit.
func fitCommits(changes []store.GatheredChange, left, messageTokens int) ([]Commit, SourceReport) {
	r := SourceReport{Name: "commits"}
	cs := append([]store.GatheredChange(nil), changes...)
	sort.SliceStable(cs, func(i, j int) bool {
		if !cs[i].At.Equal(cs[j].At) {
			return cs[i].At.After(cs[j].At)
		}
		return cs[i].ID < cs[j].ID
	})
	var kept []Commit
	fits := true
	for _, c := range cs {
		k, n, full := commit(c, messageTokens)
		r.Items++
		r.Tokens += full
		if !fits || n > left {
			fits = false
			continue
		}
		left -= n
		r.Kept++
		r.KeptTokens += n
		if n < full {
			r.Partial++
		}
		kept = append(kept, k)
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
	return s[:kept] + marker(max(1, Tokens(s[kept:]))), lines
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
	out, lines := cutLines(s, tokenBytes(tokens))
	if lines == 0 {
		return "", true, false
	}
	return out, true, true
}
