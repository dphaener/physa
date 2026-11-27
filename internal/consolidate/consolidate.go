// Package consolidate is init's consolidation phase: over the capabilities
// recovered from the walk, sessions judge which capabilities from different
// areas are one promise, then which entries of one area repeat each other, are
// misplaced or are answered, then which rules repeated across areas are one
// rule the product keeps. Code applies each choice by id, refusing and
// reporting what fails a check, so a session's words never reach the map.
// What it did is stored in init's record, with the consolidated capabilities
// and the product's rules, only when a final guard finds every entry sourced
// and nothing lost; nothing reaches the repo or the map here.
package consolidate

import (
	"io"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/consolidating"
	"github.com/dphaener/physa/internal/recovering"
)

// Concurrency is the most sessions running at once, LiftBatch the most
// candidate groups one lift session is given, and the steps are those a
// session runs for, as Result's sessions name them.
const (
	Concurrency = 3
	LiftBatch   = 40
	StepMerge   = consolidating.StepMerge
	StepArea    = consolidating.StepArea
	StepLift    = consolidating.StepLift
)

// Session is the agent, the root the map's `from` anchors resolve in, the
// product's name, progress, the clock, and whether the cap has stopped a session.
type Session struct {
	Agent    func() (adapter.Adapter, error)
	Stopped  func() bool
	Root     string
	Product  string
	Progress io.Writer
	Now      func() time.Time
}

// Capability is a consolidated capability: one whose text or terms
// consolidation changed, with the capabilities merged into it.
type Capability struct {
	Name       string               `json:"name"`
	Promise    string               `json:"promise"`
	Area       capabilitytext.Area  `json:"area"`
	MergedFrom []string             `json:"merged_from"`
	Footprint  recovering.Footprint `json:"footprint"`
	File       string               `json:"file"`
	Terms      string               `json:"terms"`
	SessionID  string               `json:"session_id"`
}

// MergedAway is a capability merged into another: the session of its
// recovery and the entries that recovery dropped, so a write keeps both.
type MergedAway struct {
	Name      string               `json:"name"`
	Into      string               `json:"into"`
	SessionID string               `json:"session_id"`
	Dropped   []recovering.Dropped `json:"dropped"`
}

// Counts is how many capabilities, entries and product rules there were.
type Counts struct {
	Capabilities int `json:"capabilities"`
	Entries      int `json:"entries"`
	ProductRules int `json:"product_rules"`
}

// SessionRun is one session consolidation ran: its step, what it judged,
// the ids it ran under, and whether it answered or why not.
type SessionRun struct {
	Step     string   `json:"step"`
	Item     string   `json:"item"`
	IDs      []string `json:"ids"`
	Answered bool     `json:"answered"`
	Error    string   `json:"error,omitempty"`
}

// Result is what consolidation did, as init's record stores it and the
// phase shows it. Its exit code (ExitCode) is 0 when every session answered
// and 1 when any session's step was left as it was; an error from Run, with
// nothing stored, is 2.
type Result struct {
	// Fingerprint is a hash of what it was made from: every recovered row
	// it read, the walk's time and the product's promise.
	Fingerprint    string    `json:"fingerprint"`
	WalkAt         time.Time `json:"walk_at"`
	ProductPromise string    `json:"product_promise"`
	// Capabilities are the consolidated ones in walk order, and Left the
	// names of those left as recovered.
	Capabilities []Capability         `json:"capabilities"`
	Left         []string             `json:"left_as_recovered"`
	MergedAway   []MergedAway         `json:"merged_away"`
	ProductRules []string             `json:"product_rules"`
	Record       consolidating.Record `json:"record"`
	Before       Counts               `json:"before"`
	After        Counts               `json:"after"`
	Sessions     []SessionRun         `json:"sessions"`
	Model        string               `json:"model"`
	Tokens       adapter.Tokens       `json:"tokens"`
}

// ExitCode is 1 when any session's step was left as it was, else 0; refused
// items alone do not change it.
func (r Result) ExitCode() int {
	for _, s := range r.Sessions {
		if !s.Answered {
			return 1
		}
	}
	return 0
}
