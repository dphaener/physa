package ideacli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dphaener/physa/internal/agentenv"
	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/configdir"
	"github.com/dphaener/physa/internal/enginestart"
	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/person"
	"github.com/dphaener/physa/internal/picker"
)

// callTimeout bounds every API call a command makes, as in the engine
// commands. Starting the engine is bounded by enginestart itself.
const callTimeout = 5 * time.Second

// Env is what the idea and pursuit commands share: how they reach the engine,
// who the person is, and how they ask a person at a terminal. New fills it
// with the real ones; tests replace fields.
type Env struct {
	// Forge is the forge that hosts the repo in dir, used to name the person.
	Forge func(dir string) forge.Forge
	// Person names the person running Physa in dir; "" when unknown, having
	// said why on stderr.
	Person func(dir string, f forge.Forge, stderr io.Writer) string
	// Start says how an engine is started when none answers.
	Start enginestart.Options
	// Getenv reads the environment: agent sessions, $VISUAL and $EDITOR.
	Getenv func(string) string
	// IsInteractive says whether a person is at the terminal; nil asks
	// output.Interactive about stdin and stdout.
	IsInteractive func() bool
	// Pick and Confirm ask the person at the terminal.
	Pick    func(title string, items []picker.Item) (string, error)
	Confirm func(question string) (bool, error)
	// Stdin is what --from - and --signal-from - read.
	Stdin io.Reader
	// Stdio are the editor's standard input, output and error.
	Stdio [3]*os.File
}

// New returns the Env of a real run: the forge newForge builds, the person's
// usual name, the real environment, terminal and pickers.
func New(newForge func(dir string) forge.Forge) *Env {
	return &Env{
		Forge: newForge,
		Person: func(dir string, f forge.Forge, stderr io.Writer) string {
			return person.ForRepo(context.TODO(), dir, f, stderr) // Who has no ctx yet
		},
		Getenv:  os.Getenv,
		Pick:    func(t string, items []picker.Item) (string, error) { return picker.Pick(os.Stdin, os.Stdout, t, items) },
		Confirm: func(q string) (bool, error) { return picker.Confirm(os.Stdin, os.Stdout, q) },
		Stdin:   os.Stdin,
		Stdio:   [3]*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
}

// Interactive is the one decision every idea and pursuit command makes between
// a person at a terminal (pickers, styles, confirms) and a pipe, CI or an
// agent (plain text or --json, errors instead of questions).
func (e *Env) Interactive() bool {
	if e.IsInteractive != nil {
		return e.IsInteractive()
	}
	_, agent := agentenv.Detect(e.Getenv)
	return output.Interactive(os.Stdin, os.Stdout, agent)
}

// Agent names the agent whose session Physa runs inside, if any.
func (e *Env) Agent() (string, bool) { return agentenv.Detect(e.Getenv) }

// Repo returns the absolute root of the repo holding the working directory:
// the nearest directory upwards with a .git entry.
func (e *Env) Repo() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if wd, err = filepath.Abs(wd); err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			return "", fmt.Errorf("%s is not inside a repo", wd)
		}
	}
}

// Who names the person running Physa in the repo at dir; "" when unknown,
// having said why on stderr.
func (e *Env) Who(dir string, stderr io.Writer) string {
	var f forge.Forge
	if e.Forge != nil {
		f = e.Forge(dir)
	}
	return e.Person(dir, f, stderr)
}

// Call connects to the engine in the settings directory, starting it when it
// is not running, and calls method within callTimeout.
func (e *Env) Call(method string, params, result any) error {
	dir, err := configdir.Dir()
	if err != nil {
		return err
	}
	o := e.Start
	o.Dir = dir
	c, err := enginestart.Connect(context.Background(), o)
	if err != nil {
		return err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return c.Call(ctx, method, params, result)
}

// BadRequest reports whether err is the engine refusing what was asked, as
// opposed to a fault.
func BadRequest(err error) bool {
	var ae *api.Error
	return errors.As(err, &ae) && ae.Code == api.CodeBadRequest
}

// Failed says what went wrong and returns the exit code: for a UsageError
// its message, then command.BadUsage so the command's help follows; else
// "physa <name>: <err>" and 2. An engine's refusal is shown as its message
// alone.
func Failed(stderr io.Writer, name string, err error) int {
	var ue *UsageError
	if errors.As(err, &ue) {
		fmt.Fprintf(stderr, "physa %s: %s\n", name, ue.Msg)
		return command.BadUsage
	}
	var ae *api.Error
	if errors.As(err, &ae) && ae.Code == api.CodeBadRequest {
		err = errors.New(ae.Message)
	}
	fmt.Fprintf(stderr, "physa %s: %v\n", name, err)
	return 2
}

// Written returns 0 for a nil err, else what Failed returns.
func Written(stderr io.Writer, name string, err error) int {
	if err != nil {
		return Failed(stderr, name, err)
	}
	return 0
}
