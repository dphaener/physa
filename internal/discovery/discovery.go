// Package discovery starts one read-only agent session in the repo that names
// the sources a map should be built from, each with a kind and a reason,
// within a token cap. The session may read and search the repo's files and
// never writes. Checking what it names is gather's job, not this package's.
package discovery

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
)

// DefaultAppetite is how many tokens a discovery session may use when init
// names no other cap. Real sessions on this repo spent about 30,000 tokens a
// turn, cached input included, so 40,000 stopped them before they named
// anything; it is still to be tuned from real runs (measure-before-budget).
const DefaultAppetite = 100_000

// MinAppetite is the least cap a session is worth starting under: real first
// turns cost 15,000 to 17,000 tokens, so a smaller cap would stop the session
// before any reply and spend tokens for nothing.
const MinAppetite = 20_000

// TooSmall says why a session cannot start under cap tokens, or is empty
// when cap reaches MinAppetite.
func TooSmall(cap int64) string {
	if cap >= MinAppetite {
		return ""
	}
	return fmt.Sprintf("the cap of %d tokens is too small to start discovery", cap)
}

// MaxNamed is how many sources a session may name; gather refuses the rest.
const MaxNamed = 200

// Source is one source the session names.
type Source struct {
	Path, Kind, Reason string
}

// Session is how a discovery session is run.
type Session struct {
	// Agent resolves the agent to run.
	Agent func() (adapter.Adapter, error)
	// Appetite is the tokens the session may use before it is stopped.
	Appetite int64
	// Root is the repo's root, where the session runs.
	Root string
	// Progress, when set, receives one line before the session starts.
	Progress io.Writer
}

// Result is what a session ended with.
type Result struct {
	Sources []Source
	Model   string
	Tokens  adapter.Tokens
	// Stopped is set when the appetite ended the session; it then names no
	// sources.
	Stopped bool
	// Malformed counts the entries, or the whole reply, that could not be
	// read as a source; they are dropped.
	Malformed int
}

// Run starts the session and parses what it names. An agent that cannot be
// resolved or a session that fails is an error; a session stopped at its
// appetite, or a reply that names nothing readable, is not.
func Run(ctx context.Context, s Session) (Result, error) {
	agent, err := s.Agent()
	if err != nil {
		return Result{}, err
	}
	if s.Progress != nil {
		fmt.Fprintf(s.Progress, "physa init: discovery: the agent reads the repo for sources, up to %d tokens\n", s.Appetite)
	}
	spec := adapter.Spec{Prompt: Prompt(), Dir: s.Root, ReadOnly: true, SessionID: draft.NewSessionID()}
	res, stopped, err := attempt(ctx, agent, spec, s.Appetite)
	out := Result{Model: res.Model, Tokens: res.Tokens, Stopped: stopped}
	if err != nil || stopped {
		return out, err
	}
	out.Sources, out.Malformed = Parse(res.Text)
	return out, nil
}

// attempt runs the session and waits for it. Its circuit breaker stops the
// session before it overshoots limit where it can: each report of its tokens
// is weighed with the largest increase any report has brought, which is about
// what one more turn costs, and the session is stopped when one more such
// increase would reach limit. stopped is then true, with no text and a nil
// error, as it is when the tokens themselves reach limit. A session that
// still ended with its reply, under limit, keeps it. The result's tokens are
// the last the session reported when it ended without a count; a report that
// overshoots limit in one step is returned as it is.
func attempt(ctx context.Context, a adapter.Adapter, spec adapter.Spec, limit int64) (res adapter.Result, stopped bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var last adapter.Tokens
	var step int64
	tripped := false
	used := func(t adapter.Tokens) int64 { return t.Input + t.Output }
	spec.Usage = func(t adapter.Tokens) {
		mu.Lock()
		defer mu.Unlock()
		step = max(step, used(t)-used(last))
		last = t
		if used(t)+step >= limit {
			tripped = true
			cancel()
		}
	}
	s, err := a.Start(ctx, spec)
	if err != nil {
		return adapter.Result{}, false, err
	}
	res, err = s.Wait()
	mu.Lock()
	defer mu.Unlock()
	if res.Tokens == (adapter.Tokens{}) {
		res.Tokens = last
	}
	if tripped && err == nil && res.Text != "" && used(res.Tokens) < limit {
		return res, false, nil // it ended with its reply before the stop reached it
	}
	if tripped || used(res.Tokens) >= limit {
		res.Text = ""
		return res, true, nil
	}
	if err != nil {
		res.Text = ""
	}
	return res, false, err
}
