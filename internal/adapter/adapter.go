// Package adapter is what Physa needs from an agent: start a headless session
// with a prompt in a directory, optionally under an id assigned by whoever
// starts it, wait for its result, the tokens it used and what it cost, and
// read a session's log for what learning needs from it (answered asks,
// interventions and checks' reports), redacted. One package per agent implements it; nothing
// here is particular to one agent. An agent may also state its window, the
// most one session holds, and name a refusal as too long.
package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotInstalled is returned by Start when the agent's command is not on
// PATH (or not at the path it was given).
var ErrNotInstalled = errors.New("agent not installed")

// ErrFailed is returned by Wait when the session ended without a result. It is
// wrapped with the agent's own message.
var ErrFailed = errors.New("agent session failed")

// ErrTooLong is returned by Wait, together with ErrFailed, when the session
// was refused because its prompt is more than the agent's model holds. Both
// wrap the agent's own message, so errors.Is finds each.
var ErrTooLong = errors.New("prompt too long for the agent's model")

// ErrUnavailable is returned by Logs.ReadSession when a session's log is
// missing or cannot be read. It is wrapped with the reason.
var ErrUnavailable = errors.New("session log unavailable")

// ErrNoResume is returned by Start, before running anything, when a Spec asks
// to resume a session and the agent cannot.
var ErrNoResume = errors.New("agent cannot resume a session")

// Adapter starts sessions of one agent.
type Adapter interface {
	// Name is the agent's stable name, as Physa records it.
	Name() string
	// Start starts a headless session for s. The session runs until it
	// ends or ctx is cancelled.
	Start(ctx context.Context, s Spec) (Session, error)
}

// Windowed is an Adapter that states how much one of its sessions holds. An
// adapter that does not implement it states no window.
type Windowed interface {
	// Window is the most estimated tokens one session of the agent holds,
	// prompt and reply together.
	Window() int
}

// Window is the window a states, or 0 when a is nil or states none; 0 means
// unknown.
func Window(a Adapter) int {
	if w, ok := a.(Windowed); ok {
		return w.Window()
	}
	return 0
}

// Spec is what a session starts with.
type Spec struct {
	// Prompt is the session's whole first message.
	Prompt string
	// System is text added to the agent's system prompt, kept the same across
	// sessions so the agent can cache it; empty adds none. It is not resent
	// on a resume.
	System string
	// Dir is the directory the session runs in; empty means the current one.
	Dir string
	// Env holds KEY=value entries added to the inherited environment; a key
	// given here wins over the inherited one.
	Env []string
	// PromptOnly makes the session answer from its prompt alone: no tools of
	// any kind.
	PromptOnly bool
	// ReadOnly makes the session read the files under Dir and search them, and
	// nothing else: it has no tool that writes, runs commands or reaches the
	// network. PromptOnly wins when both are set.
	ReadOnly bool
	// SessionID is the id assigned to the session by whoever starts it; empty
	// means none, and the agent picks its own. An adapter whose agent cannot
	// take an assigned id ignores it. One that can may refuse an id not in the
	// agent's form, before running anything.
	SessionID string
	// Resume continues the earlier session SessionID (which must be set)
	// instead of starting one: Prompt is that session's next message. It must
	// run in the same Dir as the session it continues. An adapter whose agent
	// cannot resume returns an error wrapping ErrNoResume before running
	// anything.
	Resume bool
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
	// SessionID is the id the session ran under, as the agent reports it;
	// empty when it did not say.
	SessionID string `json:"session_id"`
	// Tokens is what the session used.
	Tokens Tokens `json:"tokens"`
	// Cost is what the session cost, per model, as the agent reported it on
	// its result; empty when it did not say.
	Cost Cost `json:"cost,omitempty"`
}

// Cost is what a session cost, one entry per model it used, in the order of
// their names.
type Cost []ModelCost

