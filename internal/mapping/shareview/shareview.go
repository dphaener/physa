// Package shareview shows what a proposal made in shares read and used: each
// share's sources and session and the merge's, in the text and JSON forms of
// the proposal's Read and Session sections. It holds the shares' part of that
// rendering so that package mapping stays within its budget.
package shareview

import (
	"fmt"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/reading"
)

// Share is what one share's session, or the merge's, read and used.
type Share struct {
	// Read says what the session read, whole; empty for the merge, which
	// reads only the shares' proposals.
	Read reading.Report
	// PromptTokens is the prompt's estimated tokens.
	PromptTokens int
	// SessionID, Model and Tokens are what the session ran under, ran on and
	// used.
	SessionID string
	Model     string
	Tokens    adapter.Tokens
}

// Source is reading.SourceReport in its JSON form.
type Source struct {
	Name   string `json:"name"`
	Items  int    `json:"items"`
	Tokens int    `json:"tokens"`
}

// Session is a session in its JSON form.
type Session struct {
	ID     string         `json:"id"`
	Model  string         `json:"model"`
	Tokens adapter.Tokens `json:"tokens"`
}

// JSON is a share in its JSON form.
type JSON struct {
	PromptTokens int      `json:"prompt_tokens"`
	SourceTokens int      `json:"source_tokens"`
	Sources      []Source `json:"sources"`
	Lines        []string `json:"lines"`
	Session      Session  `json:"session"`
}

// Sources is rep's sources in their JSON form, never nil.
func Sources(rep reading.Report) []Source {
	out := []Source{}
	for _, s := range rep.Sources {
		out = append(out, Source{Name: s.Name, Items: s.Items, Tokens: s.Tokens})
	}
	return out
}

// JSON is s in its JSON form, with no null lists.
func (s Share) JSON() JSON {
	lines := s.Read.Lines()
	if lines == nil {
		lines = []string{}
	}
	return JSON{PromptTokens: s.PromptTokens, SourceTokens: s.Read.Tokens, Sources: Sources(s.Read), Lines: lines,
		Session: Session{ID: s.SessionID, Model: s.Model, Tokens: s.Tokens}}
}

func tokens(n int) string {
	if n == 1 {
		return "1 token"
	}
	return fmt.Sprintf("%d tokens", n)
}

func orNone(s string) string {
	if s == "" {
		return "not reported"
	}
	return s
}

// Read writes the body of the Read section for a proposal in shares: the
// whole read of the gather (whole tokens, prompts tokens together in every
// prompt), each share with its source lines, and the merge.
func Read(b *strings.Builder, whole, prompts int, shares []Share, merge *Share) {
	fmt.Fprintf(b, "Every source whole, about %s, in %d shares and a merge, prompts of about %s together.\n", tokens(whole), len(shares), tokens(prompts))
	for i, s := range shares {
		fmt.Fprintf(b, "\nShare %d of %d, about %s in a prompt of about %s\n\n", i+1, len(shares), tokens(s.Read.Tokens), tokens(s.PromptTokens))
		for _, l := range s.Read.Lines() {
			b.WriteString("  - " + l + "\n")
		}
	}
	if merge != nil {
		fmt.Fprintf(b, "\nThe merge read only the shares' proposals, in a prompt of about %s.\n", tokens(merge.PromptTokens))
	}
}

// Sessions writes the body of the Session section for a proposal in shares:
// each share's session and the merge's, then the total tokens.
func Sessions(b *strings.Builder, shares []Share, merge *Share, total adapter.Tokens) {
	line := func(who string, s Share) {
		fmt.Fprintf(b, "- %s: id %s, model %s, tokens %s\n", who, orNone(s.SessionID), orNone(s.Model), s.Tokens)
	}
	for i, s := range shares {
		line(fmt.Sprintf("share %d", i+1), s)
	}
	if merge != nil {
		line("merge", *merge)
	}
	fmt.Fprintf(b, "- total tokens: %s\n", total)
}
