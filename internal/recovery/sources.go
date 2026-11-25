package recovery

import (
	"strconv"
	"strings"
	"sync"

	"github.com/dphaener/physa/internal/coverage"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

// sources is the part of g linked to c, in g's order: the docs it reads, the PRs and changes
// that show it or suggested its questions, those pl places with it by their files, and the
// changes those revert or are reverted by.
func sources(g store.Gather, c mapping.Capability, pl coverage.Placement) store.Gather {
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
	sub := store.Gather{Run: g.Run, Docs: reading.Linked(g.Docs, c.Docs)}
	for _, p := range g.PRs {
		if refs["#"+strconv.Itoa(p.Number)] {
			sub.PRs = append(sub.PRs, p)
		}
	}
	linked := make([]bool, len(g.Changes))
	for i, ch := range g.Changes {
		linked[i] = placed[ch.ID]
		for r := range refs {
			if !strings.HasPrefix(r, "#") && (r == ch.Short || strings.HasPrefix(ch.ID, r)) {
				linked[i] = true
			}
		}
	}
	// reverts says whether a's message names b as the change it reverts; the
	// id it names may be abbreviated.
	reverts := func(a, b store.GatheredChange) bool {
		return a.Reverts != "" && b.ID != "" && (strings.HasPrefix(b.ID, a.Reverts) || strings.HasPrefix(a.Reverts, b.ID))
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

// resolver answers whether a PR exists: yes for every PR of g, without
// asking, and otherwise what exists says, each number asked once. It is safe
// for capabilities checked at the same time, and asks exists one call at a
// time. A nil exists gives nil: anchors are then checked for form only.
func resolver(g store.Gather, exists func(int) (bool, error)) func(int) (bool, error) {
	if exists == nil {
		return nil
	}
	known := map[int]bool{}
	for _, p := range g.PRs {
		known[p.Number] = true
	}
	var mu sync.Mutex
	return func(n int) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if ok, seen := known[n]; seen {
			return ok, nil
		}
		ok, err := exists(n)
		if err != nil {
			return false, err
		}
		known[n] = ok
		return ok, nil
	}
}
