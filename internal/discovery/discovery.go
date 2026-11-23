// Package discovery starts one read-only agent session in the repo that names
// the sources a map should be built from, each with a kind and a reason. It
// has no appetite and no circuit breaker: init is never bounded (design 10
// Q6). The session may read and search the repo's files and
// never writes. Checking what it names is gather's job, not this package's.
package discovery

import (
	"context"
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
)

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
	// Malformed counts the entries, or the whole reply, that could not be
	// read as a source; they are dropped.
	Malformed int
}

// Run starts the session and parses what it names. An agent that cannot be
// resolved or a session that fails is an error; a reply that names nothing
// readable is not.
func Run(ctx context.Context, s Session) (Result, error) {
	agent, err := s.Agent()
	if err != nil {
		return Result{}, err
	}
	if s.Progress != nil {
		fmt.Fprintln(s.Progress, "physa init: discovery: the agent reads the repo for sources")
	}
	spec := adapter.Spec{Prompt: Prompt(), Dir: s.Root, ReadOnly: true, SessionID: draft.NewSessionID()}
	sess, err := agent.Start(ctx, spec)
	if err != nil {
		return Result{}, err
	}
	res, err := sess.Wait()
	out := Result{Model: res.Model, Tokens: res.Tokens}
	if err != nil {
		return out, err
	}
	out.Sources, out.Malformed = Parse(res.Text)
	return out, nil
}
