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
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/command"
	"github.com/dphaener/physa/internal/configdir"
	"github.com/dphaener/physa/internal/engine"
	"github.com/dphaener/physa/internal/enginestart"
	"github.com/dphaener/physa/internal/forge/github"
	"github.com/dphaener/physa/internal/output"
	"github.com/dphaener/physa/internal/person"
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
func Commands(version string) []*cobra.Command {
	return commands(version, enginestart.Options{}, os.Getenv)
}

// commands builds the groups, starting engines with start's Command, so
// tests can make sure nothing is spawned, and finding an agent's session
// through getenv.
func commands(version string, start enginestart.Options, getenv func(string) string) []*cobra.Command {
	g := group{version, start, getenv}
	return []*cobra.Command{g.engineCommand(), g.apiCommand()}
}

// group is what the commands share: the version a foreground engine
// reports, how an engine is started, and how an agent's session is found.
type group struct {
	version string
	start   enginestart.Options
	getenv  func(string) string
}

func (g group) engineCommand() *cobra.Command {
	return build("engine",
		"Start, check and stop the machine's one engine: a bare engine prints its status. "+
			"status and the api commands start the engine when it is not running; stop never does.",
		[]string{
			"physa engine [--json]",
			"physa engine run [--dir <dir>] [--idle <duration>]",
			"physa engine start [--idle <duration>] [--json]",
			"physa engine status [--json]",
			"physa engine stop [--json]",
		},
		g.engineStatus, []string{"json"},
		leaf("run", "physa engine run [--dir <dir>] [--idle <duration>]",
			"Run the engine in the foreground until it is stopped, interrupted, or idle for --idle (default 30m). "+
				"--dir is the absolute directory it serves, by default the Physa settings directory; engine start passes it to the engine it starts.",
			g.engineRun, "dir", "idle"),
		leaf("start", "physa engine start [--idle <duration>] [--json]",
			"Start the engine in the background unless it is running, and print its status. --idle applies only to an engine this starts.",
			g.engineStart, "idle", "json"),
		leaf("status", "physa engine status [--json]",
			"Print the engine's version, pid, start time, API revision and open repos, starting the engine when it is not running.",
			g.engineStatus, "json"),
		leaf("stop", "physa engine stop [--json]",
			"Stop the engine and wait until it is gone. No engine running is not an error.", engineStop, "json"))
}

// namePerson names who adds an idea when idea.add leaves the person out:
// the person running Physa on this machine, named as the CLI names them.
func namePerson(ctx context.Context, dir string) string {
	return person.ForRepo(ctx, dir, github.New(dir), io.Discard)
}

// engineRun serves in the foreground until SIGINT or SIGTERM, engine.stop
// or the idle time.
func (g group) engineRun(f *flags, args []string, _, stderr io.Writer) int {
	idle, _, ok := f.duration()
	if !ok || len(args) > 0 {
		return command.BadUsage
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
	err = engine.Run(ctx, engine.Options{Dir: dir, Idle: idle, Version: g.version, Methods: workapi.Methods(namePerson), State: workapi.StateOptions()})
	var running *engine.RunningError
	if errors.As(err, &running) {
		failed(stderr, "engine run", err)
		return alreadyRunning
	}
	return written(stderr, "engine run", err)
}

func (g group) engineStart(f *flags, args []string, stdout, stderr io.Writer) int {
	idle, idleSet, ok := f.duration()
	if !ok || len(args) > 0 {
		return command.BadUsage
	}
	dir, err := configdir.Dir()
	if err != nil {
		return failed(stderr, "engine start", err)
	}
	o := g.start
	o.Dir, o.Idle = dir, idle
	started, err := enginestart.Start(context.Background(), o)
	var s engine.Status
	if err == nil {
		err = g.call(idle, "engine.status", nil, &s)
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
	if !started && idleSet {
		p.Line("The running engine keeps its own idle time; --idle applies only to an engine that starts.")
	}
	return 0
}

func (g group) engineStatus(f *flags, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return command.BadUsage
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
func engineStop(f *flags, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return command.BadUsage
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

// run is a command's job: it gets its flags and the positionals.
type run func(f *flags, args []string, stdout, stderr io.Writer) int

// leaf is a subcommand with one usage line and the flags named.
func leaf(name, usage, summary string, job run, names ...string) *cobra.Command {
	return build(name, summary, []string{usage}, job, names)
}

// build is a command declaring the flags named: "json", "dir" and "idle".
func build(name, summary string, usage []string, job run, names []string, subs ...*cobra.Command) *cobra.Command {
	f := &flags{}
	return command.Declare(command.New(name, summary, usage,
		func(_ context.Context, args []string, stdout, stderr io.Writer) int {
			return job(f, args, stdout, stderr)
		}, subs...),
		func(fs *pflag.FlagSet) {
			for _, n := range names {
				switch n {
				case "json":
					command.Switch(fs, "json", &f.json, "")
				case "dir":
					fs.StringVar(&f.dir, "dir", "", "")
				default:
					fs.StringVar(&f.idle, n, "", "")
				}
			}
		})
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

// flags are the values a command's flags bind to. --dir and --idle are
// strings, checked by the job, since an empty value is already bad usage.
type flags struct {
	json      bool
	dir, idle string
}

// duration reads --idle: set is false when it is absent, and ok is false
// unless it is a positive duration.
func (f *flags) duration() (d time.Duration, set, ok bool) {
	if f.idle == "" {
		return 0, false, true
	}
	d, err := time.ParseDuration(f.idle)
	return d, true, err == nil && d > 0
}
