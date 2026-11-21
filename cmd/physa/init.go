package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/mapping"
	"github.com/dphaener/physa/internal/vcs/git"
)

// initCommand builds physa init. Its gather and propose phases are built.
func initCommand() *cli.Command {
	return &cli.Command{
		Name:    "init",
		Summary: "Build the map of an existing repo: gather its sources (--gather), then propose a map from them (--propose).",
		Usage:   gather.Usage,
		Run:     initRun,
	}
}

// initRun runs init's --gather or --propose phase on the repo in the current
// directory: 0 when it finished, 1 when the circuit breaker stopped it, 2
// when it could not.
func initRun(args []string, stdout, stderr io.Writer) int {
	o, ok := gather.ParseFlags(args, time.Now())
	if !ok {
		return cli.BadUsage
	}
	if o.Propose {
		return initPropose(o, stdout, stderr)
	}
	if !o.Gather {
		fmt.Fprintln(stderr, "physa init: name a phase: --gather or --propose; the others follow (design 02 §2)")
		return 2
	}
	repo, err := git.Open(".")
	if err != nil {
		return initFailed(stderr, err)
	}
	dir, err := repo.StoreDir()
	if err != nil {
		return initFailed(stderr, err)
	}
	st, err := openStore(".")
	if err != nil {
		return initFailed(stderr, err)
	}
	defer st.Close()
	g, sum, err := gather.Run(".", repo, newForge("."), o, time.Now())
	if err != nil {
		return initFailed(stderr, err)
	}
	if err := st.PutGather(g); err != nil {
		return initFailed(stderr, err)
	}
	sum.Store = dir
	write := gather.WriteText
	if o.JSON {
		write = gather.WriteJSON
	}
	if err := write(stdout, sum); err != nil {
		return initFailed(stderr, err)
	}
	return 0
}

// initPropose proposes a map from the stored gather and prints it.
func initPropose(o gather.Flags, stdout, stderr io.Writer) int {
	st, err := openStore(".")
	if err != nil {
		return initFailed(stderr, err)
	}
	defer st.Close()
	res, err := mapping.Propose(context.Background(), st, mapping.Session{
		Agent:    func() (adapter.Adapter, error) { return agentFor(o.Agent) },
		Appetite: o.Appetite,
		Progress: stderr,
	})
	if err != nil {
		return initFailed(stderr, err)
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
		return initFailed(stderr, err)
	}
	return 0
}

// initFailed prints err as init's failure and returns 2.
func initFailed(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "physa init: %v\n", err)
	return 2
}
