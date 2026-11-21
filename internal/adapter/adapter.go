// Package adapter is what Physa needs from an agent: start a headless session
// with a prompt in a directory, wait for its result, and the tokens it used.
// One package per agent implements it; nothing here is particular to one
// agent.
package adapter

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotInstalled is returned by Start when the agent's command is not on
// PATH (or not at the path it was given).
var ErrNotInstalled = errors.New("agent not installed")

// ErrFailed is returned by Wait when the session ended without a result. It is
// wrapped with the agent's own message.
var ErrFailed = errors.New("agent session failed")

// Adapter starts sessions of one agent.
type Adapter interface {
	// Name is the agent's stable name, as Physa records it.
	Name() string
	// Start starts a headless session for s. The session runs until it
	// ends or ctx is cancelled.
	Start(ctx context.Context, s Spec) (Session, error)
}

// Spec is what a session starts with.
type Spec struct {
	// Prompt is the session's whole first message.
	Prompt string
	// Dir is the directory the session runs in; empty means the current one.
	Dir string
	// Env holds KEY=value entries added to the inherited environment; a key
	// given here wins over the inherited one.
	Env []string
	// PromptOnly makes the session answer from its prompt alone: no tools of
	// any kind.
	PromptOnly bool
	// Usage, when set, is called with the session's tokens so far each time the
	// agent reports them, from the goroutine running Wait. The last call
	// carries the same tokens as Result.
	Usage func(Tokens)
}

// Session is a running headless session.
//
// Later calls (send a message, stream output, progress, blocked, end) are
// added to Session by the engine (plan step 5) without changing these.
type Session interface {
	// Wait blocks until the session ends and returns its result. When the
	// session ended without a result the error wraps ErrFailed, and Result
	// still holds what the session reported (model, tokens).
	Wait() (Result, error)
}

// Result is what a session ended with.
type Result struct {
	// Text is the agent's final answer.
	Text string `json:"text"`
	// Model is the name of the language model the agent ran on, as the agent
	// reports it; empty when it did not say.
	Model string `json:"model"`
	// Tokens is what the session used.
	Tokens Tokens `json:"tokens"`
}

// Tokens counts the tokens a session used.
type Tokens struct {
	// Input counts every input token, cached ones included.
	Input int64 `json:"input"`
	// CachedInput is the part of Input read from the agent's cache.
	CachedInput int64 `json:"cached_input"`
	// Output counts the tokens the agent wrote.
	Output int64 `json:"output"`
}

// String reads like "12345 in (10000 cached), 678 out".
func (t Tokens) String() string {
	return fmt.Sprintf("%d in (%d cached), %d out", t.Input, t.CachedInput, t.Output)
}
