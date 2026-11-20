// Package design is Physa's look, the same in every view: its tokens, its
// component styles, its fonts and its brand book, embedded so the views serve
// them with no network and no third party.
package design

import "embed"

// Files holds tokens.css, components.css, the fonts with their licences, and
// README.md. Serve the whole tree under one path so tokens.css finds fonts/.
//
//go:embed tokens.css components.css fonts README.md
var Files embed.FS
