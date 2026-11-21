package initrun

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/recovery"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
)

// Env is what init reaches outside itself through, each a seam the caller
// owns: the store of the repo at a directory, the repo itself, the forge that
// hosts it, the agent a name selects (empty for the default) and the clock.
type Env struct {
	Store func(dir string) (*store.Store, error)
	Repo  func(dir string) (vcs.Repo, error)
	Forge func(dir string) forge.Forge
	// PRExists resolves #N anchors for the repo at a directory; nil checks
	// them for form only.
	PRExists func(dir string) func(int) (bool, error)
	Agent    func(name string) (adapter.Adapter, error)
	Now      func() time.Time
}

// Run runs init's --gather, --propose or --recover phase on the repo in the current
// directory: 0 when it finished, 1 when the circuit breaker stopped it (or
// some capabilities are not yet recovered), 2 when it could not (or cli.BadUsage when the arguments were not understood).
func Run(args []string, stdout, stderr io.Writer, env Env) int {
	o, ok := ParseFlags(args, env.Now())
	if !ok {
		return cli.BadUsage
	}
	if o.Propose {
		return propose(o, stdout, stderr, env)
	}
	if o.Recover {
		return recoverPhase(o, stdout, stderr, env)
	}
	if !o.Gather {
		fmt.Fprintln(stderr, "physa init: name a phase: --gather, --propose or --recover; the PR on physa/init follows (design 02 §2)")
		return 2
	}
	repo, err := env.Repo(".")
	if err != nil {
		return failed(stderr, err)
	}
	dir, err := repo.StoreDir()
	if err != nil {
		return failed(stderr, err)
	}
	st, err := env.Store(".")
	if err != nil {
		return failed(stderr, err)
	}
	defer st.Close()
	bounds := gather.Bounds{Since: o.Since, SinceSet: o.SinceSet, MaxPRs: o.MaxPRs}
	g, sum, err := gather.Run(".", repo, env.Forge("."), bounds, env.Now())
	if err != nil {
		return failed(stderr, err)
	}
	if err := st.PutGather(g); err != nil {
		return failed(stderr, err)
	}
	sum.Store = dir
	write := gather.WriteText
	if o.JSON {
		write = gather.WriteJSON
	}
	if err := write(stdout, sum); err != nil {
		return failed(stderr, err)
	}
	return 0
}

// propose proposes a map from the stored gather and prints it.
func propose(o Flags, stdout, stderr io.Writer, env Env) int {
	st, err := env.Store(".")
	if err != nil {
		return failed(stderr, err)
	}
	defer st.Close()
	res, err := mapping.Propose(context.Background(), st, mapping.Session{
		Agent:    func() (adapter.Adapter, error) { return env.Agent(o.Agent) },
		Appetite: o.Appetite,
		Progress: stderr,
	})
	if err != nil {
		return failed(stderr, err)
	}
	if res.Stopped {
		fmt.Fprintf(stderr, "physa init: the circuit breaker stopped the session at %d tokens; nothing was proposed\n", res.Appetite)
		return 1
	}
	write := mapping.WriteText
	if o.JSON {
		write = mapping.WriteJSON
	}
	if err := write(stdout, res); err != nil {
		return failed(stderr, err)
	}
	return 0
}

// recoverPhase recovers the proposed capabilities into the store and prints
// how each fared. It writes nothing to the repo.
func recoverPhase(o Flags, stdout, stderr io.Writer, env Env) int {
	root, err := filepath.Abs(".")
	if err != nil {
		return failed(stderr, err)
	}
	st, err := env.Store(".")
	if err != nil {
		return failed(stderr, err)
	}
	defer st.Close()
	s := recovery.Session{
		Agent:    func() (adapter.Adapter, error) { return env.Agent(o.Agent) },
		Appetite: o.Appetite, Only: o.Only, Root: root,
		OpenRepo: env.Repo, Progress: stderr, Now: env.Now,
	}
	if env.PRExists != nil {
		s.PRExists = env.PRExists(".")
	}
	rep, err := recovery.Run(context.Background(), st, s)
	if err != nil {
		return failed(stderr, err)
	}
	write := writeRecoveryText
	if o.JSON {
		write = writeRecoveryJSON
	}
	if err := write(stdout, rep); err != nil {
		return failed(stderr, err)
	}
	return exitCode(rep)
}

// failed prints err as init's failure and returns 2.
func failed(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "physa init: %v\n", err)
	return 2
}
