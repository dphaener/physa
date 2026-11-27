package initrun

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/dphaener/physa/internal/initcost"
	"github.com/dphaener/physa/internal/initreport"
	"github.com/dphaener/physa/internal/interrupt"
	"github.com/dphaener/physa/internal/newcodebase"
	"github.com/dphaener/physa/internal/preflight"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
)

// full is bare physa init (design 15 section 3). Init is never bounded (design 10 Q6) unless given
// a --cap: it says so, or the cap, before any token is spent, then gathers, with the discovery
// agent, and for a new codebase takes the person's answers instead. Otherwise it runs areas, walk,
// place, recover and consolidate in order, each as the phase alone does, says what the run spent,
// and with --write writes physa/init once, consolidated; without, init's report ends it. Every run
// starts fresh: the gather clears what was stored. A phase that fails, or that the cap or an
// interrupt stops, ends the run there with nothing written, and r.phase names it for the line that
// continues. Capabilities that failed in a recovery that was not stopped do not end it, and a
// consolidation that could not run leaves the map as recovered, said, with exit code 2.
// Before it writes, it asks Env.Skill for the line on the agent's Physa skill, which the PR body carries and Run says last.
func (r *run) full(ctx context.Context) int {
	if r.env.Skill != nil {
		r.skill = r.env.Skill(r.o.Agent)
	}
	// Resolved here so the first line can say what a token is worth. The
	// failure is kept in r.err, and the gather's discovery session, which asks
	// r.agent again, reports it.
	_, _ = r.agent()
	fmt.Fprintf(r.stderr, "physa init: %s %s\n", r.guard.StartLine(), initcost.Unit(r.cache, r.meter.Name()))
	g, sum, err := r.gatherInto(ctx, "")
	if err != nil {
		return r.fail(ctx, interrupt.Nothing("the gather"), err)
	}
	r.p.gather(sum)
	if newcodebase.New(g) {
		return r.newCodebase(ctx, g)
	}
	preflight.Warn(r.stderr, preflight.Gather(sum))
	if _, err := r.agent(); err != nil {
		return failed(r.stderr, err)
	}
	for _, step := range []struct {
		phase string
		run   func(context.Context) int
	}{{"areas", r.areas}, {"walk", r.walk}, {"place", r.place}} {
		r.phase = step.phase
		if code := step.run(ctx); code != 0 {
			return code
		}
	}
	r.phase = "recover"
	code, ok := r.recovered(ctx, nil)
	if !ok || r.guard.Stopped() { // stopped: nothing is written, and the lines that continue write once
		return code
	}
	r.phase = "consolidate"
	ccode, ok := r.consolidated(ctx)
	if !ok && (r.kept != "" || r.guard.Stopped()) {
		return ccode
	} else if !ok {
		fmt.Fprintln(r.stderr, "physa init: consolidation could not run, so the map stays as recovered")
	}
	code = max(code, ccode)
	fmt.Fprintf(r.stderr, "physa init: this run spent %s.\n", r.o.Cap.Show(r.meter.Spent())) // in the cap's unit
	if r.o.Write {
		return max(code, r.writeStored(ctx))
	}
	r.p.value(initreport.Part(ctx, r.st))
	return code
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
func (r *run) newCodebase(ctx context.Context, g store.Gather) int {
	terminal := r.env.IsTerminal != nil && r.env.IsTerminal() && r.env.Stdin != nil
	a, err := newcodebase.Obtain(g, filepath.Base(r.root), r.o.Answers, terminal, r.env.Stdin, r.stderr)
	if err != nil {
		return failed(r.stderr, err)
	}
	r.p.newCodebase(a, r.o.Write)
	if !r.o.Write {
		return 0
	}
	return writeMap(ctx, newMap(a, r.env, r.skill), nil, r.o, r.env, r.p, r.stderr)
}
