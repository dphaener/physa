// Package recovering holds the recovering skill and what surrounds one
// session of it: the prompt for one capability, reading the capability's
// single text (which Physa splits into its folder) and the terms the session
// writes, and dropping the entries, overview and terms the map check fails.
// It runs no session and writes nothing.
package recovering

import (
	"time"

	"github.com/dphaener/physa/internal/fit"
	"github.com/dphaener/physa/internal/mapping"
)

// Input is everything one session recovers a capability from.
type Input struct {
	// Product is the product's promise, as the proposal holds it.
	Product string
	// Name and Promise are the capability's, as the proposal holds them.
	Name, Promise string
	// Questions are the proposal's questions about this capability.
	Questions []mapping.Question
	// Date is the day of recovery: the date its questions are raised on.
	Date time.Time
	// Sources are the capability's linked sources, fitted.
	Sources fit.Input
}

// Reply is what the session wrote, in shape only: the capability's single
// text from its title down, and its glossary terms.
type Reply struct {
	// File is the content of the last fenced block marked markdown.
	File string
	// Terms is the content of the last fenced block marked glossary; empty
	// when there is none.
	Terms string
}

// The values of Dropped.Kind beside an entry's own kind.
const (
	KindTerm     = "term"
	KindOverview = "overview"
)

// Dropped is one entry, term or overview taken out of a reply because the
// map check found problems in it. A dropped overview is replaced by the
// capability's promise.
type Dropped struct {
	// Kind is "rule", "decision", "question", "term", "overview", or
	// "entry" for one outside any known section.
	Kind string `json:"kind"`
	// Slug is the entry's slug; empty for a term, or an entry without one.
	Slug string `json:"slug,omitempty"`
	// Heading is the entry's heading, the term's name, or "the overview".
	Heading string `json:"heading"`
	// Reasons are the problems the check found in it.
	Reasons []string `json:"reasons"`
}

// Counts says what a capability's single text and its terms hold.
type Counts struct {
	Rules     int `json:"rules"`
	Decisions int `json:"decisions"`
	Questions int `json:"questions"`
	Terms     int `json:"terms"`
}
