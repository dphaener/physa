package skillcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/vcs/git"
)

const (
	sessionUsage = "physa skill session"

	// sessionVar is the environment variable that names the agent's session.
	sessionVar = "CLAUDE_CODE_SESSION_ID"

	// stdinLimit bounds what the session command reads of the hook's JSON on stdin.
	stdinLimit = 64 << 10

	// defaultDeadline bounds a whole run of the session command, well inside the hook's own timeout.
	defaultDeadline = 2 * time.Second
)

// CurrentFunc returns the pursuit and change a session works on in the repo
// that holds wd, read from the store and never from the engine. It returns
// empty names and no error when there is none, and an error when it cannot
// tell.
type CurrentFunc func(wd, session string) (pursuit, change string, err error)

// sessionCommand is the command the session hook runs. It is hidden from help
// and documented in architecture 13 §4.
func (e env) sessionCommand() *cobra.Command {
	return command.New("session",
		"Print what a session needs to know about this repo's Physa map; the session hook runs it. "+
			"It prints nothing outside a repo with a map or on any fault, and always exits 0.",
		[]string{sessionUsage}, func(args []string, stdout, _ io.Writer) int {
			if len(args) > 0 {
				return command.BadUsage
			}
			e.sessionLines(stdout)
			return 0
		})
}

// sessionLines writes the session's few lines to stdout when every step succeeded
// within the deadline, and nothing otherwise. It never writes to stderr.
func (e env) sessionLines(stdout io.Writer) {
	deadline := e.deadline
	if deadline <= 0 {
		deadline = defaultDeadline
	}
	done := make(chan string, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- ""
			}
		}()
		done <- e.sessionText()
	}()
	select {
	case text := <-done:
		_, _ = io.WriteString(stdout, text)
	case <-time.After(deadline):
	}
}

// sessionText is the whole output, or "" when any step fails.
func (e env) sessionText() string {
	if e.getwd == nil || e.getenv == nil {
		return ""
	}
	wd, err := e.getwd()
	if err != nil {
		return ""
	}
	if info, err := os.Stat(filepath.Join(wd, ".physa")); err != nil || !info.IsDir() {
		return ""
	}
	if _, err := git.Open(wd); err != nil {
		return ""
	}
	m, _, err := mapfile.Read(wd)
	if err != nil {
		return ""
	}
	var out bytes.Buffer
	n := len(m.Capabilities)
	noun := "capabilities"
	if n == 1 {
		noun = "capability"
	}
	fmt.Fprintf(&out, "This repo has a Physa map with %d %s.\n", n, noun)
	if session := e.session(); session != "" && e.current != nil {
		pursuit, change, err := e.current(wd, session)
		if err != nil {
			return ""
		}
		if pursuit != "" && change != "" {
			fmt.Fprintf(&out, "This session works on %s/%s.\n", pursuit, change)
		}
	}
	out.WriteString("Before changing code, run `physa map index`, then `physa map slice <capabilities>` for the ones your task touches.\n")
	return out.String()
}

// session is the agent's session: the environment's, else the hook's JSON on
// stdin, read only when stdin is not a terminal. Bad JSON gives none.
func (e env) session() string {
	if s := e.getenv(sessionVar); s != "" {
		return s
	}
	if e.stdin == nil || (e.stdinIsTerminal != nil && e.stdinIsTerminal()) {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(e.stdin, stdinLimit))
	if err != nil {
		return ""
	}
	var in struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(b, &in) != nil {
		return ""
	}
	return in.SessionID
}

// isTerminal is whether f is a character device, as a terminal is.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err != nil || info.Mode()&os.ModeCharDevice != 0
}
