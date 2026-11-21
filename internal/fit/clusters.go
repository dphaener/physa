package fit

import (
	"sort"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/store"
)

// clusters finds files that change together. Changes touching more than
// ClusterMaxFiles files are skipped; pairs of files sharing at least
// ClusterMinChanges changes are joined, and joined pairs form clusters. Each
// cluster counts the changes touching two or more of its files. Clusters are
// ordered by that count, most first, then by first path.
func clusters(changes []store.GatheredChange) []Cluster {
	var sets [][]string
	for _, c := range changes {
		if len(c.Files) < 2 || len(c.Files) > ClusterMaxFiles {
			continue
		}
		fs := append([]string(nil), c.Files...)
		sort.Strings(fs)
		fs = dedupe(fs)
		if len(fs) >= 2 {
			sets = append(sets, fs)
		}
	}
	type pair struct{ a, b string }
	shared := map[pair]int{}
	for _, fs := range sets {
		for i := range fs {
			for j := i + 1; j < len(fs); j++ {
				shared[pair{fs[i], fs[j]}]++
			}
		}
	}
	var strong []pair
	for p, n := range shared {
		if n >= ClusterMinChanges {
			strong = append(strong, p)
		}
	}
	sort.Slice(strong, func(i, j int) bool {
		if strong[i].a != strong[j].a {
			return strong[i].a < strong[j].a
		}
		return strong[i].b < strong[j].b
	})

	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		p, ok := parent[x]
		if !ok || p == x {
			parent[x] = x
			return x
		}
		root := find(p)
		parent[x] = root
		return root
	}
	for _, p := range strong {
		ra, rb := find(p.a), find(p.b)
		if ra == rb {
			continue
		}
		if rb < ra {
			ra, rb = rb, ra
		}
		parent[rb] = ra // the smallest path is always the root
	}

	members := map[string][]string{}
	for f := range parent {
		r := find(f)
		members[r] = append(members[r], f)
	}
	count := map[string]int{}
	for _, fs := range sets {
		seen := map[string]int{}
		for _, f := range fs {
			if _, ok := parent[f]; ok {
				seen[find(f)]++
			}
		}
		for r, n := range seen {
			if n >= 2 {
				count[r]++
			}
		}
	}

	var out []Cluster
	for r, fs := range members {
		sort.Strings(fs)
		out = append(out, Cluster{Paths: fs[:min(len(fs), ClusterPaths)], Changes: count[r]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Changes != out[j].Changes {
			return out[i].Changes > out[j].Changes
		}
		return out[i].Paths[0] < out[j].Paths[0]
	})
	return out
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

func clusterTokens(c Cluster) int {
	return Tokens(strings.Join(c.Paths, " ") + " " + strconv.Itoa(c.Changes))
}

// fitClusters keeps clusters, in order, within left tokens.
func fitClusters(changes []store.GatheredChange, left int) ([]Cluster, SourceReport) {
	r := SourceReport{Name: "clusters"}
	var kept []Cluster
	full := true
	for _, c := range clusters(changes) {
		n := clusterTokens(c)
		r.Items++
		r.Tokens += n
		if !full || n > left {
			full = false
			continue
		}
		left -= n
		r.Kept++
		r.KeptTokens += n
		kept = append(kept, c)
	}
	return kept, r
}
