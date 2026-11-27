// Package engine is the one engine per machine: it holds the machine's lock, serves the local API on its socket, and exits when nothing is pending or when asked to stop. It never judges.
package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/state"
)

// DefaultIdle is how long the engine waits with nothing pending before it
// exits, when Options.Idle is not set.
const DefaultIdle = 30 * time.Minute

// maxSocket is the length a Unix socket path must stay under: macOS's
// sun_path holds 104 bytes. The limit applies on every platform, so the
// engine behaves the same on Linux and macOS.
const maxSocket = 104

// Paths are the engine's files in its directory.
type Paths struct {
	Dir    string // the settings directory, mode 0700
	Socket string // the API's Unix socket, mode 0600
	Lock   string // held while the engine runs; holds its pid as text
	Log    string // where a detached engine's output goes
}

// PathsIn returns the engine's files in dir, which must be absolute. A
// socket path of 104 bytes or more is refused, naming the path and the
// limit: no other directory is tried.
func PathsIn(dir string) (Paths, error) {
	if !filepath.IsAbs(dir) {
		return Paths{}, fmt.Errorf("the engine's directory %q is not an absolute path", dir)
	}
	dir = filepath.Clean(dir)
	p := Paths{
		Dir:    dir,
		Socket: filepath.Join(dir, client.SocketName),
		Lock:   filepath.Join(dir, "engine.lock"),
		Log:    filepath.Join(dir, "engine.log"),
	}
	if len(p.Socket) >= maxSocket {
		return Paths{}, fmt.Errorf("the engine's socket path %s is %d bytes; a Unix socket path must be under %d bytes (the limit on macOS)", p.Socket, len(p.Socket), maxSocket)
	}
	return p, nil
}

// Options say how Run serves.
type Options struct {
	Dir     string        // the settings directory, absolute
	Idle    time.Duration // how long to wait with nothing pending; ≤ 0 means DefaultIdle
	Version string        // the version engine.status reports
	State   state.Options // kinds (and in tests a clock) for every repo; StoreDir is set per repo
	Env     []string      // the environment git runs in, to find each repo's store
	// Methods (nil by default) is how tests add methods; get opens a repo's state.
	Methods func(get func(ctx context.Context, repo string) (*state.State, error)) []api.Method
}

// RunningError is Run's error when another engine holds the lock.
type RunningError struct {
	PID int // the running engine's pid; 0 when it has not written it yet
}

func (e *RunningError) Error() string {
	if e.PID <= 0 {
		return "an engine is already running"
	}
	return "an engine is already running (pid " + strconv.Itoa(e.PID) + ")"
}
