package engine

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/api"
)

// Status is engine.status's output.
type Status struct {
	Version     string   `json:"version" desc:"The Physa version the engine runs."`
	PID         int      `json:"pid" desc:"The engine's OS process id."`
	StartedAt   string   `json:"started_at" desc:"When the engine started, RFC 3339 in UTC."`
	APIRevision int      `json:"api_revision" desc:"The API revision the engine serves."`
	Repos       []string `json:"repos" desc:"The repos the engine has open."`
}

// engine is one running engine's state: what it reports and what keeps
// it up.
type engine struct {
	version  string
	started  time.Time
	mu       sync.Mutex
	clients  int
	last     time.Time // the last connect, call or disconnect
	stopped  chan struct{}
	stopOnce sync.Once
}

func newEngine(version string, now time.Time) *engine {
	return &engine{version: version, started: now, last: now, stopped: make(chan struct{})}
}

// hooks count connected clients and note each activity.
func (e *engine) hooks() api.Hooks {
	return api.Hooks{
		Connected:    func() { e.activity(1) },
		Disconnected: func() { e.activity(-1) },
		Called:       func(string) { e.activity(0) },
	}
}

func (e *engine) activity(clients int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clients += clients
	e.last = time.Now()
}

// pending reports whether anything keeps the engine up. It is the single
// place that says so: later builds add their runs, asks, intents and
// timers here. Today it is true while any client is connected, so a client
// that stays connected but hung keeps the engine up too.
func (e *engine) pending() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clients > 0
}

// idleFor reports whether nothing is pending and nothing happened for d.
func (e *engine) idleFor(d time.Duration) bool {
	if e.pending() {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return time.Since(e.last) >= d
}

// stop asks Run to shut down; asking again does nothing.
func (e *engine) stop() { e.stopOnce.Do(func() { close(e.stopped) }) }

func (e *engine) status() Status {
	return Status{
		Version:     e.version,
		PID:         os.Getpid(),
		StartedAt:   e.started.UTC().Format(time.RFC3339),
		APIRevision: api.Revision,
		Repos:       []string{},
	}
}

// table is the engine's methods, with api.list and api.describe.
func (e *engine) table() *api.Table {
	status := api.Define("engine.status", "Reports the engine's version, pid, start time, API revision and open repos.",
		[]string{
			"pid is the engine's OS process id.",
			"started_at is RFC 3339 in UTC.",
			"repos is empty in this build: the engine opens no repos yet.",
		},
		`{}`, `{"version":"0.1.0","pid":4242,"started_at":"2026-11-22T10:00:00Z","api_revision":1,"repos":[]}`,
		func(context.Context, *api.Conn, struct{}) (Status, error) { return e.status(), nil })
	stop := api.Define("engine.stop", "Stops the engine: it replies, then closes every connection and exits.",
		[]string{"The reply is written before shutdown begins; the connection is then closed going-away."},
		`{}`, `{}`,
		func(_ context.Context, c *api.Conn, _ struct{}) (struct{}, error) {
			c.AfterReply(e.stop)
			return struct{}{}, nil
		})
	return api.NewTable(status, stop)
}
