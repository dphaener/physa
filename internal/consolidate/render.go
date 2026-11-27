package consolidate

import (
	"fmt"
	"io"
	"strings"
)

// Text writes what consolidation did in plain lines: the counts before and
// after, the pre-pass's slug renames and term conflicts, the capabilities
// merged with their renamed slugs, the exact and session merges of entries,
// moves, dropped questions, lifts, every refusal with its reason, the
// capabilities left as recovered, the sessions and the model.
func Text(w io.Writer, r Result) error {
	rec, terms, p := r.Record, r.Record.TermConflicts[:len(r.Record.TermConflicts):len(r.Record.TermConflicts)], func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	p("Consolidated %d capabilities with %d entries into %d capabilities with %d entries and %d product rules.",
		r.Before.Capabilities, r.Before.Entries, r.After.Capabilities, r.After.Entries, r.After.ProductRules)
	for _, n := range rec.SlugRenames {
		p("Slug taken by an earlier capability: %s is now %s.", n.From, n.To)
	}
	for _, m := range rec.Merged {
		p("Merged capabilities %s into %s, with the promise of %s.", strings.Join(m.Group, ", "), m.Name, m.PromiseOf)
		for _, n := range m.Renamed {
			p("  renamed slug %s to %s", n.From, n.To)
		}
		terms = append(terms, m.TermConflicts...)
	}
	for _, t := range terms {
		p("Term %s of %s is defined differently in %s, whose definition is kept.", t.Name, t.Capability, t.KeptIn)
	}
	for _, m := range rec.ExactMerges {
		p("Exact duplicates merged into %s: %s.", m.Kept, strings.Join(m.Merged, ", "))
	}
	for _, m := range rec.EntryMerges {
		p("Entries merged into %s: %s.", m.Kept, strings.Join(m.Merged, ", "))
	}
	for _, m := range rec.Moves {
		p("Moved %s to %s.", m.From, m.To)
	}
	for _, d := range rec.Dropped {
		p("Dropped question %s, answered by %s.", d.ID, d.AnsweredBy)
	}
	for _, l := range rec.Lifts {
		p("Lifted %s to the product's rules, with %s, from %d areas.", l.Kept, strings.Join(l.With, ", "), len(l.Areas))
	}
	for _, f := range rec.Refused {
		p("Refused (%s) %s: %s", f.Step, f.Item, f.Reason)
	}
	if len(r.Left) > 0 {
		p("Left as recovered (%d): %s", len(r.Left), strings.Join(r.Left, ", "))
	}
	p("Sessions (%d):", len(r.Sessions))
	for _, s := range r.Sessions {
		if s.Answered {
			p("- %s %s: answered", s.Step, s.Item)
		} else {
			p("- %s %s: left as it was: %s", s.Step, s.Item, s.Error)
		}
	}
	_, err := fmt.Fprintf(w, "Model: %s\n", r.Model)
	return err
}
