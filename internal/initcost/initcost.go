// Package initcost is init being up front about its cost, since it is never
// bounded (design 10 Q6, Q10, Q10a): what a token of its spend is worth, the
// estimate of the rest of a run once it has gathered, with propose in one
// session or in shares (design 02 §2.3.1), the yes it waits for, the one
// warning when the spend passes the estimate's top, and the table of what
// each phase spent. Spend reads in weighted tokens first, with the cost
// estimate beside it when prices are known.
package initcost

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/shares"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/store"
)

// Unit says what a token of a run of the adapter named agent is worth: an
// input token of its default model, at its cached price, or a raw token
// while no price is known.
func Unit(c *spend.Cache, agent string) string {
	if model, p, ok := c.Reference(agent); ok {
		return fmt.Sprintf("Spend is shown in tokens weighted by price, each worth an input token of %s (%s a million), with the cost estimate beside it.", model, spend.Dollars(p.PerMillion))
	}
	return "Spend is shown in tokens; no prices are known yet, so they count raw and no cost is shown."
}

// Estimate is what the rest of a run is expected to spend, in weighted
// tokens, as a range measured from past runs: propose's prompt and reply
// reserve, which are known, times propose's weighted-to-raw factor, plus
// the sources' estimated tokens times recovery's weighted-to-sources ratio.
// Propose in shares counts every share's prompt and the merge's, and a reply
// reserve for each.
// A one-turn session writes its whole prompt to a 1-hour cache at twice
// input, so both are measured, never assumed. It is never a limit.
type Estimate struct {
	// Discovery is what discovery spent.
	Discovery spend.Spend
	// Prompt and Reply are propose's prompt and reply reserve, and Sources
	// the gathered sources, in estimated tokens.
	Prompt, Reply, Sources int
	// Shares is the shares propose reads its sources in, 0 for one session.
	Shares int
	// Ratios are the factors' ranges and the runs they come from.
	Ratios spend.Ratios
	// Lo and Hi are the range, weighed.
	Lo, Hi spend.Spend
}

// New estimates the rest of a run of the adapter named agent with c's
// prices and ratios.
func New(c *spend.Cache, agent string, discovery spend.Spend, prompt, reply, sources int) Estimate {
	e := Estimate{Discovery: discovery, Prompt: prompt, Reply: reply, Sources: sources, Ratios: c.RatioRange()}
	known, src := int64(prompt+reply), int64(sources)
	e.Lo = c.Input(agent, (known*e.Ratios.ProposeLo+src*e.Ratios.RecoverLo)/1000)
	e.Hi = c.Input(agent, (known*e.Ratios.ProposeHi+src*e.Ratios.RecoverHi)/1000)
	return e
}

// For estimates the rest of a run from gather g, whose sources take sources
// estimated tokens, after discovery spent what m has counted: propose in
// one session, or in the shares it needs within the window m's agent states
// (shares.Estimate). A source larger than one share is a *shares.TooLarge,
// returned before anything is spent on propose.
func For(c *spend.Cache, m *spend.Meter, g store.Gather, sources int) (Estimate, error) {
	prompt, reply, k, err := shares.Estimate(g, adapter.Window(m))
	if err != nil {
		return Estimate{}, err
	}
	e := New(c, m.Name(), m.Spent(), prompt, reply, sources)
	e.Shares = k
	return e, nil
}

// Range reads like "1.9M to 5.6M tokens (about $7.60 to $23)".
func (e Estimate) Range() string { return spend.Range(e.Lo, e.Hi) }

// Line is the estimate as init prints it.
func (e Estimate) Line() string {
	from := "nebari's measured ratio of 12.5, until two runs are recorded on this machine"
	if e.Ratios.Measured > 0 {
		from = fmt.Sprintf("the smallest and largest of %d runs recorded on this machine", e.Ratios.Measured)
	}
	r := e.Ratios
	propose := fmt.Sprintf("propose's prompt of about %s tokens plus %s for its reply", spend.Tokens(int64(e.Prompt)), spend.Tokens(int64(e.Reply)))
	if e.Shares > 0 {
		propose = fmt.Sprintf("propose's %d shares and their merge, prompts of about %s tokens plus %s for their replies", e.Shares, spend.Tokens(int64(e.Prompt)), spend.Tokens(int64(e.Reply)))
	}
	return fmt.Sprintf("discovery spent %s. The rest of the run should spend about %s: %s, at %s times as weighed, and recovering at %s times the sources' %s tokens (%s). An estimate, not a limit.",
		e.Discovery, e.Range(), propose, span(r.ProposeLo, r.ProposeHi), span(r.RecoverLo, r.RecoverHi), spend.Tokens(int64(e.Sources)), from)
}

