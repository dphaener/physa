// Package enginecli holds the engine and api command groups: starting, checking and stopping the machine's engine, and calling its API by name for debugging.
package enginecli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/dphaener/physa/internal/cli"
	"github.com/dphaener/physa/internal/configdir"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/enginestart"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/workapi"
)

// callTimeout bounds every API call a command makes. Starting and
// stopping the engine are bounded by enginestart itself.
const callTimeout = 5 * time.Second

// alreadyRunning is the exit code of `engine run` when another engine
// holds the lock, so `engine start` can tell it from a crash. Every other
// failure exits 2.
const alreadyRunning = 3

// Commands returns the engine and api groups. version is what a foreground
// engine reports in engine.status.
func Commands(version string) []*cli.Command {
	return commands(version, enginestart.Options{})
}

// commands builds the groups, starting engines with start's Command, so
// tests can make sure nothing is spawned.
func commands(version string, start enginestart.Options) []*cli.Command {
	g := group{version, start}
	return []*cli.Command{g.engineCommand(), g.apiCommand()}
}

// group is what the commands share: the version a foreground engine
// reports, and how an engine is started.
type group struct {
	version string
	start   enginestart.Options
}

func (g group) engineCommand() *cli.Command {
	return &cli.Command{
		Name: "engine",
		Summary: "Start, check and stop the machine's one engine: a bare engine prints its status. " +
			"status and the api commands start the engine when it is not running; stop never does.",
		Usage: []string{
			"physa engine [--json]",
			"physa engine run [--dir <dir>] [--idle <duration>]",
			"physa engine start [--idle <duration>] [--json]",
			"physa engine status [--json]",
			"physa engine stop [--json]",
		},
		Run: g.engineStatus,
		Subs: []*cli.Command{
			leaf("run", "physa engine run [--dir <dir>] [--idle <duration>]",
				"Run the engine in the foreground until it is stopped, interrupted, or idle for --idle (default 30m). "+
					"--dir is the absolute directory it serves, by default the Physa settings directory; engine start passes it to the engine it starts.", g.engineRun),
			leaf("start", "physa engine start [--idle <duration>] [--json]",
				"Start the engine in the background unless it is running, and print its status. --idle applies only to an engine this starts.", g.engineStart),
			leaf("status", "physa engine status [--json]",
				"Print the engine's version, pid, start time, API revision and open repos, starting the engine when it is not running.", g.engineStatus),
			leaf("stop", "physa engine stop [--json]",
				"Stop the engine and wait until it is gone. No engine running is not an error.", engineStop),
		},
	}
}

