package initrun

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/discovery"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Env is what init reaches outside itself through, each a seam the caller
// owns: the store, repo, forge, writer and hooks of the repo at a directory,
// the agent a name selects (empty for the default), the clock, and the
// person's input, where a new codebase's answers are asked when IsTerminal.
// PRExists resolves #N anchors; nil checks them for form only.
type Env struct {
	Store      func(dir string) (*store.Store, error)
	Repo       func(dir string) (vcs.Repo, error)
	Forge      func(dir string) forge.Forge
	PRExists   func(dir string) func(int) (bool, error)
	Agent      func(name string) (adapter.Adapter, error)
	Now        func() time.Time
	Writer     func(dir string) (vcs.Writer, error)
	Hooks      func(dir string) (vcs.Hooks, error)
	Stdin      io.Reader
	IsTerminal func() bool
}

// Run runs physa init on the repo in the current directory: the phase named,
// or every phase in order under one appetite; --write then writes the map to
// physa/init. It returns 0 when all asked was done, 1 when the circuit
// breaker stopped proposing or some capabilities are not yet recovered
// (still written with --write), 2 when it could not (a capability failed,
// the map failed its check, the hook was refused), and cli.BadUsage for
// arguments not understood.
func Run(args []string, stdout, stderr io.Writer, env Env) int {
	o, ok := ParseFlags(args, env.Now())
	if !ok {
		return cli.BadUsage
	}
	root, err := filepath.Abs(".")
	if err != nil {
		return failed(stderr, err)
	}
	off := ""
	if o.Gather { // --gather alone runs on a mapped repo too, but starts no agent there
		off = discoveryOff(env)
	} else if err := refuseMapped(env); err != nil {
		return failed(stderr, err)
	}
	st, err := env.Store(".")
	if err != nil {
		return failed(stderr, err)
	}
	defer st.Close()
	p, code := newPrinter(o, stdout), 0
	switch {
	case o.Propose:
		code = propose(o, st, p, stderr, env)
	case o.Recover:
		code = recoverPhase(o, st, root, p, stderr, env)
	case o.Full():
		code = full(o, st, root, p, stderr, env)
	default:
		limit := int64(discovery.DefaultAppetite)
		if o.Appetite != 0 {
			limit = min(limit, o.Appetite)
		}
		if off == "" {
			fmt.Fprintf(stderr, "physa init: gather up to %d tokens (the discovery agent)\n", limit)
		} else {
			fmt.Fprintf(stderr, "physa init: gather starts no agent (%s)\n", off)
		}
		var sum gather.Summary
		if _, sum, err = gatherInto(st, o, env, discover(o, env, limit, off, stderr)); err != nil {
			return failed(stderr, err)
		}
		p.gather(sum)
	}
	return p.done(code, stderr)
}

// refuseMapped is the early refusal: it errors, before anything is spent,
// when the default branch already holds a map in either layout
// (mapfolder.Products), naming the file found. With no default branch
// to be found it checks the working tree instead, and leaves a missing
// default branch for --write to report.
func refuseMapped(env Env) error {
	w, err := env.Writer(".")
	if err != nil {
		return err
	}
	if _, err := w.DefaultBranch(); err == nil {
		return initbranch.HasMap(w)
	}
	for _, p := range mapfolder.Products {
		if _, err := os.Stat(filepath.FromSlash(p)); err == nil {
			return fmt.Errorf("the repo already has a map (%s in the working tree); init only starts a map, and learning keeps an existing one current: run physa map learn", p)
		}
	}
	return nil
}

// discoveryOff is why --gather alone starts no discovery agent, or empty to
// start one: the repo has a map, by the early refusal's check, or whether it
// has one could not be told.
func discoveryOff(env Env) string {
	switch err := refuseMapped(env); {
	case err == nil:
		return ""
	case strings.Contains(err.Error(), "already has a map"):
		return "discovery not started: the repo has a map"
	default:
		return "discovery not started: could not tell whether the repo has a map: " + err.Error()
	}
}

// discover is gather's discovery agent, capped at limit tokens, or none,
// reported with why off says, when off is set.
func discover(o Flags, env Env, limit int64, off string, stderr io.Writer) gather.Discover {
	if off != "" {
		return gather.Discover{Off: off}
	}
	return gather.Discover{Session: discovery.Session{
		Agent:    func() (adapter.Adapter, error) { return env.Agent(o.Agent) },
		Appetite: limit, Progress: stderr,
	}}
}

// gatherInto gathers the repo's sources under o's bounds, with d's discovery,
// and stores them in st, replacing the earlier gather, and says what it
// gathered.
func gatherInto(st *store.Store, o Flags, env Env, d gather.Discover) (store.Gather, gather.Summary, error) {
	repo, err := env.Repo(".")
	if err != nil {
		return store.Gather{}, gather.Summary{}, err
	}
	dir, err := repo.StoreDir()
	if err != nil {
		return store.Gather{}, gather.Summary{}, err
	}
	bounds := gather.Bounds{Since: o.Since, SinceSet: o.SinceSet, MaxPRs: o.MaxPRs}
	g, sum, err := gather.Run(".", repo, env.Forge("."), bounds, d, env.Now())
	if err != nil {
		return g, sum, err
	}
	sum.Store = dir
	return g, sum, st.PutGather(g)
}

// propose proposes a map from the stored gather and prints it.
func propose(o Flags, st *store.Store, p *printer, stderr io.Writer, env Env) int {
	res, err := mapping.Propose(context.Background(), st, mapping.Session{
		Agent:    func() (adapter.Adapter, error) { return env.Agent(o.Agent) },
		Appetite: o.Appetite,
		Progress: stderr,
	})
	if err != nil {
		return failed(stderr, err)
	}
	if res.Stopped {
		fmt.Fprintf(stderr, "physa init: the circuit breaker stopped the session at %s; nothing was proposed\n", plural(int(res.Appetite), "token", "tokens"))
		return 1
	}
	p.proposal(res)
	return 0
}

// recoverPhase recovers the proposed capabilities into the store and prints
// how each fared. Without --write it writes nothing to the repo; with it,
// it then writes everything stored to physa/init, keeping what an
// earlier run recovered.
func recoverPhase(o Flags, st *store.Store, root string, p *printer, stderr io.Writer, env Env) int {
	rep, err := recovery.Run(context.Background(), st, recoverySession(o, o.Appetite, o.Only, root, env, stderr))
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

// failed prints err as init's failure and returns 2.
func failed(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "physa init: %v\n", err)
	return 2
}
