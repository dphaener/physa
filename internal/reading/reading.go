// Package reading shapes a gather into what one session reads, whole and
// deterministically, and estimates its tokens. Nothing a session is given is
// ever cut: every doc, PR title, PR body, review comment and change it reads
// reaches it whole (the owner, 2026-11-23: "This process is important,
// nothing should get cut."). Options choose what a session reads, never how
// much of it: propose reads change subjects and clusters of files that
// change together, as hints; recovery reads change messages and no clusters.
// The appetite, not this package, decides whether a session starts. Tokens
// are estimated at one measured figure, BytesPerTokenTenths. Reading does no
// I/O and calls no agent.
package reading

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/store"
)

// Options say what a session reads of a gather.
type Options struct {
	// Messages keeps each change's message (without its subject line),
	// author, time and the change it reverts; without it a change is its
	// short id and subject.
	Messages bool
	// Clusters adds clusters of files that change together, as hints.
	Clusters bool
}

// Propose is what init's propose phase reads: change subjects, and clusters.
var Propose = Options{Clusters: true}

// Input is what a session reads, in order, and the report of it.
type Input struct {
	Docs     []Doc
	PRs      []PR
	Commits  []Commit
	Clusters []Cluster
	Report   Report
}

// Doc is a doc, whole.
type Doc struct {
	Path string
	Kind string
	Body string
}

// PR is a PR, whole: its number, title, author, merge date, linked issue
// titles, body and review comments.
type PR struct {
	Number   int
	Title    string
	Author   string
	MergedAt time.Time
	Issues   []string
	Body     string
	Comments []Comment
}

// Comment is a review comment, whole.
type Comment struct {
	Author          string
	At              time.Time
	RequestsChanges bool
	Body            string
}

// Commit is a change: its short id and subject and, when Options.Messages,
// its whole message without the subject line, author, time and the change
// it reverts.
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

// kindRank orders docs: agent docs, contributing guides, readmes, overviews,
// decisions, then anything else; within a kind, by path.
var kindRank = map[string]int{"agent": 0, "contributing": 1, "readme": 2, "overview": 3, "decisions": 4}

func rank(kind string) int {
	if r, ok := kindRank[kind]; ok {
		return r
	}
	return len(kindRank)
}

// Of is all of g as o reads it: docs by kind, then path; PRs newest merge
// first; changes newest first; then, with o.Clusters, the clusters. The same
// gather and options always give the same Input.
func Of(g store.Gather, o Options) Input {
	var in Input
	docs := SourceReport{Name: "docs"}
	ds := append([]store.Doc(nil), g.Docs...)
	sort.SliceStable(ds, func(i, j int) bool {
		if ri, rj := rank(ds[i].Kind), rank(ds[j].Kind); ri != rj {
			return ri < rj
		}
		return ds[i].Path < ds[j].Path
	})
	for _, d := range ds {
		in.Docs = append(in.Docs, Doc{Path: d.Path, Kind: d.Kind, Body: d.Body})
		docs.add(len(d.Path) + len(d.Kind) + len(d.Body))
	}

	prs := SourceReport{Name: "pull requests"}
	comments := SourceReport{Name: "review comments"}
	ps := append([]store.GatheredPR(nil), g.PRs...)
	sort.SliceStable(ps, func(i, j int) bool {
		if !ps[i].MergedAt.Equal(ps[j].MergedAt) {
			return ps[i].MergedAt.After(ps[j].MergedAt)
		}
		return ps[i].Number > ps[j].Number
	})
	for _, p := range ps {
		pr := PR{Number: p.Number, Title: p.Title, Author: p.Author, MergedAt: p.MergedAt, Issues: append([]string{}, p.Issues...), Body: p.Body}
		n := len(p.Title) + len(p.Author) + len(p.Body)
		for _, is := range p.Issues {
			n += len(is)
		}
		prs.add(n)
		for _, c := range p.Comments {
			pr.Comments = append(pr.Comments, Comment{Author: c.Author, At: c.At, RequestsChanges: c.RequestsChanges, Body: c.Body})
			comments.add(len(c.Author) + len(c.Body))
		}
		in.PRs = append(in.PRs, pr)
	}

	changes := SourceReport{Name: "changes"}
	cs := append([]store.GatheredChange(nil), g.Changes...)
	sort.SliceStable(cs, func(i, j int) bool {
		if !cs[i].At.Equal(cs[j].At) {
			return cs[i].At.After(cs[j].At)
		}
		return cs[i].ID < cs[j].ID
	})
	for _, c := range cs {
		k := Commit{Short: c.Short, Subject: c.Subject}
		if o.Messages {
			if i := strings.IndexByte(c.Message, '\n'); i >= 0 {
				k.Message = strings.TrimLeft(c.Message[i+1:], "\n")
			}
			k.Author, k.At, k.Reverts = c.Author, c.At, c.Reverts
		}
		in.Commits = append(in.Commits, k)
		changes.add(len(k.Short) + len(k.Subject) + len(k.Message) + len(k.Author) + len(k.Reverts))
	}
	in.Report.Sources = []SourceReport{docs, prs, comments, changes}

	if o.Clusters {
		clustered := SourceReport{Name: "clusters"}
		for _, c := range clusters(g.Changes) {
			in.Clusters = append(in.Clusters, c)
			n := 0
			for _, p := range c.Paths {
				n += len(p)
			}
			clustered.add(n)
		}
		in.Report.Sources = append(in.Report.Sources, clustered)
	}
	for i := range in.Report.Sources {
		s := &in.Report.Sources[i]
		s.Tokens = BytesTokens(s.bytes)
		in.Report.Tokens += s.Tokens
	}
	return in
}

// Linked is the docs a capability's session reads: the agent docs, readme
// and contributing guide always, the others only when linked names them.
func Linked(docs []store.Doc, linked []string) (read []store.Doc) {
	for _, d := range docs {
		if d.Kind == "agent" || d.Kind == "readme" || d.Kind == "contributing" || slices.Contains(linked, d.Path) {
			read = append(read, d)
		}
	}
	return read
}
