// Package recoverinput is what each capability's recovery reads and in what
// order: from the stored proposal and coverage as before, or from the walk's
// footprints and the placements, in a seeded order across areas. It reads
// the store's records and never writes them, and it cuts no source.
package recoverinput

import (
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/coverage"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

// Proposed is the part of g linked to c, a capability proposed from the
// history, in g's order: the docs it reads, the PRs and changes that show it
// or suggested its questions, those pl places with it by their files, and
// the changes those revert or are reverted by.
func Proposed(g store.Gather, c mapping.Capability, pl coverage.Placement) store.Gather {
	refs := map[string]bool{}
	for _, r := range c.ShownBy {
		refs[strings.TrimSpace(r)] = true
	}
	for _, q := range c.Questions {
		for _, r := range q.SuggestedBy {
			refs[strings.TrimSpace(r)] = true
		}
	}
	delete(refs, "")
	for _, n := range pl.PRs[c.Name] {
		refs["#"+strconv.Itoa(n)] = true
	}
	placed := map[string]bool{}
	for _, id := range pl.Changes[c.Name] {
		placed[id] = true
	}
	sub := pick(g,
		func(p store.GatheredPR) bool { return refs["#"+strconv.Itoa(p.Number)] },
		func(ch store.GatheredChange) bool {
			if placed[ch.ID] {
				return true
			}
			for r := range refs {
				if !strings.HasPrefix(r, "#") && (r == ch.Short || strings.HasPrefix(ch.ID, r)) {
					return true
				}
			}
			return false
		})
	sub.Docs = reading.Linked(g.Docs, c.Docs)
	return sub
}

// pick is g's PRs that pr links and its changes that change links, together
// with the changes those revert or are reverted by, in g's order, with g's
// run and no docs.
func pick(g store.Gather, pr func(store.GatheredPR) bool, change func(store.GatheredChange) bool) store.Gather {
	sub := store.Gather{Run: g.Run}
	for _, p := range g.PRs {
		if pr(p) {
			sub.PRs = append(sub.PRs, p)
		}
	}
	linked := make([]bool, len(g.Changes))
	for i, ch := range g.Changes {
		linked[i] = change(ch)
	}
	for i, ch := range g.Changes {
		keep := linked[i]
		for j := 0; !keep && j < len(g.Changes); j++ {
			keep = linked[j] && (reverts(ch, g.Changes[j]) || reverts(g.Changes[j], ch))
		}
		if keep {
			sub.Changes = append(sub.Changes, ch)
		}
	}
	return sub
}

// reverts says whether a's message names b as the change it reverts; the id
// it names may be abbreviated.
func reverts(a, b store.GatheredChange) bool {
	return a.Reverts != "" && b.ID != "" && (strings.HasPrefix(b.ID, a.Reverts) || strings.HasPrefix(a.Reverts, b.ID))
}
