package mapping

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/store"
)

var (
	prRefRe = regexp.MustCompile(`^#[1-9][0-9]*$`)
	hexRe   = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// gathered answers whether a ref names something in the gather.
type gathered struct {
	docs map[string]bool
	prs  map[int]bool
	ids  []string // the gathered changes' full ids, lower case
}

func newGathered(g store.Gather) gathered {
	s := gathered{prs: map[int]bool{}, docs: map[string]bool{}}
	for _, d := range g.Docs {
		s.docs[d.Path] = true
	}
	for _, p := range g.PRs {
		s.prs[p.Number] = true
	}
	for _, c := range g.Changes {
		s.ids = append(s.ids, strings.ToLower(c.ID))
	}
	return s
}

// resolve returns ref in the form it is kept in and what it names (a PR's
// number or a change's full id), or the reason it names nothing gathered.
func (s gathered) resolve(ref string) (kept, names, reason string) {
	ref = strings.TrimSpace(ref)
	if prRefRe.MatchString(ref) {
		n, err := strconv.Atoi(ref[1:])
		if err != nil || !s.prs[n] {
			return "", "", "no such PR was gathered"
		}
		return ref, ref, ""
	}
	id := strings.ToLower(ref)
	if !hexRe.MatchString(id) {
		return "", "", "neither a PR (#N) nor a change's id of 7 to 40 hex characters"
	}
	found := ""
	for _, full := range s.ids {
		if !strings.HasPrefix(full, id) {
			continue
		}
		if found != "" {
			return "", "", "the id matches more than one gathered change"
		}
		found = full
	}
	if found == "" {
		return "", "", "no such change was gathered"
	}
	return id, found, ""
}

// refs keeps the refs that name something gathered, each thing once, and
// reports the others as drops on capability.
func (s gathered) refs(list []string, capability string, d *Dropped) []string {
	kept := []string{}
	seen := map[string]bool{}
	for _, ref := range list {
		k, names, reason := s.resolve(ref)
		if reason != "" {
			*d = append(*d, Drop{What: WhatRef, Capability: capability, Value: ref, Reason: reason})
			continue
		}
		if !seen[names] {
			seen[names] = true
			kept = append(kept, k)
		}
	}
	return kept
}

// questions keeps the questions that end with a question mark, each on one
// line, with the refs that name something gathered.
func (s gathered) questions(list []Question, capability string, d *Dropped) []Question {
	kept := []Question{}
	for _, q := range list {
		text := draft.OneLine(q.Question)
		if !strings.HasSuffix(text, "?") {
			*d = append(*d, Drop{What: WhatQuestion, Capability: capability, Value: q.Question, Reason: "a question must end with ?"})
			continue
		}
		kept = append(kept, Question{Question: text, SuggestedBy: s.refs(q.SuggestedBy, capability, d)})
	}
	return kept
}

// promise returns p trimmed, or the reason it is not a single non-empty
// line.
func promise(p string) (string, string) {
	p = strings.TrimSpace(p)
	switch {
	case p == "":
		return "", "the promise is empty"
	case strings.ContainsAny(p, "\n\r"):
		return "", "the promise must be a single line"
	}
	return p, ""
}

// Check holds r against the gather g and returns what stands, with
// everything it took out and why. A capability is dropped when its name is
// not a slug (mapfile.ValidSlug), when an earlier capability has its name, or
// when its promise is not a single non-empty line. A PR or change that was
// not gathered, or a short id that matches more than one gathered change, is
// removed from where it was named; a capability left with nothing that shows
// it is kept, with Unshown set. A question that does not end with a question
// mark is dropped. The product's promise must be a single non-empty line
// too: when it is not, the Proposal's is empty and the drop says so.
func Check(r Reply, g store.Gather) (Proposal, Dropped) {
	s := newGathered(g)
	d := Dropped{}
	p := Proposal{Capabilities: []Capability{}}

	text, reason := promise(r.Product.Promise)
	if reason != "" {
		d = append(d, Drop{What: WhatPromise, Value: r.Product.Promise, Reason: "the product's promise: " + reason})
	}
	p.Product.Promise = text

	names := map[string]bool{}
	for _, c := range r.Capabilities {
		text, reason := promise(c.Promise)
		switch {
		case !mapfile.ValidSlug(c.Name):
			reason = "the name is not lowercase words joined by single hyphens"
		case names[c.Name]:
			reason = "an earlier capability has this name"
		}
		if reason != "" {
			d = append(d, Drop{What: WhatCapability, Value: c.Name, Reason: reason})
			continue
		}
		names[c.Name] = true
		shown := s.refs(c.ShownBy, c.Name, &d)
		var docs []string
		for _, path := range c.Docs {
			if path = strings.TrimSpace(path); s.docs[path] {
				docs = append(docs, path)
			} else {
				d = append(d, Drop{What: WhatRef, Capability: c.Name, Value: path, Reason: "no such doc was gathered"})
			}
		}
		p.Capabilities = append(p.Capabilities, Capability{
			Name: c.Name, Promise: text, ShownBy: shown, Docs: docs,
			Questions: s.questions(c.Questions, c.Name, &d),
			Unshown:   len(shown) == 0,
		})
	}
	p.Questions = s.questions(r.Questions, "", &d)
	return p, d
}
