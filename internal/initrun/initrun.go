package initrun

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/coverage"
	"github.com/dphaener/physa/internal/discovery"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/initbranch"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/preflight"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/recoveryreport"
	"github.com/dphaener/physa/internal/shares"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Env is what init reaches outside itself through, each a seam the caller
// owns: the store, repo, forge, writer and hooks of the repo at a directory,
// the agent a name selects (empty for the default), the clock, and the
// person's input, where a new codebase's answers are asked, and Confirm asks
// whether to go on past the estimate, when IsTerminal. PRExists resolves #N
// anchors; nil checks them for form only. Spend is the file of the
// machine's spend cache (spend.Path); empty keeps prices for the run only.
// Skill is the line saying the agent a name selects has no current Physa
// skill, empty for none; nil says nothing.
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
	Confirm    func(question string) (bool, error)
	Spend      string
	Skill      func(agent string) string
}

// Run runs physa init on the repo in the current directory: the phase named,
// or every phase in order; --write then writes the map to physa/init. No
// phase has an appetite or a circuit breaker (design 10 Q6). Every session
// is weighed through a spend.Meter, and the prices it learns are saved to
// the machine's spend cache. It returns 0 when all asked was done, 1 when
// it stopped before proposing, for want of a yes or for a source larger than
// one share of propose, or some capabilities are not yet recovered (still written with --write), 2 when it
// could not (a capability failed, the map failed its check, the hook was
// refused), and command.BadUsage for a bad line. o is the line's flags as
// Declare read them, args its positionals and words the words typed after
// init, which a rerun hint repeats.
func Run(o Flags, args, words []string, stdout, stderr io.Writer, env Env) int {
	if !o.read(args, env.Now()) {
		return command.BadUsage
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
	cache := spend.Open(env.Spend)
	defer func() {
		if err := cache.Save(); err != nil {
			fmt.Fprintf(stderr, "physa init: the spend cache was not saved: %v\n", err)
		}
	}()
	r := &run{o: o, env: env, st: st, root: root, args: words, stderr: stderr, cache: cache, p: newPrinter(o, stdout), counts: map[string]spend.Count{}}
	code := 0
	switch {
	case o.Propose:
		code = r.propose()
	case o.Recover:
		code = r.recoverPhase(r.o.Only, r.record)
	case o.Full():
		code = r.full()
	default:
		if off == "" {
			fmt.Fprintln(stderr, "physa init: gather, with the discovery agent; init is never bounded, so it has no appetite and no circuit breaker")
		} else {
			fmt.Fprintf(stderr, "physa init: gather starts no agent (%s)\n", off)
		}
		var sum gather.Summary
		if _, sum, err = r.gatherInto(off); err != nil {
			return failed(stderr, err)
		}
		r.p.gather(sum)
		preflight.Warn(stderr, preflight.Gather(sum))
	}
	code = r.p.done(code, stderr)
	if r.skill != "" { // said last, so it is the line left on the screen; it never changes the code
		fmt.Fprintf(stderr, "physa init: %s\n", r.skill)
	}
	return code
}

// run is one physa init: its flags, seams, store, root, arguments, output
// and spend cache, and the agent once resolved, weighed through a meter.
type run struct {
	o      Flags
	env    Env
	st     *store.Store
	root   string
	args   []string
	stderr io.Writer
	cache  *spend.Cache
	p      *printer
	skill  string // the full run's line on the agent's Physa skill, empty for none

	once   sync.Once
	meter  *spend.Meter
	err    error
	counts map[string]spend.Count // what each phase spent in this run
}

// agent resolves o's agent once, weighed through a meter on the cache.
func (r *run) agent() (adapter.Adapter, error) {
	r.once.Do(func() {
		a, err := r.env.Agent(r.o.Agent)
		r.meter, r.err = &spend.Meter{Adapter: a, Cache: r.cache, Now: r.env.Now}, err
		if err == nil { // said before the first session, so nothing is spent on a setup that fails
			preflight.Warn(r.stderr, adapter.Warnings(a))
		}
	})
	if r.err != nil {
		return nil, r.err
	}
	return r.meter, nil
}

// spent stores what the meter counted since from as phase's spend, so the
// PR body shows what each phase really spent; nothing spent stores nothing,
// and a failure to store it is said and does not stop the run.
func (r *run) spent(phase string, from spend.Count) {
	if c := r.meter.Count().Since(from); c != (spend.Count{}) {
		r.counts[phase] = c
		if err := r.st.AddSpent(store.Spent{Phase: phase, Count: c}); err != nil {
			fmt.Fprintf(r.stderr, "physa init: what %s spent was not stored: %v\n", phase, err)
		}
	}
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

// gatherInto gathers the repo's sources under o's bounds, with the
// discovery agent unless off says why not, and stores them, replacing the
// earlier gather, and says what it gathered.
func (r *run) gatherInto(off string) (store.Gather, gather.Summary, error) {
	from, d := r.meter.Count(), gather.Discover{Off: off}
	if off == "" {
		d.Session = discovery.Session{Agent: r.agent, Progress: r.stderr}
	}
	repo, err := r.env.Repo(".")
	if err != nil {
		return store.Gather{}, gather.Summary{}, err
	}
	dir, err := repo.StoreDir()
	if err != nil {
		return store.Gather{}, gather.Summary{}, err
	}
	bounds := gather.Bounds{Since: r.o.Since, SinceSet: r.o.SinceSet, MaxPRs: r.o.MaxPRs}
	g, sum, err := gather.Run(".", repo, r.env.Forge("."), bounds, d, r.env.Now())
	if err != nil {
		return g, sum, err
	}
	sum.Store = dir
	if err := r.st.PutGather(g); err != nil {
		return g, sum, err
	}
	r.spent("discover", from)
	return g, sum, nil
}

// propose proposes a map from the stored gather, in one session or in shares, and prints it.
func (r *run) propose() int {
	from := r.meter.Count()
	res, err := shares.Propose(context.Background(), r.st, mapping.Session{Agent: r.agent, Progress: r.stderr, Now: r.env.Now})
	r.spent("propose", from)
	if err != nil {
		return failed(r.stderr, err)
	}
	r.p.proposal(res)
	if g, ok, err := r.st.Gathered(); err == nil && ok { // what recovery reads beyond the citations, and what no capability got
		_, c := coverage.Place(g, res.Proposal.Capabilities)
		if coverage.Warn(r.stderr, c); !r.p.json || r.p.multi { // --propose --json prints the proposal alone
			r.p.value("coverage", coverage.Text(c), c)
		}
	}
	return 0
}

// recoverPhase recovers the proposed capabilities into the store and prints
// how each fared. Without --write it writes nothing to the repo; with it,
// it then writes everything stored to physa/init, keeping what an
// earlier run recovered. done, when set, is called with the report first.
func (r *run) recoverPhase(only []string, done func(recovery.Report)) int {
	from := r.meter.Count()
	rep, err := recovery.Run(context.Background(), r.st, r.recoverySession(only))
	r.spent("recover", from)
	if err != nil {
		return failed(r.stderr, err)
	}
	if done != nil {
		done(rep)
	}
	r.p.recovery(rep)
	code := recoveryreport.ExitCode(rep)
	if r.o.Write {
		code = max(code, r.writeStored())
	}
	return code
}

// failed prints err as init's failure and returns 2, or 1 for a stop before spending on propose.
func failed(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "physa init: %v\n", err)
	if _, ok := err.(*shares.TooLarge); ok { // found before any share ran; after a halving it comes wrapped
		return 1
	}
	return 2
}
