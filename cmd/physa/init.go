package main

import (
	"fmt"
	"io"
	"time"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/gather"
	"github.com/dphaener/physa/internal/vcs/git"
)

// initCommand builds physa init. Only its gather phase is built yet.
func initCommand() *cli.Command {
	return &cli.Command{
		Name:    "init",
		Summary: "Build the map of an existing repo: only its gather phase (--gather) is built yet.",
		Usage:   gather.Usage,
		Run:     initRun,
	}
}

// initRun gathers the sources of the repo in the current directory into its
// store: 0 when it gathered, 2 when it could not.
func initRun(args []string, stdout, stderr io.Writer) int {
	o, ok := gather.ParseFlags(args, time.Now())
	if !ok {
		return cli.BadUsage
	}
	if !o.Gather {
		fmt.Fprintln(stderr, "physa init: only --gather is built yet; the other phases follow (design 02 §2)")
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

// initFailed prints err as init's failure and returns 2.
func initFailed(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "physa init: %v\n", err)
	return 2
}
