// Package recoverinput is what each capability's recovery reads and in what
// order: the walk's footprints and the placements, in a seeded order across
// areas. It reads the store's records and never writes them, and it cuts no
// source.
package recoverinput

import (
	"strings"

	"github.com/dphaener/physa/internal/store"
)

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
