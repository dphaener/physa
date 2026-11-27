// Package areas divides a repo's source files into areas by promise for init: one session names the repo's test, doc and other files as patterns, then one session (one per part on a large repo) places every source file in exactly one area. Areas are init's own scaffolding, kept in init's record and never in the map.
package areas

import _ "embed"

// AcrossParts is the first rule of the parts skill: a part is only where the
// listing was cut, so areas are grouped across every part.
const AcrossParts = "Group by promise across every part, never by part."

// Sources is the split skill: what a session is told before the repo's
// folders, so that it names the test, doc and other files as patterns.
//
//go:embed sources.md
var Sources string

// Areas is the grouping skill: what a session is told before the source
// files it places in areas.
//
//go:embed areas.md
var Areas string

// Parts is appended to Areas when the listing comes in parts.
//
//go:embed parts.md
var Parts string

// Merge is the skill for finding areas that keep one family of promises
// after the parts were placed one at a time.
//
//go:embed merge.md
var Merge string
