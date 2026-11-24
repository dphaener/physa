package shares

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
)

// merging is the merge session's skill: what it is told before the shares'
// proposals. It reads those and never a source.
//
//go:embed merging.md
var merging string

// MergePrompt returns everything the merge session reads: the merging skill,
// then each share's checked proposal under its own label. What the sessions
// wrote sits in fenced blocks nothing inside can close. It holds no source.
// The same proposals always give the same text.
func MergePrompt(ps []mapping.Proposal) string {
	var w strings.Builder
	w.WriteString(strings.TrimRight(merging, "\n"))
	w.WriteString("\n\n## Proposals\n")
	for i, p := range ps {
		b, err := json.MarshalIndent(p, "", "  ")
		if err != nil { // a Proposal is plain strings and lists
			panic(err)
		}
		fmt.Fprintf(&w, "\nShare %d of %d:\n\n", i+1, len(ps))
		draft.Fence(&w, string(b))
	}
	return w.String()
}

// MergeTokens estimates the merge session before the shares have run, for
// init's estimate: the skill and labels for n shares, and a reply reserve for
// each share's proposal, which is what the merge will read.
func MergeTokens(n int) int {
	return reading.Tokens(MergePrompt(make([]mapping.Proposal, n))) + n*mapping.ReplyReserve
}

// given is every PR, change and doc the shares' proposals named.
type given struct {
	prs, ids, docs map[string]bool
}

func newGiven(ps []mapping.Proposal) given {
	g := given{prs: map[string]bool{}, ids: map[string]bool{}, docs: map[string]bool{}}
	add := func(refs []string) {
		for _, r := range refs {
			if r = strings.TrimSpace(r); strings.HasPrefix(r, "#") {
				g.prs[r] = true
			} else if r != "" {
				g.ids[strings.ToLower(r)] = true
			}
		}
	}
	questions := func(qs []mapping.Question) {
		for _, q := range qs {
			add(q.SuggestedBy)
		}
	}
	for _, p := range ps {
		questions(p.Questions)
		for _, c := range p.Capabilities {
			add(c.ShownBy)
			questions(c.Questions)
			for _, d := range c.Docs {
				g.docs[strings.TrimSpace(d)] = true
			}
		}
	}
	return g
}

// has reports whether a share gave the PR or change ref names. A change id is
// given when it and a share's id are prefixes one of the other.
func (g given) has(ref string) bool {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "#") {
		return g.prs[ref]
	}
	id := strings.ToLower(ref)
	if id == "" {
		return false
	}
	for s := range g.ids {
		if strings.HasPrefix(s, id) || strings.HasPrefix(id, s) {
			return true
		}
	}
	return false
}

// CheckLinks drops from the merge's reply every link no share's proposal
// gave: each PR or change in a capability's shown_by or a question's
// suggested_by, and each doc path in a capability's docs, whether on a
// capability or product-wide. Each drop is a mapping.WhatRef with the reason
// "no share gave it". Order and everything else are kept. The caller then
// checks the reply against the gather with mapping.Check.
func CheckLinks(r mapping.Reply, ps []mapping.Proposal) (mapping.Reply, mapping.Dropped) {
	g := newGiven(ps)
	d := mapping.Dropped{}
	refs := func(list []string, capability string) []string {
		kept := make([]string, 0, len(list))
		for _, ref := range list {
			if g.has(ref) {
				kept = append(kept, ref)
				continue
			}
			d = append(d, mapping.Drop{What: mapping.WhatRef, Capability: capability, Value: ref, Reason: "no share gave it"})
		}
		return kept
	}
	questions := func(qs []mapping.Question, capability string) []mapping.Question {
		if qs == nil {
			return nil
		}
		out := make([]mapping.Question, len(qs))
		for i, q := range qs {
			q.SuggestedBy = refs(q.SuggestedBy, capability)
			out[i] = q
		}
		return out
	}
	out := mapping.Reply{Product: r.Product}
	for _, c := range r.Capabilities {
		c.ShownBy = refs(c.ShownBy, c.Name)
		docs := make([]string, 0, len(c.Docs))
		for _, p := range c.Docs {
			if g.docs[strings.TrimSpace(p)] {
				docs = append(docs, p)
				continue
			}
			d = append(d, mapping.Drop{What: mapping.WhatRef, Capability: c.Name, Value: p, Reason: "no share gave it"})
		}
		c.Docs = docs
		c.Questions = questions(c.Questions, c.Name)
		out.Capabilities = append(out.Capabilities, c)
	}
	out.Questions = questions(r.Questions, "")
	return out, d
}
