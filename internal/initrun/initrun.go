package initrun

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/areas"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/consolidate"
	"github.com/dphaener/physa/internal/coverage"
	"github.com/dphaener/physa/internal/discovery"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/gather/merged"
	"github.com/dphaener/physa/internal/initcap"
	"github.com/dphaener/physa/internal/initrecord"
	"github.com/dphaener/physa/internal/mapped"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/newcodebase"
	"github.com/dphaener/physa/internal/place"
	"github.com/dphaener/physa/internal/preflight"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/recoveryreport"
	"github.com/dphaener/physa/internal/shares"
	"github.com/dphaener/physa/internal/spend"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/walk"
)

// Env is what init reaches outside itself through, each a seam the caller
// owns: the store, repo, forge, writer and hooks of the repo at a directory,
// the agent a name selects (empty for the default), the clock, and the
// person's input, where a new codebase's answers are asked when IsTerminal. PRExists resolves #N
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
	Spend      string
	Skill      func(agent string) string
}

// Run runs physa init on the repo in the current directory: the phase named, or every phase in
// order; --write then writes the map to physa/init. No phase has an appetite or a circuit breaker
// (design 10 Q6); a --cap holds every session to it (initcap). Every session is weighed through a
// spend.Meter, and the prices it learns are saved to the machine's spend cache. It returns 0 when
// all asked was done, 1 when it stopped before proposing for a source larger than one share of
// propose, or before a session that would pass the cap, or some capabilities are not yet recovered
// (still written with --write), or, for --consolidate, a
// session of consolidation was left as it was (see consolidate.Result.ExitCode; a write then still
// happens), 2 when it could not (a capability failed, the map failed its check,
// the hook was refused, the cap needs usage the agent cannot report), and command.BadUsage for a
// bad line. o is the line's flags as Declare read them, args its positionals and words the words
// typed after init, which a rerun hint repeats.
func Run(o Flags, args, words []string, stdout, stderr io.Writer, env Env) int {
	if !o.read(args, env.Now()) {
		if o.bad != nil {
			fmt.Fprintf(stderr, "physa init: %v\n", o.bad)
		}
		return command.BadUsage
	}
	root, err := filepath.Abs(".")
	if err != nil {
		return failed(stderr, err)
	}
	off := ""
	if o.Gather { // --gather alone runs on a mapped repo too, but starts no agent there
		off = mapped.DiscoveryOff(env.Writer)
	} else if err := mapped.Refuse(env.Writer); err != nil {
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
	r := &run{o: o, env: env, st: initrecord.Of(st), root: root, args: words, stderr: stderr, cache: cache, p: newPrinter(o, stdout), counts: map[string]spend.Count{}}
	code := 0
	if o.Cap.Kind != initcap.None { // held to the cap from here on; a code other than 0 ends the run before any phase
		if r.guard, code = initcap.Start(context.Background(), initcap.Run{Cap: o.Cap, Agent: r.agent, Cache: cache, Record: r.st, Fresh: o.Full() || o.Gather, Again: initcap.Again(words, false), Say: !o.Full(), Out: stderr}); code != 0 {
			return code
		}
	}
	if o.Propose || o.Recover || o.Areas || o.Walk || o.Place || o.Consolidate { // the phases on a stored gather say when the forge stopped it, and still run
		preflight.Stored(stderr, st)
	}
	switch {
	case o.Areas:
		code = r.areas()
	case o.Walk:
		code = r.walk()
	case o.Place:
		code = r.place()
	case o.Consolidate:
		code = r.consolidate()
	case o.Propose:
		code = r.propose()
	case o.Recover:
		code = r.recoverPhase(r.o.Only, nil)
	case o.Full():
		code = r.full()
	default:
		if off == "" {
			fmt.Fprintln(stderr, "physa init: gather, with the discovery agent; init has no appetite and no circuit breaker")
		} else {
			fmt.Fprintf(stderr, "physa init: gather starts no agent (%s)\n", off)
		}
		var sum gather.Summary
		if _, sum, err = r.gatherInto(off); err != nil {
			return r.guard.Finish(stderr, r.fail(err), initcap.Again(words, false))
		}
		r.p.gather(sum)
		preflight.Warn(stderr, preflight.Gather(sum))
	}
	code = r.p.done(r.guard.Finish(stderr, code, initcap.Again(words, r.inRec && o.Full())), stderr)
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
	st     *initrecord.Store
	root   string
	args   []string
	stderr io.Writer
	cache  *spend.Cache
	p      *printer
	skill  string         // the full run's line on the agent's Physa skill, empty for none
	guard  *initcap.Guard // the agent sessions start on when capped, nil when not
	inRec  bool           // recovering has started

	once   sync.Once
	meter  *spend.Meter
	err    error
	counts map[string]spend.Count // what each phase spent in this run
}

// agent resolves o's agent once, weighed through a meter on the cache, and
// held to the cap by the guard around the meter once Run made it.
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
	if r.guard != nil {
		return r.guard, nil
	}
	return r.meter, nil
}

// spent stores what the meter counted since from as phase's spend, so the
// PR body shows what each phase really spent; nothing spent stores nothing,
// and a failure to store it is said and does not stop the run.
func (r *run) spent(phase string, from spend.Count) {
	if c := r.meter.Count().Since(from); c != (spend.Count{}) {
		r.counts[phase] = c
		if err := r.st.AddSpent(initrecord.Spent{Phase: phase, Count: c}); err != nil {
			fmt.Fprintf(r.stderr, "physa init: what %s spent was not stored: %v\n", phase, err)
		}
	}
}

