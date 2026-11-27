// Package walk holds the skill for init's walk: one session reads an area's code, tests and docs and names its capabilities, each a promise with a footprint, and marks every other source file of the area shared with a reason. Footprints are init's own scaffolding, kept in init's record and never in the map.
package walk

import _ "embed"

// CapabilityDefinition is the definition of a capability the walk skill
// carries, word for word as architecture 15 section 4 quotes it.
const CapabilityDefinition = "A promise the product makes to its users, stated as intended behaviour: something a user can rely on and would name, true however the code is arranged, and whose breaking someone would report. A capability can be large and is kept by many changes over time; a change that needs two capabilities usually means they are one."

// Walk is the walk session's system text: what the map is, what a
// capability is, how to walk an area, what each capability carries and the
// shape of the answer.
//
//go:embed walk.md
var Walk string

// Split is the section a top-level area's session gets after Walk: it may
// split its area once, by promise, into parts that are walked on their own.
//
//go:embed split.md
var Split string
