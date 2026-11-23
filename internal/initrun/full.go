package initrun

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/fit"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/newcodebase"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
)

// full is bare physa init (design 02 section 2.1): it prints the
// appetite's plan before any token is spent, then gathers, with a discovery
// agent capped at a quarter of the appetite (at most discovery's default), and
// for a new codebase takes the person's answers instead; otherwise it
// proposes with half the appetite (at most propose's default), recovers with
// what discovery and proposing left, and with --write writes physa/init.
// Every run starts fresh: a new gather and proposal replace the stored ones
// and their recoveries.
func full(o Flags, st *store.Store, root string, p *printer, stderr io.Writer, env Env) int {
	appetite := o.Appetite
	if appetite == 0 {
		appetite = recovery.DefaultAppetite
	}
	limit := appetite / 4                             // a share of the appetite, never a fixed cap (10 Q3)
	share := min(appetite/2, mapping.DefaultAppetite) // recovering gets the rest, less what discovery and proposing used
	budget, err := fit.Budget(share, mapping.Overhead())
	if err != nil {
		return failed(stderr, err)
	}
	fmt.Fprintf(stderr, "physa init: appetite %s: gather up to %d (the discovery agent); propose up to %d; recover the rest, about %d, shared by its sessions; check and write 0.\n",
		plural(int(appetite), "token", "tokens"), limit, share, appetite-limit-share)
	g, sum, err := gatherInto(st, o, env, discover(o, env, limit, "", stderr))
	if err != nil {
		return failed(stderr, err)
	}
	p.gather(sum)
	if newcodebase.New(g) {
		return newCodebase(g, root, o, env, p, stderr)
	}
	fmt.Fprintf(stderr, "physa init: propose's prompt is about %d tokens plus %d for the reply.\n",
		fit.Tokens(mapping.Prompt(fit.Fit(g, budget))), fit.Propose.ReplyReserve)
	res, err := mapping.Propose(context.Background(), st, mapping.Session{
		Agent:    func() (adapter.Adapter, error) { return env.Agent(o.Agent) },
		Appetite: share, Progress: stderr, Now: env.Now,
	})
	if err != nil {
		return failed(stderr, err)
	}
	if res.Stopped {
		fmt.Fprintf(stderr, "physa init: the circuit breaker stopped proposing at %s; nothing was proposed or written\n", plural(int(res.Appetite), "token", "tokens"))
		return 1
	}
	p.proposal(res)
	spent := sum.Discovery.Tokens.Input + sum.Discovery.Tokens.Output + res.Tokens.Input + res.Tokens.Output
	rest := max(appetite-spent, 1) // zero would mean recovery's default; one reaches no capability
	rep, err := recovery.Run(context.Background(), st, recoverySession(o, rest, nil, root, env, stderr))
	if err != nil {
		return failed(stderr, err)
	}
	p.recovery(rep)
	code := exitCode(rep)
	if o.Write {
		code = max(code, writeStored(st, root, o, env, p, stderr))
	}
	return code
}

// recoverySession is how recovering runs under appetite: on the
// capabilities of only (none means all), from the repo at root.
func recoverySession(o Flags, appetite int64, only []string, root string, env Env, stderr io.Writer) recovery.Session {
	s := recovery.Session{
		Agent:    func() (adapter.Adapter, error) { return env.Agent(o.Agent) },
		Appetite: appetite, Only: only, Root: root,
		OpenRepo: env.Repo, Progress: stderr, Now: env.Now,
	}
	if env.PRExists != nil {
		s.PRExists = env.PRExists(".")
	}
	return s
}

// newCodebase starts a new codebase's map from the person's answers, read
// from --answers or asked on a terminal; with neither it fails, saying how
// to answer. It resolves no agent. --write writes the promise and terms.
func newCodebase(g store.Gather, root string, o Flags, env Env, p *printer, stderr io.Writer) int {
	qs := newcodebase.Questions(g, filepath.Base(root))
	var a newcodebase.Answers
	var err error
	switch {
	case o.Answers != "":
		a, err = newcodebase.ReadAnswers(o.Answers, qs...)
	case env.IsTerminal != nil && env.IsTerminal() && env.Stdin != nil:
		a, err = newcodebase.Ask(env.Stdin, stderr, qs)
	default:
		fmt.Fprintf(stderr, "physa init: this looks like a new codebase (no agent docs, contributing guide, decisions or PRs, and %s); answer its questions on a terminal or with --answers FILE\n",
			plural(len(g.Changes), "change", "changes"))
		return 2
	}
	if err != nil {
		return failed(stderr, err)
	}
	p.newCodebase(a, o.Write)
	if !o.Write {
		return 0
	}
	return writeMap(newMap(a, env), o, env, p, stderr)
}
