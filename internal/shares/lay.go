package shares

import (
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
)

// The kinds of source, in the order a share reads them.
const (
	kindDoc = iota
	kindPR
	kindChange
	kindCluster
)

// slack is the bytes a share's estimate keeps for what its prompt writes
// once per share around its sources: fences, and labels whose digits grow.
// The real prompt is checked after, so slack only makes a first guess safe.
const slack = 64

// unit is one source as Lay lays it, whole: a doc, a PR with all its review
// comments, a change, or a cluster.
type unit struct {
	kind, i int
	// cost is the bytes it adds to a prompt beside others of its kind.
	cost int
	// alone is the bytes of propose's prompt holding it and nothing else.
	alone int
}

// Lay lays in's sources into shares whose whole prompts are each at most
// room estimated tokens: the window less mapping.ReplyReserve, or less
// after a halving. Sources keep reading.Of's order (docs, PRs each with all
// its review comments, changes, clusters), and a share ends when the next
// source does not fit beside it. When the docs take at most half of room,
// every share reads all of them and only the other sources are divided;
// the docs are divided too when they take more, or when a source fits an
// empty share but not beside them. Every source reaches exactly one share
// whole, the docs every share when they are not divided. A source that does
// not fit even an empty share is a *TooLarge, and Lay returns no shares.
// Each share's Report counts only what it reads. The same input and room
// always give the same shares.
func Lay(in reading.Input, room int) ([]reading.Input, error) {
	limit := max(room, 0) * reading.BytesPerTokenTenths / 10
	base := len(mapping.Prompt(reading.Input{}))
	units, err := measure(in, room, base, limit)
	if err != nil {
		return nil, err
	}
	if len(units) == 0 {
		return []reading.Input{in.Recounted()}, nil
	}
	nDocs := len(in.Docs)
	if nDocs > 0 {
		docs := len(mapping.Prompt(reading.Input{Docs: in.Docs})) - base
		shared := reading.BytesTokens(docs) <= room/2
		for _, u := range units[nDocs:] {
			if !shared {
				break
			}
			shared = u.alone+docs+slack <= limit
		}
		if shared {
			if out, _ := lay(in, units[nDocs:], base+docs, limit, true); out != nil {
				return out, nil
			}
		}
	}
	out, u := lay(in, units, base, limit, false)
	if out == nil {
		return nil, tooLarge(in, u.kind, u.i, reading.BytesTokens(u.alone-base), room)
	}
	return out, nil
}

// measure gives each source its unit, in order, rendering it alone and
// twice to learn what it costs; a source whose prompt alone is over limit
// bytes is a *TooLarge.
func measure(in reading.Input, room, base, limit int) ([]unit, error) {
	var units []unit
	add := func(k, i int, one, two reading.Input) error {
		alone := len(mapping.Prompt(one))
		if alone > limit {
			return tooLarge(in, k, i, reading.BytesTokens(alone-base), room)
		}
		units = append(units, unit{kind: k, i: i, cost: len(mapping.Prompt(two)) - alone, alone: alone})
		return nil
	}
	for i, d := range in.Docs {
		if err := add(kindDoc, i, reading.Input{Docs: []reading.Doc{d}}, reading.Input{Docs: []reading.Doc{d, d}}); err != nil {
			return nil, err
		}
	}
	for i, p := range in.PRs {
		if err := add(kindPR, i, reading.Input{PRs: []reading.PR{p}}, reading.Input{PRs: []reading.PR{p, p}}); err != nil {
			return nil, err
		}
	}
	for i, c := range in.Commits {
		if err := add(kindChange, i, reading.Input{Commits: []reading.Commit{c}}, reading.Input{Commits: []reading.Commit{c, c}}); err != nil {
			return nil, err
		}
	}
	for i, c := range in.Clusters {
		if err := add(kindCluster, i, reading.Input{Clusters: []reading.Cluster{c}}, reading.Input{Clusters: []reading.Cluster{c, c}}); err != nil {
			return nil, err
		}
	}
	return units, nil
}

// lay packs units into shares that start at start bytes, then renders each
// share's real prompt and, while it is over limit, moves its last sources
// to the next share. With shared, every share reads all of in's docs. A
// share of one source that is still over limit ends it: lay returns no
// shares and that source.
func lay(in reading.Input, units []unit, start, limit int, shared bool) ([]reading.Input, unit) {
	var groups [][]unit
	var g []unit
	cur := start + slack
	for _, u := range units {
		if len(g) > 0 && cur+u.cost > limit {
			groups, g, cur = append(groups, g), nil, start+slack
		}
		g, cur = append(g, u), cur+u.cost
	}
	groups = append(groups, g)

	var out []reading.Input
	for i := 0; i < len(groups); i++ {
		for {
			share := build(in, groups[i], shared)
			over := len(mapping.Prompt(share)) - limit
			if over <= 0 {
				out = append(out, share.Recounted())
				break
			}
			if len(groups[i]) <= 1 {
				return nil, append(groups[i], unit{})[0]
			}
			k, moved := len(groups[i]), 0
			for k > 1 && moved < over {
				k--
				moved += groups[i][k].cost
			}
			if i+1 == len(groups) {
				groups = append(groups, nil)
			}
			groups[i+1] = append(append([]unit{}, groups[i][k:]...), groups[i+1]...)
			groups[i] = groups[i][:k]
		}
	}
	return out, unit{}
}

// build is the share of in that reads units, and all of in's docs when
// shared, under the source names of in's Report.
func build(in reading.Input, units []unit, shared bool) reading.Input {
	s := reading.Input{Report: in.Report}
	if shared {
		s.Docs = in.Docs
	}
	for _, u := range units {
		switch u.kind {
		case kindDoc:
			s.Docs = append(s.Docs, in.Docs[u.i])
		case kindPR:
			s.PRs = append(s.PRs, in.PRs[u.i])
		case kindChange:
			s.Commits = append(s.Commits, in.Commits[u.i])
		default:
			s.Clusters = append(s.Clusters, in.Clusters[u.i])
		}
	}
	return s
}
