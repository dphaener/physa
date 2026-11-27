package place

import (
	"math"
	"math/big"
	"sort"

	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/walk"
)

// Broad is the most files a footprint's test, doc or other entry may name
// and still place: an entry naming more, such as a whole test folder, says
// nothing about one capability.
const Broad = 40

// BroadEntry is a footprint entry naming more than Broad files: kept in the
// footprint, not used to place.
type BroadEntry struct {
	Capability string `json:"capability"`
	Kind       string `json:"kind"`
	Entry      string `json:"entry"`
	Files      int    `json:"files"`
}

// Placement is one capability an item is placed on: the weight of the
// item's files there, and that weight's share of the item's whole weight.
type Placement struct {
	Capability string  `json:"capability"`
	Weight     float64 `json:"weight"`
	Share      float64 `json:"share"`
}

// weights says what each file weighs for each capability. The weights are
// exact fractions, so a tie is a tie and half is half however the files of
// an item happen to be ordered.
type weights struct {
	of      map[string]map[string]*big.Rat
	broad   []BroadEntry
	entries bool
}

// weigh reads the footprints. A source file weighs 1 for the capability that
// owns it, the first in walk order should two name it. A test, doc or other
// file a footprint names and no capability owns as source weighs 1/k for
// each of the k capabilities naming it. An entry is matched against the
// split's files of its kind; one naming more than Broad is reported instead.
// A capability stored with no entries for a kind claims that kind's stored
// files, each as itself.
func weigh(caps []walk.CapabilityRecord, split sourceset.Division) weights {
	w := weights{of: map[string]map[string]*big.Rat{}, broad: []BroadEntry{}, entries: true}
	one := big.NewRat(1, 1)
	for _, c := range caps {
		for _, f := range c.Sources {
			if w.of[f] == nil {
				w.of[f] = map[string]*big.Rat{c.Name: one}
			}
		}
	}
	claims, order := map[string][]string{}, []string{}
	anyEntry, anyFile := false, false
	for _, c := range caps {
		mine := map[string]bool{}
		kinds := []struct {
			kind    sourceset.Kind
			entries []string
			files   []string
		}{
			{sourceset.Test, c.Entries.Tests, c.Tests},
			{sourceset.Doc, c.Entries.Docs, c.Docs},
			{sourceset.Other, c.Entries.Other, c.Other},
		}
		for _, k := range kinds {
			anyEntry, anyFile = anyEntry || len(k.entries) > 0, anyFile || len(k.files) > 0
			if len(k.entries) == 0 {
				for _, f := range k.files {
					mine[f] = true
				}
				continue
			}
			for _, e := range k.entries {
				var named []string
				for _, f := range split.Files[k.kind] {
					if sourceset.Match(e, f) {
						named = append(named, f)
					}
				}
				if len(named) > Broad {
					w.broad = append(w.broad, BroadEntry{c.Name, string(k.kind), e, len(named)})
					continue
				}
				for _, f := range named {
					mine[f] = true
				}
			}
		}
		for f := range mine {
			if _, source := w.of[f]; source {
				continue
			}
			if _, seen := claims[f]; !seen {
				order = append(order, f)
			}
			claims[f] = append(claims[f], c.Name)
		}
	}
	for _, f := range order {
		share := big.NewRat(1, int64(len(claims[f])))
		w.of[f] = map[string]*big.Rat{}
		for _, name := range claims[f] {
			w.of[f][name] = share
		}
	}
	w.entries = anyEntry || !anyFile
	return w
}

// take totals the files' weights per capability and takes the largest
// first, ties by name, until half the total is covered, at most three. It
// returns nothing when no file is weighed.
func take(files []string, w weights) []Placement {
	tot, sum := map[string]*big.Rat{}, new(big.Rat)
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		for name, x := range w.of[f] {
			if tot[name] == nil {
				tot[name] = new(big.Rat)
			}
			tot[name].Add(tot[name], x)
			sum.Add(sum, x)
		}
	}
	names := make([]string, 0, len(tot))
	for name := range tot {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if c := tot[names[i]].Cmp(tot[names[j]]); c != 0 {
			return c > 0
		}
		return names[i] < names[j]
	})
	half, cum := new(big.Rat).Quo(sum, big.NewRat(2, 1)), new(big.Rat)
	var out []Placement
	for _, name := range names {
		if len(out) == 3 || (len(out) > 0 && cum.Cmp(half) >= 0) {
			break
		}
		cum.Add(cum, tot[name])
		weight, _ := tot[name].Float64()
		share, _ := new(big.Rat).Quo(tot[name], sum).Float64()
		out = append(out, Placement{name, round3(weight), round3(share)})
	}
	return out
}

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }
