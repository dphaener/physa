package mapfolder

import _ "embed"

// Readme is .physa/README.md's text, the map's fallback for agents without
// the CLI (design 01 section 6). It carries the slice skill's judgment, the
// folder layout and how to cut one entry out by its slug. The text lives in
// readme.md.
//
//go:embed readme.md
var Readme string