// ModelCost is what one model's part of a session cost: its tokens by kind,
// which do not overlap, and its dollars as the agent reported them, in
// millionths of a dollar so that stored state holds no fractions.
type ModelCost struct {
	// Model is the model's name, as the agent names it in its prices.
	Model string `json:"model"`
	// Input counts the input tokens neither read from nor written to the
	// agent's cache.
	Input int64 `json:"input"`
	// CacheRead counts the input tokens read from the cache.
	CacheRead int64 `json:"cache_read"`
	// CacheWrite5m and CacheWrite1h count the input tokens written to the
	// cache to be kept 5 minutes and 1 hour.
	CacheWrite5m int64 `json:"cache_write_5m"`
	CacheWrite1h int64 `json:"cache_write_1h"`
	// Output counts the tokens the model wrote.
	Output int64 `json:"output"`
	// MicroUSD is what this part cost in millionths of a dollar; zero when
	// the agent did not say.
	MicroUSD int64 `json:"micro_usd"`
}

// Raw is every token of c, each counted once whatever its kind.
func (c ModelCost) Raw() int64 {
	return c.Input + c.CacheRead + c.CacheWrite5m + c.CacheWrite1h + c.Output
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

// Logs reads what past sessions of one agent left in their logs. It is
// read-only: it never writes a log or anything beside one.
//
// ReadSession returns what learning needs from the session with id: the asks
// a person answered, a person's messages mid-run, and checks' reports, from
// the session's lead and its subagents. Tool output, file contents and
// command output are never returned. Every text field of the Log has been
// passed through Redact before it returns. A log that is missing or cannot be
// read gives an error wrapping ErrUnavailable, never another error; parts of
// a log that cannot be read are counted in Log.Skipped instead.
type Logs interface {
	ReadSession(id string) (Log, error)
}

// Log is what one session's log holds for learning, each list in time order.
type Log struct {
	// Session is the session's id.
	Session string
	// Asks are the asks a person answered.
	Asks []Ask
	// Messages are a person's messages sent mid-run (interventions).
	Messages []Message
	// Reports are checks' structured reports.
	Reports []Report
	// Skipped counts the transcripts or lines that could not be read.
	Skipped int
}

// Ask is an ask an agent put to a person, and the person answered.
type Ask struct {
	// At is when the ask was answered.
	At time.Time
	// Agent is empty for the session's lead; otherwise the subagent's name
	// and label, for example "dharness:reviewer review:quality".
	Agent string
	// Text is what the agent asked.
	Text string
	// Options are the choices the agent offered, in order; none when it
	// offered none.
	Options []string
	// Answer is the person's chosen option or own words, with any notes
	// they added appended.
	Answer string
}

// Message is a message sent into a session mid-run through the person's
// channel (an intervention); never the session's first message. That it came
// through the person's channel does not prove a person wrote it.
type Message struct {
	// At is when the message was sent.
	At time.Time
	// Agent is empty for the session's lead; otherwise the subagent's name
	// and label.
	Agent string
	// Text is the message.
	Text string
}

// Report is one structured report from a check.
type Report struct {
	// At is when the check reported.
	At time.Time
	// Agent is the reporting subagent's name and label.
	Agent string
	// Group names the run of the harness the report came in, empty for a
	// subagent the lead started directly. Labels repeat across runs, so
	// reports are paired only within one group.
	Group string
	// Label is the reporting subagent's description as the harness gave it,
	// for example "verify:W4.T1"; empty when none.
	Label string
	// Verdict is the verdict as the harness wrote it; empty when none.
	Verdict string
	// Passed is set by the adapter: the harness's verdict says the check
	// passed, or there is no verdict and no findings. It keeps one harness's
	// words below the adapter.
	Passed bool
	// Findings are the report's findings, in the order reported.
	Findings []Finding
}

// Finding is one finding in a check's report.
type Finding struct {
	// Level is the finding's level as the harness names it; empty when none.
	Level string
	// Text says what is wrong.
	Text string
	// Fixed is set when the report itself marks the finding fixed.
	Fixed bool
}
