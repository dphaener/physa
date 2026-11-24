package initrun

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/initcost"
	"github.com/dphaener/physa/internal/newcodebase"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
)

// full is bare physa init (design 02 section 2.1). Init is never bounded
// (design 10 Q6): it says so before any token is spent, then gathers, with the discovery agent,
// and for a new codebase takes the person's answers instead. Otherwise it estimates the rest of
// the run and asks to go on (initcost), or stops, before spending on propose, when a source is
// larger than one share of it (shares.TooLarge); with a yes it proposes, recovers, saying once if
// the spend passes the top of the estimate, records what proposing and recovering spent, weighed,
// against their prompt and sources for the next estimate, and with --write writes physa/init.
// Every run starts fresh: it replaces the stored gather, proposal and recoveries.
func (r *run) full() int {
	r.agent() // resolved here so the first line can say what a token is worth; gather reports a failure
	fmt.Fprintf(r.stderr, "physa init: no appetite and no circuit breaker: init is never bounded, and is up front about its cost instead: once it has gathered it estimates the rest of the run and asks before going on. %s\n", initcost.Unit(r.cache, r.meter.Name()))
	g, sum, err := r.gatherInto("")
	if err != nil {
		return failed(r.stderr, err)
	}
	r.p.gather(sum)
	if newcodebase.New(g) {
		return r.newCodebase(g)
	}
	if _, err := r.agent(); err != nil {
		return failed(r.stderr, err)
	}
	est, err := initcost.For(r.cache, r.meter, g, sum.EstimatedTokens)
	if err != nil {
		return failed(r.stderr, err)
	}
	fmt.Fprintf(r.stderr, "physa init: %s\n", est.Line())
	ask := initcost.Ask{Yes: r.o.Yes, Rerun: strings.Join(append([]string{"physa init"}, append(r.args, "--yes")...), " ")}
	if r.env.IsTerminal != nil && r.env.IsTerminal() {
		ask.Confirm = r.env.Confirm
	}
	if !initcost.Proceed(r.stderr, est, ask) {
		return 1
	}
	initcost.WarnPast(r.stderr, r.meter, est.Hi)
	if code := r.propose(); code != 0 {
		return code
	}
	return r.recoverPhase(nil, func(recovery.Report) {
		if p, rc := r.counts["propose"], r.counts["recover"]; !p.Spend.Unpriced && !rc.Spend.Unpriced { // raw tokens would mix units
			r.cache.RecordRatio(rc.Spend.Tokens, int64(sum.EstimatedTokens), p.Spend.Tokens, p.Raw.Input+p.Raw.Output, r.env.Now())
		}
		fmt.Fprintf(r.stderr, "physa init: this run spent %s.\n", r.meter.Spent())
	})
}

// recoverySession is how recovering runs: on the capabilities of only (none
// means all), from the repo at root.
func (r *run) recoverySession(only []string) recovery.Session {
	s := recovery.Session{Agent: r.agent, Only: only, Root: r.root, OpenRepo: r.env.Repo, Progress: r.stderr, Now: r.env.Now}
	if r.env.PRExists != nil {
		s.PRExists = r.env.PRExists(".")
	}
	return s
}

// newCodebase starts a new codebase's map from the person's answers, read
// from --answers or asked on a terminal; with neither it fails, saying how
// to answer. It starts no session. --write writes the promise and terms.
func (r *run) newCodebase(g store.Gather) int {
	qs := newcodebase.Questions(g, filepath.Base(r.root))
	var a newcodebase.Answers
	var err error
	switch {
	case r.o.Answers != "":
		a, err = newcodebase.ReadAnswers(r.o.Answers, qs...)
	case r.env.IsTerminal != nil && r.env.IsTerminal() && r.env.Stdin != nil:
		a, err = newcodebase.Ask(r.env.Stdin, r.stderr, qs)
	default:
		fmt.Fprintf(r.stderr, "physa init: this looks like a new codebase (no agent docs, contributing guide, decisions or PRs, and %s); answer its questions on a terminal or with --answers FILE\n",
			plural(len(g.Changes), "change", "changes"))
		return 2
	}
	if err != nil {
		return failed(r.stderr, err)
	}
	r.p.newCodebase(a, r.o.Write)
	if !r.o.Write {
		return 0
	}
	return writeMap(newMap(a, r.env), r.o, r.env, r.p, r.stderr)
}