// engineRun serves in the foreground until SIGINT or SIGTERM, engine.stop
// or the idle time.
func (g group) engineRun(args []string, _, stderr io.Writer) int {
	f, ok := parse(args, "dir", "idle")
	if !ok || len(f.args) > 0 {
		return cli.BadUsage
	}
	dir, err := f.dir, error(nil)
	if dir == "" {
		dir, err = configdir.Dir()
	} else if !filepath.IsAbs(dir) {
		err = fmt.Errorf("--dir %q is not an absolute path", dir)
	}
	if err != nil {
		return failed(stderr, "engine run", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = engine.Run(ctx, engine.Options{Dir: dir, Idle: f.idle, Version: g.version, Methods: workapi.Methods, State: workapi.StateOptions()})
	var running *engine.RunningError
	if errors.As(err, &running) {
		failed(stderr, "engine run", err)
		return alreadyRunning
	}
	return written(stderr, "engine run", err)
}

func (g group) engineStart(args []string, stdout, stderr io.Writer) int {
	f, ok := parse(args, "idle", "json")
	if !ok || len(f.args) > 0 {
		return cli.BadUsage
	}
	dir, err := configdir.Dir()
	if err != nil {
		return failed(stderr, "engine start", err)
	}
	o := g.start
	o.Dir, o.Idle = dir, f.idle
	started, err := enginestart.Start(context.Background(), o)
	var s engine.Status
	if err == nil {
		err = g.call(f.idle, "engine.status", nil, &s)
	}
	if err != nil {
		return failed(stderr, "engine start", err)
	}
	if f.json {
		return written(stderr, "engine start", output.JSON(stdout, struct {
			Started bool          `json:"started"`
			Status  engine.Status `json:"status"`
		}{started, s}))
	}
	p := output.For(stdout)
	if started {
		p.Heading("Engine started")
	} else {
		p.Heading("Engine already running")
	}
	printStatus(p, s)
	if !started && f.idleSet {
		p.Line("The running engine keeps its own idle time; --idle applies only to an engine that starts.")
	}
	return 0
}

func (g group) engineStatus(args []string, stdout, stderr io.Writer) int {
	f, ok := parse(args, "json")
	if !ok || len(f.args) > 0 {
		return cli.BadUsage
	}
	var s engine.Status
	if err := g.call(0, "engine.status", nil, &s); err != nil {
		return failed(stderr, "engine status", err)
	}
	if f.json {
		return written(stderr, "engine status", output.JSON(stdout, s))
	}
	printStatus(output.For(stdout), s)
	return 0
}

func printStatus(p *output.Printer, s engine.Status) {
	repos := "none"
	if len(s.Repos) > 0 {
		repos = strings.Join(s.Repos, ", ")
	}
	p.Fields(
		[2]string{"version", s.Version},
		[2]string{"pid", fmt.Sprint(s.PID)},
		[2]string{"started", s.StartedAt},
		[2]string{"API revision", fmt.Sprint(s.APIRevision)},
		[2]string{"repos", repos},
	)
}

// engineStop stops the engine; it never starts one.
func engineStop(args []string, stdout, stderr io.Writer) int {
	f, ok := parse(args, "json")
	if !ok || len(f.args) > 0 {
		return cli.BadUsage
	}
	dir, err := configdir.Dir()
	if err != nil {
		return failed(stderr, "engine stop", err)
	}
	stopped, err := enginestart.Stop(context.Background(), dir)
	if err != nil {
		return failed(stderr, "engine stop", err)
	}
	if f.json {
		return written(stderr, "engine stop", output.JSON(stdout, struct {
			Stopped bool `json:"stopped"`
		}{stopped}))
	}
	if stopped {
		output.For(stdout).Heading("Engine stopped")
	} else {
		output.For(stdout).Heading("No engine running")
	}
	return 0
}

// call connects to the engine in the settings directory, starting it with
// idle when it is not running, and calls method within callTimeout.
func (g group) call(idle time.Duration, method string, params, result any) error {
	dir, err := configdir.Dir()
	if err != nil {
		return err
	}
	o := g.start
	o.Dir, o.Idle = dir, idle
	c, err := enginestart.Connect(context.Background(), o)
	if err != nil {
		return err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return c.Call(ctx, method, params, result)
}

// leaf is a subcommand with one usage line.
func leaf(name, usage, summary string, run func(args []string, stdout, stderr io.Writer) int) *cli.Command {
	return &cli.Command{Name: name, Summary: summary, Usage: []string{usage}, Run: run}
}

// failed prints "physa <name>: <err>" and returns 2.
func failed(stderr io.Writer, name string, err error) int {
	fmt.Fprintf(stderr, "physa %s: %v\n", name, err)
	return 2
}

// written returns 0 for a nil err, else what failed returns.
func written(stderr io.Writer, name string, err error) int {
	if err != nil {
		return failed(stderr, name, err)
	}
	return 0
}

// flags are a command line's flags and the arguments left.
type flags struct {
	json, idleSet bool
	dir           string
	idle          time.Duration
	args          []string
}

// parse reads args, taking only the flags in allowed: --json, and --dir
// and --idle each with a value (--name value or --name=value). A lone "-"
// is an argument. ok is false for any other flag, a flag given twice, or
// a missing or bad value; --idle must be a positive duration.
func parse(args []string, allowed ...string) (f flags, ok bool) {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-" || !strings.HasPrefix(a, "-") {
			f.args = append(f.args, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !strings.HasPrefix(a, "--") || seen[name] || !slices.Contains(allowed, name) || (name == "json" && hasVal) {
			return f, false
		}
		seen[name] = true
		if name == "json" {
			f.json = true
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return f, false
			}
			i++
			val = args[i]
		}
		if name == "dir" {
			f.dir = val
		} else if d, err := time.ParseDuration(val); err == nil && d > 0 {
			f.idle, f.idleSet = d, true
		} else {
			return f, false
		}
		if val == "" {
			return f, false
		}
	}
	return f, true
}