// gatherInto gathers the repo's sources under o's bounds, with the
// discovery agent unless off says why not, and stores them, replacing the
// earlier gather, and says what it gathered. A gather the forge stopped
// (*gather.Stopped) is stored like a complete one and returned with that
// error; each wait for the forge's rate limit is said on stderr.
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
	bounds := gather.Bounds{Since: r.o.Since, SinceSet: r.o.SinceSet, MaxPRs: r.o.MaxPRs, Wait: merged.Wait{Progress: r.stderr}}
	g, sum, err := gather.Run(".", repo, r.env.Forge("."), bounds, d, r.env.Now())
	if stop := (*gather.Stopped)(nil); r.guard.Stopped() || err != nil && !errors.As(err, &stop) { // the cap refused discovery: nothing is stored
		return g, sum, cmp.Or(err, adapter.ErrCapped)
	}
	sum.Store = dir
	if perr := r.st.PutGather(g); perr != nil {
		return g, sum, perr
	}
	r.spent("discover", from)
	return g, sum, err // nil, or the stop of a gather now stored
}

// propose proposes a map from the stored gather, in one session or in shares, and prints it.
func (r *run) propose() int {
	from := r.meter.Count()
	res, err := shares.Propose(context.Background(), r.st, mapping.Session{Agent: r.agent, Progress: r.stderr, Now: r.env.Now})
	r.spent("propose", from)
	if err != nil {
		return r.fail(err)
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

// areas divides the stored gather's source files into areas, stores them in init's record and prints them.
func (r *run) areas() int {
	repo, err := r.env.Repo(".")
	if err != nil {
		return failed(r.stderr, err)
	}
	from := r.meter.Count()
	res, err := areas.Run(context.Background(), r.st, areas.Session{Agent: r.agent, Root: r.root, Repo: repo, Progress: r.stderr, Now: r.env.Now})
	r.spent("areas", from)
	if err != nil {
		return r.fail(err)
	}
	r.p.emit("areas", func(w io.Writer) error { return areas.Text(w, res) }, func(w io.Writer) error { return json.NewEncoder(w).Encode(res) })
	return 0
}

// walk names each stored area's capabilities with their footprints, stores them in init's record and prints them.
func (r *run) walk() int {
	from := r.meter.Count()
	res, err := walk.Run(context.Background(), r.st, walk.Session{Agent: r.agent, Root: r.root, Progress: r.stderr, Now: r.env.Now})
	r.spent("walk", from)
	if err != nil {
		return r.fail(err)
	}
	r.p.emit("walk", func(w io.Writer) error { return walk.Text(w, res) }, func(w io.Writer) error { return json.NewEncoder(w).Encode(res) })
	return 0
}

// consolidate consolidates the capabilities recovered from the stored walk, stores the
// consolidation in init's record and prints it; with --write it then writes everything stored.
func (r *run) consolidate() int {
	from := r.meter.Count()
	res, err := consolidate.Run(context.Background(), r.st, consolidate.Session{Agent: r.agent, Stopped: r.guard.Stopped, Root: r.root,
		Product: newcodebase.TitleCase(filepath.Base(r.root)), Progress: r.stderr, Now: r.env.Now})
	r.spent("consolidate", from)
	if err != nil {
		return r.fail(err)
	}
	r.p.emit("consolidation", func(w io.Writer) error { return consolidate.Text(w, res) }, func(w io.Writer) error { return json.NewEncoder(w).Encode(res) })
	code := res.ExitCode()
	if r.o.Write {
		code = max(code, r.writeStored())
	}
	return code
}

// place places the stored gather's PRs and changes on the capabilities by the stored footprints, with no
// session, stores the placements in init's record and prints the counts.
func (r *run) place() int {
	res, err := place.Run(r.st)
	if err != nil {
		return failed(r.stderr, err)
	}
	r.p.emit("place", func(w io.Writer) error { return place.Text(w, res) }, func(w io.Writer) error { return json.NewEncoder(w).Encode(res) })
	return 0
}

// recoverPhase recovers the proposed capabilities into the store and prints
// how each fared. Without --write it writes nothing to the repo; with it,
// it then writes everything stored to physa/init, keeping what an
// earlier run recovered. done, when set, is called with the report first.
func (r *run) recoverPhase(only []string, done func(recovery.Report)) int {
	from := r.meter.Count()
	r.inRec = true
	rep, err := recovery.Run(context.Background(), r.st, r.recoverySession(only))
	r.spent("recover", from)
	if err != nil {
		return r.fail(err)
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

// fail is failed, but once the cap stopped the run it says nothing and returns 1, as the guard's Finish says why.
func (r *run) fail(err error) int {
	if r.guard.Stopped() {
		return 1
	}
	return failed(r.stderr, err)
}

// failed prints err as init's failure and returns 2, or 1 for a stop before spending on propose.
func failed(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "physa init: %v\n", err)
	if _, ok := err.(*shares.TooLarge); ok { // found before any share ran; after a halving it comes wrapped
		return 1
	}
	return 2
}
