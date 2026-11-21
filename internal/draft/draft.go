// Package draft is learning's third stage (design 08 section 2): one
// prompt-only session per run turns a batch of signals into proposed map
// entries, with the learning skill, under an appetite whose circuit breaker
// stops the session and keeps nothing.
package draft

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/learn"
	"github.com/dphaener/physa/internal/mapfile"
)

// DefaultAppetite is how many tokens one draft session may use when the run
// names no appetite. It is a first guess, to be tuned from real runs (design
// 08 section 7, and the decision measure-before-budget).
const DefaultAppetite = 60000

// Batch is what one session drafts from: the changes of a run that carry
// signals, and the capabilities those signals bear on.
type Batch struct {
	// Changes are the changes with signals, newest first, as triage found
	// them.
	Changes []learn.ChangeSignals
	// Capabilities are the capabilities the map holds that the batch's
	// changes serve or its signals are about, each once, sorted.
	Capabilities []string
}

// NewBatch returns the batch of r: its changes with signals, and the
// capabilities they bear on, which are each change's Capability trailers and
// each signal's capability. Names m does not hold as a capability are left
// out.
func NewBatch(m *mapfile.Map, r learn.Result) Batch {
	has := map[string]bool{}
	if m != nil {
		for _, c := range m.Capabilities {
			has[c.Name] = true
		}
	}
	var b Batch
	seen := map[string]bool{}
	add := func(name string) {
		if has[name] && !seen[name] {
			seen[name] = true
			b.Capabilities = append(b.Capabilities, name)
		}
	}
	for _, c := range r.Learn {
		if len(c.Signals) == 0 {
			continue
		}
		b.Changes = append(b.Changes, c)
		for _, name := range c.Capabilities {
			add(name)
		}
		for _, s := range c.Signals {
			add(s.Capability)
		}
	}
	sort.Strings(b.Capabilities)
	return b
}

// SignalCount is how many signals the batch holds.
func (b Batch) SignalCount() int {
	n := 0
	for _, c := range b.Changes {
		n += len(c.Signals)
	}
	return n
}

// ChangeCount is how many changes carry the batch's signals.
func (b Batch) ChangeCount() int { return len(b.Changes) }

// CapabilityCount is how many capabilities the batch bears on.
func (b Batch) CapabilityCount() int { return len(b.Capabilities) }

// Empty reports whether the batch holds no signal, so that no session is
// worth starting.
func (b Batch) Empty() bool { return b.SignalCount() == 0 }

// Outcome is how one draft session ended.
type Outcome struct {
	// Text is the agent's answer; empty when the circuit breaker stopped the
	// session, whatever had arrived.
	Text string
	// Model is the name the agent reported for what it ran on.
	Model string
	// Tokens is what the session used: its result's count, or the last the
	// agent reported when it ended without one.
	Tokens adapter.Tokens
	// Stopped says the circuit breaker stopped the session at the appetite.
	Stopped bool
	// SessionID is the id the session ran under, as the agent reported it;
	// empty when the agent did not say.
	SessionID string
}

// NewSessionID returns a fresh version 4 UUID, 8-4-4-4-12 in lower-case hex,
// for a draft session to run under.
func NewSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("draft: no randomness: " + err.Error())
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Run starts one prompt-only session of a with prompt, in a fresh empty
// directory that is removed afterwards, and waits for it. The appetite counts
// every token in and out, cached input included: when a report of the
// session's tokens reaches it, the circuit breaker stops the session and the
// Outcome is Stopped, with no text and a nil error. Any other failure is the
// adapter's own error (adapter.ErrNotInstalled, adapter.ErrFailed), with the
// tokens the session reported when it reported any. sessionID is the id the
// session is asked to run under (see NewSessionID); the Outcome's SessionID is
// the one the agent reported, which is empty for an agent that cannot take one.
func Run(ctx context.Context, a adapter.Adapter, prompt string, appetite int64, sessionID string) (Outcome, error) {
	if appetite <= 0 {
		return Outcome{}, fmt.Errorf("the appetite must be a positive number of tokens, not %d", appetite)
	}
	if a == nil {
		return Outcome{}, errors.New("no agent to draft with")
	}
	if strings.TrimSpace(prompt) == "" {
		return Outcome{}, errors.New("nothing to draft from")
	}
	dir, err := os.MkdirTemp("", "physa-draft-")
	if err != nil {
		return Outcome{}, err
	}
	defer os.RemoveAll(dir)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu      sync.Mutex
		last    adapter.Tokens
		stopped bool
	)
	over := func(t adapter.Tokens) bool { return t.Input+t.Output >= appetite }
	breaker := func(t adapter.Tokens) {
		mu.Lock()
		defer mu.Unlock()
		last = t
		if over(t) {
			stopped = true
			cancel()
		}
	}
	s, err := a.Start(ctx, adapter.Spec{Prompt: prompt, SessionID: sessionID, Dir: dir, PromptOnly: true, Usage: breaker})
	if err != nil {
		return Outcome{}, err
	}
	res, err := s.Wait()
	mu.Lock()
	defer mu.Unlock()
	out := Outcome{Text: res.Text, Model: res.Model, Tokens: res.Tokens, SessionID: res.SessionID}
	if stopped || out.Tokens == (adapter.Tokens{}) {
		out.Tokens = last
	}
	// A session whose result alone shows the appetite spent is stopped too:
	// what it cost is the same whether or not the agent reported as it went.
	if stopped || over(out.Tokens) {
		out.Text, out.Stopped = "", true
		return out, nil
	}
	if err != nil {
		out.Text = ""
		return out, err
	}
	return out, nil
}