// span is a range of ratios in thousandths: "2.05", or "6.25 to 18.75".
func span(lo, hi int64) string {
	if lo == hi {
		return times(lo)
	}
	return times(lo) + " to " + times(hi)
}

// times is a ratio in thousandths, shown to two decimals at most: "4",
// "6.5", "2.95".
func times(milli int64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", float64(milli)/1000), "0"), ".")
}

// Ask says how a run may go on past its estimate.
type Ask struct {
	// Yes goes on without asking (--yes).
	Yes bool
	// Confirm asks the person; nil when there is no terminal to ask on.
	Confirm func(question string) (bool, error)
	// Rerun is the command that goes on without asking.
	Rerun string
}

// Proceed reports whether a run goes on past e: Yes goes on, Confirm asks,
// and without either the run stops, naming e and a.Rerun. When it stops,
// for a no or a question that could not be asked, it says why on w.
func Proceed(w io.Writer, e Estimate, a Ask) bool {
	switch {
	case a.Yes:
		return true
	case a.Confirm != nil:
		yes, err := a.Confirm(fmt.Sprintf("Go on to propose and recover, about %s?", e.Range()))
		if err != nil {
			fmt.Fprintf(w, "physa init: stopped before proposing: the question could not be asked: %v. The gather is stored; rerun with --yes to go on without asking: %s\n", err, a.Rerun)
		} else if !yes {
			fmt.Fprintln(w, "physa init: stopped before proposing, as asked; nothing was spent on proposing or recovering. The gather is stored: physa init --propose continues, then physa init --recover.")
		}
		return err == nil && yes
	}
	fmt.Fprintf(w, "physa init: stopped before proposing: the rest of the run is estimated at about %s, and with no terminal to ask on, init goes on only when told to. The gather is stored; rerun with --yes to go on without asking: %s\n", e.Range(), a.Rerun)
	return false
}

// WarnPast makes m say once on w, as its sessions end, when what it spends
// from now passes top, the top of the estimate. The run goes on.
func WarnPast(w io.Writer, m *spend.Meter, top spend.Spend) {
	from := m.Spent()
	var once sync.Once
	m.Ended = func(total spend.Spend) {
		since := spend.Spend{Tokens: total.Tokens - from.Tokens, MicroUSD: total.MicroUSD - from.MicroUSD, Unpriced: total.Unpriced}
		if since.Tokens > top.Tokens {
			once.Do(func() {
				fmt.Fprintf(w, "physa init: past the estimate: proposing and recovering have spent %s, more than its top of %s; init is never bounded, so it goes on.\n", since, top)
			})
		}
	}
}

// Row is one phase's tokens, raw and Weighted.
type Row struct {
	Phase                      string
	Input, CachedInput, Output int64
	Weighted                   spend.Spend
}

// Table is rows as a markdown table, with their total, the raw counts
// beside the weighted tokens and the cost estimate.
func Table(rows []Row) string {
	var b strings.Builder
	b.WriteString("| Phase | Input | Cached input | Output | Weighted | Cost estimate |\n|---|---:|---:|---:|---:|---:|\n")
	total := Row{Phase: "total"}
	for _, t := range rows {
		total.Input, total.CachedInput, total.Output, total.Weighted = total.Input+t.Input, total.CachedInput+t.CachedInput, total.Output+t.Output, total.Weighted.Add(t.Weighted)
	}
	for _, t := range append(rows, total) {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %s | %s |\n", t.Phase, t.Input, t.CachedInput, t.Output, spend.Tokens(t.Weighted.Tokens), t.Weighted.Cost())
	}
	return b.String()
}
