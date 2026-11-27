package pursuitcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/ideacli"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/state/plan"
	"github.com/dphaener/physa/internal/store"
	"github.com/dphaener/physa/internal/vcs"
	"github.com/dphaener/physa/internal/vcs/git"
)

// sessionVar is the environment variable that names the agent's session.
const sessionVar = "CLAUDE_CODE_SESSION_ID"

func workCommand(p pursuits) *cobra.Command {
	var current, asJSON bool
	return command.Declare(command.New("work",
		"Record that this agent session works on a planned change, so its commits carry a Change: trailer. "+
			"--current prints <pursuit>/<change> for the session, or nothing; it reads the store and never fails.",
		[]string{"physa pursuit work <pursuit>/<change> [--json]", "physa pursuit work --current"},
		func(_ context.Context, args []string, stdout, stderr io.Writer) int {
			return p.work(args, current, asJSON, stdout, stderr)
		}),
		func(fs *pflag.FlagSet) {
			command.Switch(fs, "current", &current, "")
			ideacli.JSON(fs, &asJSON)
		})
}

func (p pursuits) work(args []string, current, asJSON bool, stdout, stderr io.Writer) int {
	if current {
		if len(args) > 0 || asJSON {
			return command.BadUsage
		}
		return p.current(stdout, stderr)
	}
	if len(args) != 1 {
		return command.BadUsage
	}
	slug, change, ok := strings.Cut(args[0], "/")
	if !ok || slug == "" || change == "" || strings.Contains(change, "/") {
		return command.BadUsage
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
	var out client.WorkOut
	if err := p.Call("pursuit.work", client.WorkIn{Repo: repo, Pursuit: slug, Change: change, Session: session}, &out); err != nil {
		return ideacli.Failed(stderr, "pursuit work", err)
	}
	if asJSON {
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
	pursuit, change, err := Current(wd, session)
	if err != nil {
		fmt.Fprintf(stderr, "physa pursuit work: %v\n", err)
		return 0
	}
	if pursuit != "" && change != "" {
		fmt.Fprintf(stdout, "%s/%s\n", pursuit, change)
	}
	return 0
}

// Current returns the pursuit and change the session works on in the repo that
// holds wd, read from the store and never from the engine. A directory outside
// a repo, a store that does not exist yet, an empty session and a session with
// no change give empty names and no error. A failing git, a store that cannot
// be read and a failing query give an error.
func Current(wd, session string) (pursuit, change string, err error) {
	if session == "" {
		return "", "", nil
	}
	ctx := context.TODO()
	r, err := git.Open(ctx, wd)
	if err != nil {
		if errors.Is(err, vcs.ErrNotRepo) {
			return "", "", nil
		}
		return "", "", err
	}
	dir, err := r.StoreDir(ctx)
	if err != nil {
		return "", "", err
	}
	db, err := store.OpenReadOnly(ctx, filepath.Join(dir, "physa.db"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", nil
		}
		return "", "", err
	}
	defer db.Close()
	return plan.Current(db, session)
}
