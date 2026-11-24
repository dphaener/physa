package mapping

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapping/shareview"
)

// ResultVersion is the version of a Result's JSON form. It changes only when
// a field changes meaning or goes away.
const ResultVersion = 3

// rawRunes is the most characters WriteText shows of one malformed item.
const rawRunes = 200

// Reading is what the session read, in its JSON form: the prompt's
// estimated tokens, the sources' together, and each source.
type Reading struct {
	PromptTokens int          `json:"prompt_tokens"`
	SourceTokens int          `json:"source_tokens"`
	Sources      []ReadSource `json:"sources"`
	// Lines says per source what was read, as reading.Report.Lines does.
	Lines []string `json:"lines"`
	// Shares and Merge are set only for a proposal made in shares.
	Shares []shareview.JSON `json:"shares,omitempty"`
	Merge  *shareview.JSON  `json:"merge,omitempty"`
}

// ReadSource is reading.SourceReport in its JSON form.
type ReadSource = shareview.Source

func read(r Result) Reading {
	f := Reading{PromptTokens: r.PromptTokens, SourceTokens: r.Read.Tokens, Sources: shareview.Sources(r.Read), Lines: orEmpty(r.Read.Lines())}
	for _, s := range r.Shares {
		f.Shares = append(f.Shares, s.JSON())
	}
	if r.Merge != nil {
		m := r.Merge.JSON()
		f.Merge = &m
	}
	return f
}

// suggested writes what suggested a question, or nothing.
func suggested(q Question) string {
	if len(q.SuggestedBy) == 0 {
		return ""
	}
	return " (suggested by " + strings.Join(q.SuggestedBy, ", ") + ")"
}

// WriteText writes r for people, as markdown: the product's promise, each
// capability with its promise, what shows it and its questions, the
// product-wide questions, what was dropped and why, what the session read,
// and what it cost.
func WriteText(w io.Writer, r Result) error {
	var b strings.Builder
	b.WriteString("# Proposed map\n\n")
	p := r.Proposal
	if p.Product.Promise == "" {
		b.WriteString("No promise proposed for the product.\n\n")
	} else {
		b.WriteString(p.Product.Promise + "\n\n")
	}
	fmt.Fprintf(&b, "## Capabilities (%d)\n\n", len(p.Capabilities))
	for _, c := range p.Capabilities {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", c.Name, c.Promise)
		if len(c.ShownBy) == 0 {
			b.WriteString("Shown by: nothing gathered\n\n")
		} else {
			b.WriteString("Shown by: " + strings.Join(c.ShownBy, ", ") + "\n\n")
		}
		if len(c.Docs) > 0 {
			b.WriteString("Docs: " + strings.Join(c.Docs, ", ") + "\n\n")
		}
		if len(c.Questions) > 0 {
			b.WriteString("Questions:\n")
			for _, q := range c.Questions {
				b.WriteString("- " + q.Question + suggested(q) + "\n")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("## Questions\n\n")
	if len(p.Questions) == 0 {
		b.WriteString("None.\n")
	}
	for _, q := range p.Questions {
		b.WriteString("- " + q.Question + suggested(q) + "\n")
	}
	b.WriteString("\n")
	if len(r.Dropped)+len(r.Malformed) > 0 {
		b.WriteString("## Dropped\n\n")
		for _, d := range r.Dropped {
			what := d.What
			if d.Capability != "" {
				what += " of " + d.Capability
			}
			fmt.Fprintf(&b, "- %s `%s`: %s\n", what, shown(d.Value), draft.OneLine(d.Reason))
		}
		for _, m := range r.Malformed {
			fmt.Fprintf(&b, "- malformed item `%s`: %s\n", shown(m.Raw), draft.OneLine(m.Reason))
		}
		b.WriteString("\n")
	}
	b.WriteString("## Read\n\n")
	if len(r.Shares) > 0 {
		shareview.Read(&b, r.Read.Tokens, r.PromptTokens, r.Shares, r.Merge)
	} else {
		fmt.Fprintf(&b, "Every source whole, about %s, in a prompt of about %s.\n\n", plural(r.Read.Tokens, "token", "tokens"), plural(r.PromptTokens, "token", "tokens"))
		for _, l := range r.Read.Lines() {
			b.WriteString("- " + l + "\n")
		}
	}
	b.WriteString("\n## Session\n\n")
	if len(r.Shares) > 0 {
		shareview.Sessions(&b, r.Shares, r.Merge, r.Tokens)
	} else {
		fmt.Fprintf(&b, "- id: %s\n- model: %s\n- tokens: %s\n", orNone(r.SessionID), orNone(r.Model), r.Tokens)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// shown returns s on one line, without backticks and cut to rawRunes, to sit
// in a line of the Dropped list.
func shown(s string) string {
	s = strings.ReplaceAll(draft.OneLine(s), "`", "'")
	if r := []rune(s); len(r) > rawRunes {
		return string(r[:rawRunes]) + "…"
	}
	return s
}

func orNone(s string) string {
	if s == "" {
		return "not reported"
	}
	return s
}

// resultJSON is a Result's versioned JSON form.
type resultJSON struct {
	Version      int          `json:"version"`
	Product      Product      `json:"product"`
	Capabilities []Capability `json:"capabilities"`
	Questions    []Question   `json:"questions"`
	Dropped      Dropped      `json:"dropped"`
	Malformed    []Malformed  `json:"malformed"`
	Read         Reading      `json:"read"`
	Session      sessionJSON  `json:"session"`
}

type sessionJSON = shareview.Session

// WriteJSON writes r in its versioned JSON form, indented, holding what
// WriteText shows, with empty lists as [].
func WriteJSON(w io.Writer, r Result) error {
	out := resultJSON{
		Version: ResultVersion,
		Product: r.Proposal.Product, Capabilities: []Capability{}, Questions: []Question{},
		Dropped: orEmpty(r.Dropped), Malformed: orEmpty(r.Malformed), Read: read(r),
		Session: sessionJSON{ID: r.SessionID, Model: r.Model, Tokens: r.Tokens},
	}
	question := func(q Question) Question {
		return Question{Question: q.Question, SuggestedBy: orEmpty(q.SuggestedBy)}
	}
	for _, c := range r.Proposal.Capabilities {
		c.ShownBy = orEmpty(c.ShownBy)
		c.Docs = orEmpty(c.Docs)
		qs := []Question{}
		for _, q := range c.Questions {
			qs = append(qs, question(q))
		}
		c.Questions = qs
		out.Capabilities = append(out.Capabilities, c)
	}
	for _, q := range r.Proposal.Questions {
		out.Questions = append(out.Questions, question(q))
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// plural returns n with its noun in singular or plural form.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
