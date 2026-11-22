// Package enginestart gives a command a client connected to the machine's engine, starting the engine first when it is not running, and stops it on request. Map and check commands never use it.
package enginestart

import (
	"context"
	"os/exec"
	"time"

	"github.com/dphaener/physa/internal/apiclient"
	"github.com/dphaener/physa/internal/engine"
)

// The bounds on every dial and call enginestart makes, so a hung or
// half-stopped engine can never hang a command.
const (
	pollTimeout  = time.Second            // each attempt while polling
	callTimeout  = 5 * time.Second        // every other dial and call
	startWait    = 5 * time.Second        // how long a started engine has to answer
	stopWait     = 5 * time.Second        // how long a stopped engine has to go
	pollEvery    = 20 * time.Millisecond  // between polls
	respawnEvery = 100 * time.Millisecond // between children that lost the lock
)

// alreadyRunning is the exit code `physa engine run` uses for "an engine
// is already running" alone; any other failure exits with another code.
const alreadyRunning = 3

// Options say which engine to connect to and how to start it.
type Options struct {
	Dir  string        // the engine's directory, absolute
	Idle time.Duration // the started engine's idle time; ≤ 0 means engine.DefaultIdle
	// Command builds the engine's process for dir and idle. nil runs this
	// executable as `engine run --dir <dir> --idle <idle>`.
	Command func(dir string, idle time.Duration) *exec.Cmd
}

// Connect returns a client connected to the engine on o.Dir, which has
// answered engine.status. When no engine answers it starts one, and when
// the engine it reached was just exiting idle it starts one again, once.
func Connect(ctx context.Context, o Options) (*apiclient.Client, error) {
	p, err := engine.PathsIn(o.Dir)
	if err != nil {
		return nil, err
	}
	c, _, err := probe(ctx, p.Socket, callTimeout)
	for try := 0; err != nil && try < 2; try++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if _, serr := Start(ctx, o); serr != nil {
			return nil, serr
		}
		c, _, err = probe(ctx, p.Socket, callTimeout)
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// probe dials the socket and calls engine.status, each within timeout.
// On success the client stays open for the caller.
func probe(ctx context.Context, socket string, timeout time.Duration) (*apiclient.Client, engine.Status, error) {
	var s engine.Status
	dctx, cancel := context.WithTimeout(ctx, timeout)
	c, err := apiclient.Dial(dctx, socket)
	cancel()
	if err != nil {
		return nil, s, err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	err = c.Call(cctx, "engine.status", nil, &s)
	cancel()
	if err != nil {
		c.Close()
		return nil, s, err
	}
	return c, s, nil
}

// answering reports whether an engine answers engine.status on socket,
// and its status.
func answering(ctx context.Context, socket string, timeout time.Duration) (engine.Status, bool) {
	c, s, err := probe(ctx, socket, timeout)
	if err != nil {
		return s, false
	}
	c.Close()
	return s, true
}
