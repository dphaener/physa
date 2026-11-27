// Package consolidatedmap puts a stored consolidation into the map init
// writes: its consolidated capabilities, terms and product rules take the
// place of the recovered ones, the sessions and entries of capabilities
// merged away are kept, and everything consolidation left out is listed for
// the PR body with its reason. A consolidation that no longer matches the
// recovered rows is never used.
package consolidatedmap

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/consolidate"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/recovery"
)

// Stale is the note for a stored consolidation that no longer matches the
// recovered rows, which a write then ignores.
const Stale = "the stored consolidation no longer matches the recovered capabilities, so the map is written without it; run physa init --consolidate again"

// Apply returns m with the stored consolidation in it, when one is stored and
// still current, and a note, empty unless a stored one is stale (then m comes
// back as it was). Capabilities merged away drop out of m, their entries
// dropped in recovery going to the one they merged into; the sessions of
// consolidation are added to m's.
func Apply(st *initrecord.Store, m initbranch.Map) (initbranch.Map, string, error) {
	ctx := context.TODO()
	res, ok, err := consolidate.Load(st)
	if err != nil || !ok {
		return m, "", err
	}
	if cur, err := consolidate.Current(st); err != nil || !cur {
		return m, Stale, err
	}
	rows, err := st.Recovered(ctx)
	if err != nil {
		return m, "", err
	}
	own := map[string]string{}
	for _, r := range rows {
		own[r.Capability] = strings.TrimSpace(r.Terms)
	}
	changed, away := map[string]consolidate.Capability{}, map[string][]initbranch.Drop{}
	for _, c := range res.Capabilities {
		changed[c.Name] = c
	}
	gone := map[string]string{}
	for _, a := range res.MergedAway {
		gone[a.Name] = a.Into
		for _, d := range a.Dropped {
			away[a.Into] = append(away[a.Into], initbranch.Drop{What: d.Kind, Value: d.Heading + " (" + d.Slug + ")", Reason: strings.Join(d.Reasons, "; ")})
		}
	}
	caps, terms := m.Capabilities, []string(nil)
	m.Capabilities = nil
	for _, c := range caps {
		if gone[c.Name] != "" {
			continue
		}
		t := own[c.Name]
		if n, ok := changed[c.Name]; ok {
			c.Promise, c.File, c.Status, c.Reason, t = n.Promise, n.File, recovery.StatusRecovered, "", strings.TrimSpace(n.Terms)
		}
		c.Dropped = append(c.Dropped, away[c.Name]...)
		if c.File != "" && t != "" {
			terms = append(terms, t)
		}
		m.Capabilities = append(m.Capabilities, c)
	}
	m.Terms, m.ProductRules = strings.Join(terms, "\n\n"), res.ProductRules
	for _, s := range res.Sessions {
		m.Sessions = append(m.Sessions, s.IDs...)
	}
	m.Summary.Dropped = append(m.Summary.Dropped, left(res)...)
	return m, "", nil
}

// left lists what consolidation left out, each with its reason.
func left(res consolidate.Result) []initbranch.Drop {
	var out []initbranch.Drop
	rec, add := res.Record, func(what, value, format string, a ...any) {
		out = append(out, initbranch.Drop{What: what, Value: value, Reason: fmt.Sprintf(format, a...)})
	}
	terms := slices.Clone(rec.TermConflicts)
	for _, m := range rec.Merged {
		terms = append(terms, m.TermConflicts...)
		for _, o := range m.DroppedOverviews {
			add("overview", o.Capability, "merged into %s, whose overview is kept: %s", m.Name, oneLine(o.Text))
		}
	}
	for _, t := range terms {
		add("term", t.Capability+": "+t.Name, "defined differently in %s, whose definition is kept; this one read: %s", t.KeptIn, oneLine(t.Text))
	}
	for _, d := range rec.Dropped {
		add("question", d.ID, "answered by %s", d.AnsweredBy)
	}
	for _, e := range slices.Concat(rec.ExactMerges, rec.EntryMerges) {
		add("entry", strings.Join(e.Merged, ", "), "merged into %s", e.Kept)
	}
	for _, l := range rec.Lifts {
		add("rule", strings.Join(l.With, ", "), "repeats %s, which the product's rules now hold", l.Kept)
	}
	for _, f := range rec.Refused {
		add("refused "+f.Step, f.Item, "%s", f.Reason)
	}
	return out
}

// oneLine is text on one line.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }
