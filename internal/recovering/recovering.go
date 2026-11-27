// Package recovering holds the recovering skill and what surrounds one
// session of it: the prompt for one capability, or for one share of its
// sources when they are more than one session holds, reading the capability's
// single text (which Physa splits into its folder) and the terms the session
// writes, and dropping the entries, overview and terms the map check fails.
// It runs no session and writes nothing.
package recovering

import (
	"time"

	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/reading"
)

// Reads is what a session reads of a capability's linked sources, all of it
// whole: change messages, and no clusters, which Prompt never writes.
var Reads = reading.Options{Messages: true}

// Input is everything one session recovers a capability from.
type Input struct {
	// Product is the product's promise, as the proposal holds it.
	Product string
	// Name and Promise are the capability's, as the proposal or the walk
	// holds them.
	Name, Promise string
	// Questions are the proposal's questions about this capability; a
	// capability found in the code has none.
	Questions []mapping.Question
	// Area is the area of the code a capability found in the code was found
	// in; empty for one proposed from the history.
	Area string
	// Footprint is set for a capability found in the code, and nil for one
	// proposed from the history. When it is set, the prompt is the walk's
	// form: the area, the capability's files, and its placed PRs and changes.
	Footprint *Footprint
	// Date is the day of recovery: the date its questions are raised on.
	Date time.Time
	// Sources are the capability's linked sources, whole, or one share of
	// them.
	Sources reading.Input
	// Share and Shares say which share of the capability's sources this
	// session reads when they are more than one session holds; Shares below
	// 2 is one session. Earlier and EarlierTerms are the text and terms the
	// sessions on the shares before it recovered.
	Share, Shares         int
	Earlier, EarlierTerms string
}

// Footprint is the files a capability found in the code owns and those that
// bear on it, as the walk named them, each a path from the repo root; a path
// ending in "/" is a folder. Every share of the capability's sources is given
// all of it.
type Footprint struct {
	// Sources are the source files the capability owns.
	Sources []SourceFile
	// Tests, Docs and Other are the tests, the docs and guides, and the other
	// files (configuration, fixtures, assets and the like) that bear on it.
	Tests, Docs, Other []string
}

// SourceFile is one source file of a footprint, with its line count; Lines is 0
// when the count is not known, and then none is written.
type SourceFile struct {
	Path  string
	Lines int
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
