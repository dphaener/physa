// Package quiet reaches into the areas of a repo that the gathered PRs
// miss. When the PR limit stops the PRs before the date bound does, history
// starts where the PRs do, so a long-lived area with little recent work has
// no PR or change gathered and comes out thin or folded into a neighbour.
// For each cluster of files that change together in the history between
// the date bound and the PRs' start, and that no later change touches,
// Reach finds the PRs of its newest few changes through the forge and gives
// them, with those changes, to the gather. It reads the forge and never
// writes to it.
package quiet

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

// PerCluster is the most PRs Reach gathers for one quiet cluster. A first
// guess, to be tuned from real runs (measure-before-budget).
const PerCluster = 3

// Summary says what Reach did: how many clusters were quiet, how many PRs
// it gathered for them, and why it stopped early, when it did.
type Summary struct {
	Clusters int    `json:"clusters"`
	PRs      int    `json:"prs"`
	Why      string `json:"why,omitempty"`
}

// PR is the gathered form of a forge's PR.
func PR(p forge.PR) store.GatheredPR {
	g := store.GatheredPR{Number: p.Number, Title: p.Title, Body: p.Body, Author: p.Author, MergedBy: p.MergedBy, MergedAt: p.MergedAt, Issues: []string{}}
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
	return g
}

// squash is the PR number at the end of a squash subject, "Title (#12)".
var squash = regexp.MustCompile(`\s*\(#\d+\)\s*$`)

// Reach returns the PRs and changes to add to a gather whose PRs reach back
// only as far as recent's changes: for each cluster of older's changes
// (reading.Clusters) none of whose files a change of recent touches, the PRs
// of its PerCluster newest changes in older that touch it, with those
// changes, skipping PRs have holds. older is newest first. A PR the forge
// gives without a title or a merge date takes its change's subject and
// date. A forge that cannot answer stops Reach, and the Summary says why;
// what was found before is kept.
func Reach(recent, older []store.GatheredChange, f forge.Forge, have map[int]bool) ([]store.GatheredPR, []store.GatheredChange, Summary) {
	var sum Summary
	touched := map[string]bool{}
	for _, c := range recent {
		for _, p := range c.Files {
			touched[p] = true
		}
	}
	var prs []store.GatheredPR
	var changes []store.GatheredChange
	seen := map[int]bool{}
	for n := range have {
		seen[n] = true
	}
	for _, cl := range reading.Clusters(older) {
		if f == nil || anyIn(cl.Paths, touched) {
			continue
		}
		sum.Clusters++
		paths, got := set(cl.Paths), 0
		for _, c := range older {
			if got == PerCluster {
				break
			}
			if !anyIn(c.Files, paths) {
				continue
			}
			p, ok, err := f.ChangePR(c.ID)
			if err != nil {
				sum.Why = forge.Reason(err)
				sum.PRs = len(prs)
				return prs, changes, sum
			}
			if !ok || seen[p.Number] {
				continue
			}
			seen[p.Number], got = true, got+1
			g := PR(p)
			if g.Title == "" {
				g.Title = squash.ReplaceAllString(strings.TrimSpace(c.Subject), "")
			}
			if g.MergedAt.IsZero() {
				g.MergedAt = c.At
			}
			prs, changes = append(prs, g), append(changes, c)
		}
	}
	sum.PRs = len(prs)
	return prs, changes, sum
}

// Add keeps the changes of g since histSince, where its PRs start, and
// adds to g what Reach finds in the older ones. It returns what Reach did.
func Add(g *store.Gather, histSince time.Time, f forge.Forge) *Summary {
	var recent, older []store.GatheredChange
	for _, c := range g.Changes {
		if c.At.Before(histSince) {
			older = append(older, c)
		} else {
			recent = append(recent, c)
		}
	}
	have := map[int]bool{}
	for _, p := range g.PRs {
		have[p.Number] = true
	}
	prs, changes, sum := Reach(recent, older, f, have)
	g.PRs, g.Changes = append(g.PRs, prs...), append(recent, changes...)
	return &sum
}

// anyIn reports whether any of paths is in s.
func anyIn(paths []string, s map[string]bool) bool {
	for _, p := range paths {
		if s[p] {
			return true
		}
	}
	return false
}

func set(paths []string) map[string]bool {
	s := make(map[string]bool, len(paths))
	for _, p := range paths {
		s[p] = true
	}
	return s
}
