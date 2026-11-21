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

// full is bare physa init (design 02 section 2.1): it gathers, and for a new
// codebase takes the person's answers instead; otherwise it prints the
// appetite's plan before any token is spent, proposes with half the
// appetite (at most propose's default), recovers with what proposing left,
// and with --write writes physa/init. Every run starts fresh: a new gather
// and proposal replace the stored ones and their recoveries.
func full(o Flags, st *store.Store, root string, p *printer, stderr io.Writer, env Env) int {
	g, sum, err := gatherInto(st, o, env)
	if err != nil {
		return failed(stderr, err)
	}
	p.gather(sum)
	if newcodebase.New(g) {
		return newCodebase(g, root, o, env, p, stderr)
	}
	appetite := o.Appetite
	if appetite == 0 {
		appetite = recovery.DefaultAppetite
	}
	share := min(appetite/2, mapping.DefaultAppetite) // recovering gets the rest, less what proposing used
	line, err := planLine(g, appetite, share)
	if err != nil {
		return failed(stderr, err)
	}
	fmt.Fprintln(stderr, line)
	res, err := mapping.Propose(context.Background(), st, mapping.Session{
		Agent:    func() (adapter.Adapter, error) { return env.Agent(o.Agent) },
		Appetite: share, Progress: stderr, Now: env.Now,
	})
	if err != nil {
		return failed(stderr, err)
	}
	if res.Stopped {
		fmt.Fprintf(stderr, "physa init: the circuit breaker stopped proposing at %d tokens; nothing was proposed or written\n", res.Appetite)
		return 1
	}
	p.proposal(res)
	rest := appetite - res.Tokens.Input - res.Tokens.Output
	rest = max(rest, 1) // zero would mean recovery's default; one reaches no capability
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

// planLine is the appetite's plan. The propose prompt is estimated by
// fitting the gather as proposing will. It errors, before any agent is
// resolved, when the share is too small to propose with.
func planLine(g store.Gather, appetite, share int64) (string, error) {
	budget, err := fit.Budget(share, mapping.Overhead())
	if err != nil {
		return "", err
	}
	prompt := int64(fit.Tokens(mapping.Prompt(fit.Fit(g, budget))))
	reply := int64(fit.Propose.ReplyReserve)
	rest := max(appetite-prompt-reply, 0)
	return fmt.Sprintf("physa init: appetite %d tokens: gather 0 (no agent); propose up to %d (its prompt about %d plus %d for the reply); recover the rest, about %d, shared by its sessions; check and write 0.",
		appetite, share, prompt, reply, rest), nil
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
