package place

import (
	"encoding/json"
	"fmt"

	"github.com/dphaener/physa/internal/initrecord"
)

// Record is the body init's record keeps for the placement, read back with
// Load. Its file paths are init's own and never reach the map.
type Record struct {
	// PRs and Changes are the placed ones, in gather order.
	PRs     []PRPlaced     `json:"prs"`
	Changes []ChangePlaced `json:"changes"`
	Nowhere Nowhere        `json:"nowhere"`
	// ByCapability is every capability of the walk, in walk order, with the
	// PRs placed on it and the changes placed there on their own.
	ByCapability []CapabilityPlaced `json:"by_capability"`
	Broad        []BroadEntry       `json:"broad_entries"`
	// WalkEntries is false when the walk was stored without its footprints'
	// entries, so each stored file placed as itself and none could be broad.
	WalkEntries  bool `json:"walk_entries"`
	FilesWeighed int  `json:"files_weighed"`
}

// PRPlaced is a gathered PR with the capabilities it is placed on.
type PRPlaced struct {
	Number     int         `json:"number"`
	Placements []Placement `json:"placements"`
}

// ChangePlaced is a gathered change with the capabilities it is placed on.
// PR is the gathered PR it follows, 0 when it is placed on its own.
type ChangePlaced struct {
	ID         string      `json:"id"`
	PR         int         `json:"pr,omitempty"`
	Placements []Placement `json:"placements"`
}

// Nowhere is the PRs and changes placed on no capability, with the reason.
type Nowhere struct {
	PRs     []PRNowhere     `json:"prs"`
	Changes []ChangeNowhere `json:"changes"`
}

// PRNowhere is a gathered PR placed nowhere.
type PRNowhere struct {
	Number int    `json:"number"`
	Reason string `json:"reason"`
}

// ChangeNowhere is a gathered change placed nowhere. PR is the gathered PR
// whose reason it carries, 0 when the reason is its own.
type ChangeNowhere struct {
	ID     string `json:"id"`
	PR     int    `json:"pr,omitempty"`
	Reason string `json:"reason"`
}

// CapabilityPlaced is a capability, with its area and, when that area is a
// part, the area it was split from, and what is placed on it.
type CapabilityPlaced struct {
	Capability string   `json:"capability"`
	Area       string   `json:"area"`
	Parent     string   `json:"parent"`
	PRs        []int    `json:"prs"`
	Changes    []string `json:"changes"`
}

// Loader is the store as Load reads it.
type Loader interface {
	Place() (initrecord.Place, bool, error)
}

// Load reads the stored placement and decodes its body, false when none
// exists.
func Load(st Loader) (Record, bool, error) {
	p, ok, err := st.Place()
	if err != nil || !ok {
		return Record{}, false, err
	}
	var r Record
	if err := json.Unmarshal(p.Body, &r); err != nil {
		return Record{}, false, fmt.Errorf("the stored placement does not decode: %w", err)
	}
	return r, true, nil
}
