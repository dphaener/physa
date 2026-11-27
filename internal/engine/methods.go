package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/api"
	"github.com/dphaener/physa/internal/repos"
)

// Status is engine.status's output.
type Status struct {
	Version     string   `json:"version" desc:"The Physa version the engine runs."`
	PID         int      `json:"pid" desc:"The engine's OS process id."`
	StartedAt   string   `json:"started_at" desc:"When the engine started, RFC 3339 in UTC."`
	APIRevision int      `json:"api_revision" desc:"The API revision the engine serves."`
	Repos       []string `json:"repos" desc:"The repos the engine has open."`
}

// EventsInput is engine.events's input.
type EventsInput struct {
	Repo  string `json:"repo" desc:"The repo, as an absolute path to it or to a directory inside it."`
	Limit int    `json:"limit,omitempty" desc:"How many of the newest events to return: 1 to 500, 50 when left out."`
}

// EventOut is one event of engine.events's output.
type EventOut struct {
	At      string          `json:"at" desc:"When the event was appended, RFC 3339 in UTC."`
	Kind    string          `json:"kind" desc:"What happened, such as intent.done."`
	Subject string          `json:"subject" desc:"What it happened to, such as intent."`
	Person  string          `json:"person,omitempty" desc:"Who made the change; left out when no person did."`
	Data    json.RawMessage `json:"data" desc:"The event's details, a JSON object."`
}

// engine is one running engine: what it reports and what keeps it up.
type engine struct {
	o        Options // Idle is set
	set      *repos.Set
	started  time.Time
	mu       sync.Mutex
	clients  int
	last     time.Time // the last connect, call or disconnect
	stopped  chan struct{}
	stopOnce sync.Once
}

func newEngine(o Options, now time.Time) *engine {
	return &engine{o: o, set: repos.NewSet(o.Dir, o.Env, o.State), started: now, last: now, stopped: make(chan struct{})}
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

// pending reports whether anything keeps the engine up; it is the single
// place that says so. True while a client is connected (a hung one too), the
// registered repos are opening, an intent is pending, or a timer is due
// within the idle duration. The Set is read first, never under e.mu.
func (e *engine) pending() bool {
	opening := e.set.Opening()
	intents, next, ok := e.set.Pending()
	soon := ok && next.Before(time.Now().Add(e.o.Idle))
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clients > 0 || opening || intents > 0 || soon
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
	return Status{Version: e.o.Version, PID: os.Getpid(), StartedAt: e.started.UTC().Format(time.RFC3339),
		APIRevision: api.Revision, Repos: e.set.Paths()}
}

// table is the engine's methods, with api.list and api.describe.
func (e *engine) table() *api.Table {
	status := api.Define("engine.status", "Reports the engine's version, pid, start time, API revision and open repos.",
		[]string{
			"pid is the engine's OS process id.",
			"started_at is RFC 3339 in UTC.",
			"repos is the root of each repo whose store the engine has open, sorted; [] when none is.",
		},
		`{}`, `{"version":"0.1.0","pid":4242,"started_at":"2026-11-22T10:00:00Z","api_revision":1,"repos":["/home/me/code/shop"]}`,
		func(context.Context, *api.Conn, struct{}) (Status, error) { return e.status(), nil })
	stop := api.Define("engine.stop", "Stops the engine: it replies, then closes every connection and exits.",
		[]string{"The reply is written before shutdown begins; the connection is then closed going-away."},
		`{}`, `{}`,
		func(_ context.Context, c *api.Conn, _ struct{}) (struct{}, error) {
			c.AfterReply(e.stop)
			return struct{}{}, nil
		})
	events := api.Define("engine.events", "Lists a repo's newest events, newest first, from its store.",
		[]string{
			"repo must be an absolute path in a git repo; anything else replies bad_request naming it.",
			"The call registers the repo, so the engine opens it again at its next start.",
			"limit is held to 1..500 and is 50 when left out; the result is [] when there are none.",
		},
		`{"repo":"/home/me/code/shop","limit":1}`,
		`[{"at":"2026-11-22T10:00:00Z","kind":"intent.done","subject":"intent","data":{"kind":"log.append","attempts":1}}]`,
		e.events)
	methods := []api.Method{status, stop, events}
	if e.o.Methods != nil {
		methods = append(methods, e.o.Methods(e.set.Get)...)
	}
	return api.NewTable(methods...)
}

func (e *engine) events(ctx context.Context, _ *api.Conn, in EventsInput) ([]EventOut, error) {
	st, err := e.set.Get(ctx, in.Repo)
	if errors.As(err, new(*repos.NotServedError)) {
		return nil, &api.Error{Code: api.CodeBadRequest, Message: err.Error()}
	} else if err != nil {
		return nil, err
	}
	evs, err := st.Events(ctx, in.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]EventOut, 0, len(evs))
	for _, ev := range evs {
		out = append(out, EventOut{At: ev.At.UTC().Format(time.RFC3339), Kind: ev.Kind, Subject: ev.Subject, Person: ev.Person, Data: ev.Data})
	}
	return out, nil
}
