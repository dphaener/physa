package pursuitcli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/state/plan"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
	"github.com/dphaener/physa/internal/workapi"
)

// sessionVar is the environment variable that names the agent's session.
const sessionVar = "CLAUDE_CODE_SESSION_ID"

func workCommand(p pursuits) *cli.Command {
	return &cli.Command{
		Name:  "work",
		Usage: []string{"physa pursuit work <pursuit>/<change> [--json]", "physa pursuit work --current"},
		Summary: "Record that this agent session works on a planned change, so its commits carry a Change: trailer. " +
			"--current prints <pursuit>/<change> for the session, or nothing; it reads the store and never fails.",
		Run: p.work,
	}
}

func (p pursuits) work(args []string, stdout, stderr io.Writer) int {
	f, ok := ideacli.Parse(args, "current", "json")
	if !ok {
		return cli.BadUsage
	}
	if f.Current {
		if len(f.Args) > 0 || f.JSON {
			return cli.BadUsage
		}
		return p.current(stdout, stderr)
	}
	if len(f.Args) != 1 {
		return cli.BadUsage
	}
	slug, change, ok := strings.Cut(f.Args[0], "/")
	if !ok || slug == "" || change == "" || strings.Contains(change, "/") {
		return cli.BadUsage
	}
	session := p.Getenv(sessionVar)
	if session == "" {
		fmt.Fprintln(stderr, "physa pursuit work: no agent session: run it inside the session that works on the change")
		return 2
	}
	repo, err := p.Repo()
	if err != nil {
		return ideacli.Failed(stderr, "pursuit work", err)
	}
	var out workapi.WorkOut
	if err := p.Call("pursuit.work", workapi.WorkIn{Repo: repo, Pursuit: slug, Change: change, Session: session}, &out); err != nil {
		return ideacli.Failed(stderr, "pursuit work", err)
	}
	if f.JSON {
		return ideacli.Written(stderr, "pursuit work", output.JSON(stdout, out))
	}
	p.View(stdout).Title(fmt.Sprintf("Session %s works on %s/%s", session, out.Pursuit, out.Change))
	return 0
}

// current prints the pursuit and change the current session works on. It always
// returns 0: the commit hook runs it and must never fail a commit. It asks
// no engine and writes nothing; a fault goes to stderr.
func (p pursuits) current(stdout, stderr io.Writer) int {
	session := p.Getenv(sessionVar)
	if session == "" {
		return 0
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "physa pursuit work: %v\n", err)
		return 0
	}
	r, err := git.Open(wd)
	if err != nil {
		if !errors.Is(err, vcs.ErrNotRepo) {
			fmt.Fprintf(stderr, "physa pursuit work: %v\n", err)
		}
		return 0
	}
	dir, err := r.StoreDir()
	if err != nil {
		fmt.Fprintf(stderr, "physa pursuit work: %v\n", err)
		return 0
	}
	db, err := store.OpenReadOnly(filepath.Join(dir, "physa.db"))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(stderr, "physa pursuit work: %v\n", err)
		}
		return 0
	}
	defer db.Close()
	pursuit, change, err := plan.Current(db, session)
	if err != nil {
		fmt.Fprintf(stderr, "physa pursuit work: %v\n", err)
		return 0
	}
	if pursuit != "" && change != "" {
		fmt.Fprintf(stdout, "%s/%s\n", pursuit, change)
	}
	return 0
}
