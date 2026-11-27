package recoverinput

import (
	"cmp"
	"math/rand/v2"
	"slices"

	"github.com/dphaener/physa/internal/store"
)

// Seed is the seed of recovery's order chosen for the gather g: its time,
// so a gather always gives the same seed and a new gather a new one.
func Seed(g store.Gather) uint64 {
	return uint64(g.Run.At.UnixNano())
}

// Order is caps in recovery's order for seed: one capability from each area
// in turn, round after round, until all are taken, the areas and each
// area's capabilities in a random order seed fixes. An area is its name
// together with the area it was split from. The order depends only on the
// set of capabilities and the seed, never on the order caps come in, so a
// run that stops early leaves a sample spread across the areas.
func Order(caps []Walked, seed uint64) []Walked {
	type key struct{ parent, area string }
	groups := map[key][]Walked{}
	var keys []key
	for _, c := range caps {
		k := key{c.Parent, c.Area}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], c)
	}
	slices.SortFunc(keys, func(a, b key) int {
		return cmp.Or(cmp.Compare(a.parent, b.parent), cmp.Compare(a.area, b.area))
	})
	// PCG's sequence is its documented algorithm; the shuffle is written
	// here because Go does not promise rand.Shuffle's.
	src := rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)
	lists := make([][]Walked, len(keys))
	for i, k := range keys {
		lists[i] = groups[k]
		slices.SortStableFunc(lists[i], func(a, b Walked) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Promise, b.Promise))
		})
	}
	shuffle(src, lists)
	for _, l := range lists {
		shuffle(src, l)
	}
	out := make([]Walked, 0, len(caps))
	for round := 0; len(out) < len(caps); round++ {
		for _, l := range lists {
			if round < len(l) {
				out = append(out, l[round])
			}
		}
	}
	return out
}

// shuffle is Fisher–Yates over s, each index drawn from src's next value.
// The modulo's bias is below one in 2^40 for any list recovery orders.
func shuffle[T any](src *rand.PCG, s []T) {
	for i := len(s) - 1; i > 0; i-- {
		j := int(src.Uint64() % uint64(i+1))
		s[i], s[j] = s[j], s[i]
	}
}
