package mapping

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/fit"
)

// ResultVersion is the version of a Result's JSON form. It changes only when
// a field changes meaning or goes away.
const ResultVersion = 1

// rawRunes is the most characters WriteText shows of one malformed item.
const rawRunes = 200

// Fitting is fit.Report in its JSON form.
type Fitting struct {
	Budget  int         `json:"budget"`
	Used    int         `json:"used"`
	Sources []FitSource `json:"sources"`
	// Lines says per source what was cut, as fit.Report.Lines does.
	Lines []string `json:"lines"`
}

// FitSource is fit.SourceReport in its JSON form.
type FitSource struct {
	Name       string `json:"name"`
	Items      int    `json:"items"`
	Kept       int    `json:"kept"`
	Partial    int    `json:"partial"`
	Tokens     int    `json:"tokens"`
	KeptTokens int    `json:"kept_tokens"`
}

func fitting(r fit.Report) Fitting {
	f := Fitting{Budget: r.Budget, Used: r.Used, Sources: []FitSource{}, Lines: orEmpty(r.Lines())}
	for _, s := range r.Sources {
		f.Sources = append(f.Sources, FitSource{Name: s.Name, Items: s.Items, Kept: s.Kept, Partial: s.Partial, Tokens: s.Tokens, KeptTokens: s.KeptTokens})
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
// product-wide questions, what was dropped and why, what fitting cut, and
// what the session cost. A stopped session says so in place of a proposal.
func WriteText(w io.Writer, r Result) error {
	var b strings.Builder
	b.WriteString("# Proposed map\n\n")
	if r.Stopped {
		fmt.Fprintf(&b, "Stopped at the appetite (%s) after %s; nothing proposed and nothing stored.\n\n", plural(int(r.Appetite), "token", "tokens"), r.Tokens)
	} else {
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
	}
	fmt.Fprintf(&b, "## Fitting\n\nSources fitted to about %d of a budget of %s.\n\n", r.Fit.Used, plural(r.Fit.Budget, "token", "tokens"))
	for _, l := range r.Fit.Lines() {
		b.WriteString("- " + l + "\n")
	}
	b.WriteString("\n## Session\n\n")
	fmt.Fprintf(&b, "- id: %s\n- model: %s\n- tokens: %s\n- appetite: %d\n", orNone(r.SessionID), orNone(r.Model), r.Tokens, r.Appetite)
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
	Stopped      bool         `json:"stopped"`
	Product      Product      `json:"product"`
	Capabilities []Capability `json:"capabilities"`
	Questions    []Question   `json:"questions"`
	Dropped      Dropped      `json:"dropped"`
	Malformed    []Malformed  `json:"malformed"`
	Fitting      Fitting      `json:"fitting"`
	Session      sessionJSON  `json:"session"`
}

type sessionJSON struct {
	ID       string         `json:"id"`
	Model    string         `json:"model"`
	Tokens   adapter.Tokens `json:"tokens"`
	Appetite int64          `json:"appetite"`
}

// WriteJSON writes r in its versioned JSON form, indented, holding what
// WriteText shows, with empty lists as [].
func WriteJSON(w io.Writer, r Result) error {
	out := resultJSON{
		Version: ResultVersion, Stopped: r.Stopped,
		Product: r.Proposal.Product, Capabilities: []Capability{}, Questions: []Question{},
		Dropped: orEmpty(r.Dropped), Malformed: orEmpty(r.Malformed), Fitting: fitting(r.Fit),
		Session: sessionJSON{ID: r.SessionID, Model: r.Model, Tokens: r.Tokens, Appetite: r.Appetite},
	}
	question := func(q Question) Question {
		return Question{Question: q.Question, SuggestedBy: orEmpty(q.SuggestedBy)}
	}
	for _, c := range r.Proposal.Capabilities {
		c.ShownBy = orEmpty(c.ShownBy)
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
