package initrun

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/newcodebase"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
)

// full is bare physa init (design 02 section 2.1): it prints the
// appetite's plan before any token is spent, then gathers, with a discovery
// agent capped at a quarter of the appetite, and for a new codebase takes
// the person's answers instead; otherwise it proposes with what discovery
// left, recovers with what both left, and with --write writes physa/init.
// A phase whose whole prompt does not fit what is left never starts (ended).
// Every run starts fresh: it replaces the stored gather, proposal and recoveries.
func full(o Flags, st *store.Store, root string, p *printer, stderr io.Writer, env Env) int {
	appetite := cmp.Or(o.Appetite, recovery.DefaultAppetite)
	limit := appetite / 4 // a share of the appetite, never a fixed cap (10 Q3)
	fmt.Fprintf(stderr, "physa init: appetite %s: gather up to %d (the discovery agent); propose with what gather leaves; recover with what is left after both, shared by its sessions; check and write 0. No source is cut: a phase that does not fit what is left is not started.\n", plural(int(appetite), "token", "tokens"), limit)
	g, sum, err := gatherInto(st, o, env, discover(o, env, limit, "", stderr))
	if err != nil {
		return failed(stderr, err)
	}
	p.gather(sum)
	if newcodebase.New(g) {
		return newCodebase(g, root, o, env, p, stderr)
	}
	left := max(appetite-sum.Discovery.Tokens.Input-sum.Discovery.Tokens.Output, 1) // zero would mean propose's default
	fmt.Fprintf(stderr, "physa init: propose's prompt is about %d tokens plus %d for the reply; %d of the appetite is left.\n", mapping.Estimate(g), mapping.ReplyReserve, left)
	res, err := mapping.Propose(context.Background(), st, mapping.Session{
		Agent:    func() (adapter.Adapter, error) { return env.Agent(o.Agent) },
		Appetite: left, Progress: stderr, Now: env.Now,
	})
	if err != nil {
		return ended(err, o, nil, stderr)
	}
	if res.Stopped {
		fmt.Fprintf(stderr, "physa init: the circuit breaker stopped proposing at %s; nothing was proposed or written\n", plural(int(res.Appetite), "token", "tokens"))
		return 1
	}
	p.proposal(res)
	rest := max(left-res.Tokens.Input-res.Tokens.Output, 1) // zero would mean recovery's default; one reaches no capability
	rep, err := recovery.Run(context.Background(), st, recoverySession(o, rest, nil, root, env, stderr))
	if err != nil {
		return ended(err, o, nil, stderr)
	}
	p.recovery(rep)
	code := exitCode(rep)
	if o.Write {
		code = max(code, writeStored(st, root, o, env, p, stderr))
	}
	return code
}

// ended reports err and returns the run's code: 1 for an appetite too small
// for a phase's whole prompt, saying what is stored and the command that
// continues under the appetite needed; 2 for any other err.
func ended(err error, o Flags, only []string, stderr io.Writer) int {
	var p *mapping.TooSmall
	var r *recovery.TooSmall
	switch {
	case errors.As(err, &p) && o.Full():
		fmt.Fprintf(stderr, "physa init: stopped before proposing: %v. The gather is stored: %s continues, then %s.\n", err, continuing("--propose", nil, p.Need(), o), continuing("--recover", nil, 0, o))
	case errors.As(err, &p):
		fmt.Fprintf(stderr, "physa init: stopped before proposing: %v. %s continues.\n", err, continuing("--propose", nil, p.Need(), o))
	case errors.As(err, &r):
		fmt.Fprintf(stderr, "physa init: stopped before recovering: %v. The proposal is stored: %s continues.\n", err, continuing("--recover", only, r.Need, o))
	default:
		return failed(stderr, err)
	}
	return 1
}

// continuing is the command that runs phase under need (none when zero), with
// o's agent, the capabilities of only and, for --recover, o's writing.
func continuing(phase string, only []string, need int64, o Flags) string {
	cmd := []string{"physa init", phase}
	if len(only) > 0 {
		cmd = append(cmd, "--only", strings.Join(only, ","))
	}
	if need > 0 {
		cmd = append(cmd, "--appetite", strconv.FormatInt(need, 10))
	}
	if o.Agent != "" {
		cmd = append(cmd, "--agent", o.Agent)
	}
	for i, on := range []bool{o.Write, o.Push, o.PR, o.Hooks} {
		if on && phase == "--recover" {
			cmd = append(cmd, [...]string{"--write", "--push", "--pr", "--hooks"}[i])
		}
	}
	return strings.Join(cmd, " ")
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
