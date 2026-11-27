// Package place puts each gathered PR and change on the capabilities that
// own most of its files. Placement is a fact of the footprints the walk
// stored and never a judgment: code weighs the files, with no agent call
// and no session, and the placements stay in init's record, never the map.
package place

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/sourceset"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/walk"
)

// The reasons a PR or change is placed nowhere.
const (
	NoFiles = "no files known from the gather"
	NoOwner = "none of its files belongs to a capability"
)

// Store is init's record as Run reads and writes it.
type Store interface {
	Gathered(ctx context.Context) (store.Gather, bool, error)
	Areas(ctx context.Context) (initrecord.Areas, bool, error)
	Walk(ctx context.Context) (initrecord.Walk, bool, error)
	PutPlace(context.Context, initrecord.Place) error
}

// Result is what placement did, in counts.
type Result struct {
	Capabilities int `json:"capabilities"`
	PRsPlaced    int `json:"prs_placed"`
	// PRsNowhere and ChangesNowhere count each reason, both always listed.
	PRsNowhere []ReasonCount `json:"prs_nowhere"`
	// ChangesPlaced are the changes no gathered PR holds, placed on their
	// own; ChangesWithPR are those placed where their PR is.
	ChangesPlaced  int           `json:"changes_placed"`
	ChangesWithPR  int           `json:"changes_placed_with_pr"`
	ChangesNowhere []ReasonCount `json:"changes_nowhere"`
	// Without are the capabilities no PR or change is placed on.
	Without      Without      `json:"capabilities_without"`
	Broad        []BroadEntry `json:"broad_entries"`
	WalkEntries  bool         `json:"walk_entries"`
	FilesWeighed int          `json:"files_weighed"`
}

// ReasonCount is how many items are placed nowhere for one reason.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Without is the capabilities with no PR or change, in walk order.
type Without struct {
	Count int      `json:"count"`
	Names []string `json:"names"`
}

// Run places every gathered PR and change by the stored footprints and
// stores the placement in init's record. It needs the walk and the gather,
// and says which phase to run when one is missing, before anything else.
func Run(ctx context.Context, st Store) (Result, error) {
	w, ok, err := st.Walk(ctx)
	if err == nil && !ok {
		err = errors.New("no walk stored yet; run physa init --walk first")
	}
	if err != nil {
		return Result{}, err
	}
	g, ok, err := st.Gathered(ctx)
	if err == nil && !ok {
		err = errors.New("no gather stored yet; run physa init --gather first")
	}
	if err != nil {
		return Result{}, err
	}
	ar, ok, err := areas.Load(ctx, st)
	if err == nil && !ok {
		err = errors.New("no areas stored yet; run physa init --areas first")
	}
	if err != nil {
		return Result{}, err
	}
	var wr walk.Record
	if err := json.Unmarshal(w.Body, &wr); err != nil {
		return Result{}, fmt.Errorf("the stored walk does not decode: %w", err)
	}
	rec, res := placeAll(g, ar.Split, wr.Capabilities)
	body, err := json.Marshal(rec)
	if err != nil {
		return Result{}, err
	}
	if err := st.PutPlace(ctx, initrecord.Place{WalkAt: w.At, At: time.Now().UTC(), Body: body}); err != nil {
		return Result{}, err
	}
	return res, nil
}

// placeAll places each gathered PR, then each change no gathered PR holds,
// in gather order; a change a gathered PR holds follows it. A PR's files
// are those of the changes that name it. A capability is known by its name
// in the walk, which is unique there, and carries its area and parent.
func placeAll(g store.Gather, split sourceset.Division, caps []walk.CapabilityRecord) (Record, Result) {
	w := weigh(caps, split)
	rec := Record{PRs: []PRPlaced{}, Changes: []ChangePlaced{}, Nowhere: Nowhere{PRs: []PRNowhere{}, Changes: []ChangeNowhere{}},
		ByCapability: []CapabilityPlaced{}, Broad: w.broad, WalkEntries: w.entries, FilesWeighed: len(w.of)}
	res := Result{Capabilities: len(caps), PRsNowhere: []ReasonCount{{NoFiles, 0}, {NoOwner, 0}}, ChangesNowhere: []ReasonCount{{NoFiles, 0}, {NoOwner, 0}},
		Without: Without{Names: []string{}}, Broad: w.broad, WalkEntries: w.entries, FilesWeighed: len(w.of)}
	at := map[string]int{}
	for _, c := range caps {
		if _, dup := at[c.Name]; !dup {
			at[c.Name] = len(rec.ByCapability)
			rec.ByCapability = append(rec.ByCapability, CapabilityPlaced{c.Name, c.Area, c.Parent, []int{}, []string{}})
		}
	}
	// decide gives an item's placements, or the reason it has none.
	decide := func(files []string) ([]Placement, string) {
		if len(files) == 0 {
			return nil, NoFiles
		}
		if taken := take(files, w); len(taken) > 0 {
			return taken, ""
		}
		return nil, NoOwner
	}
	count := func(counts []ReasonCount, reason string) {
		for i := range counts {
			if counts[i].Reason == reason {
				counts[i].Count++
			}
		}
	}
	type outcome struct {
		placements []Placement
		reason     string
	}
	prs, files := map[int]*outcome{}, map[int][]string{}
	for _, p := range g.PRs {
		prs[p.Number] = nil
	}
	held := func(ch store.GatheredChange) (int, bool) {
		n := store.PROf(ch)
		_, ok := prs[n]
		return n, ok && n > 0
	}
	for _, ch := range g.Changes {
		if n, ok := held(ch); ok {
			files[n] = append(files[n], ch.Files...)
		}
	}
	for _, p := range g.PRs {
		if prs[p.Number] != nil {
			continue
		}
		o := &outcome{}
		o.placements, o.reason = decide(files[p.Number])
		prs[p.Number] = o
		if o.reason != "" {
			rec.Nowhere.PRs = append(rec.Nowhere.PRs, PRNowhere{p.Number, o.reason})
			count(res.PRsNowhere, o.reason)
			continue
		}
		rec.PRs, res.PRsPlaced = append(rec.PRs, PRPlaced{p.Number, o.placements}), res.PRsPlaced+1
		for _, pl := range o.placements {
			c := &rec.ByCapability[at[pl.Capability]]
			c.PRs = append(c.PRs, p.Number)
		}
	}
	for _, ch := range g.Changes {
		n, follows := held(ch)
		var o outcome
		if follows {
			o = *prs[n]
		} else {
			n = 0
			o.placements, o.reason = decide(ch.Files)
		}
		switch {
		case o.reason != "":
			rec.Nowhere.Changes = append(rec.Nowhere.Changes, ChangeNowhere{ch.ID, n, o.reason})
			count(res.ChangesNowhere, o.reason)
		case follows:
			rec.Changes, res.ChangesWithPR = append(rec.Changes, ChangePlaced{ch.ID, n, o.placements}), res.ChangesWithPR+1
		default:
			rec.Changes, res.ChangesPlaced = append(rec.Changes, ChangePlaced{ch.ID, 0, o.placements}), res.ChangesPlaced+1
			for _, pl := range o.placements {
				c := &rec.ByCapability[at[pl.Capability]]
				c.Changes = append(c.Changes, ch.ID)
			}
		}
	}
	for _, c := range rec.ByCapability {
		if len(c.PRs)+len(c.Changes) == 0 {
			res.Without.Names = append(res.Without.Names, c.Capability)
		}
	}
	res.Without.Count = len(res.Without.Names)
	return rec, res
}
