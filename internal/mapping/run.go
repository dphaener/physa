package mapping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

// DefaultAppetite is how many tokens the session may use when the run names
// no appetite. It is a first guess, to be tuned from real runs
// (measure-before-budget).
const DefaultAppetite = 150_000

// ReplyReserve is the part of the appetite kept for propose's reply. It is
// a first guess, to be tuned from real runs (measure-before-budget).
const ReplyReserve = 16_000

// ErrNoGather is returned by Propose when the store holds no gather.
var ErrNoGather = errors.New("nothing gathered yet; run physa init --gather first")

// TooSmall is returned by Propose when its prompt plus ReplyReserve does not
// fit in the appetite. Nothing is cut to make it fit, and nothing was spent:
// the session never started.
type TooSmall struct {
	// Prompt is the prompt's estimated tokens, Reply the reply's reserve.
	Prompt, Reply int
	// Appetite is the appetite propose had.
	Appetite int64
}

// Need is the appetite propose needs, an estimate.
func (e *TooSmall) Need() int64 { return int64(e.Prompt + e.Reply) }

func (e *TooSmall) Error() string {
	return fmt.Sprintf("propose needs an appetite of about %s tokens (its prompt about %s, its reply %s) and has %s; it did not start, and nothing was spent on it",
		reading.Thousands(e.Need()), reading.Thousands(int64(e.Prompt)), reading.Thousands(int64(e.Reply)), reading.Thousands(e.Appetite))
}

// Read is all of g as propose reads it, whole.
func Read(g store.Gather) reading.Input { return reading.Of(g, reading.Propose) }

// Estimate is the estimated tokens of propose's prompt for g.
func Estimate(g store.Gather) int { return reading.Tokens(Prompt(Read(g))) }

// Session says how Propose runs its session.
type Session struct {
	// Agent resolves the agent to propose with. Propose calls it only when
	// there is a gather and the appetite leaves room for it.
	Agent func() (adapter.Adapter, error)
	// Appetite is the most tokens the session may use; zero means
	// DefaultAppetite.
	Appetite int64
	// Progress receives one line before the session starts; nil drops it.
	Progress io.Writer
	// Now is the clock the stored proposal is dated by; nil means time.Now.
	Now func() time.Time
}

// Store is what Propose needs of the store: the gather to read and a place
// for the proposal.
type Store interface {
	Gathered() (store.Gather, bool, error)
	PutProposal(store.Proposal) error
}

// Result is how one run of Propose ended.
type Result struct {
	// Proposal is the checked proposal; empty when Stopped.
	Proposal Proposal
	// Dropped is what Check took out of the reply, and why.
	Dropped Dropped
	// Malformed are the items of the reply that were not what they should be.
	Malformed []Malformed
	// Read says what the session read of the gather, all of it whole.
	Read reading.Report
	// PromptTokens is the prompt's estimated tokens.
	PromptTokens int
	// SessionID is the id the session ran under, as the agent reported it.
	SessionID string
	// Model is the name the agent reported for what it ran on.
	Model string
	// Tokens is what the session used.
	Tokens adapter.Tokens
	// Appetite is the appetite the session ran under.
	Appetite int64
	// Stopped says the circuit breaker stopped the session at the appetite:
	// nothing was proposed and nothing stored.
	Stopped bool
}

// Stored is the JSON form of store.Proposal's Body: the checked proposal
// with what was dropped, what was malformed and what the session read.
type Stored struct {
	Proposal  Proposal    `json:"proposal"`
	Dropped   Dropped     `json:"dropped"`
	Malformed []Malformed `json:"malformed"`
	Read      Reading     `json:"read"`
}

// Propose is init's second phase end to end. It reads the stored gather,
// all of it whole, runs one prompt-only session with the mapping skill under
// the appetite, checks the reply against the gather and stores the proposal,
// replacing any earlier one. It writes nothing to the repo.
//
// With no gather it returns ErrNoGather, and with an appetite that cannot
// hold the prompt plus ReplyReserve a *TooSmall naming the appetite needed;
// neither resolves an agent, and nothing is cut to fit. The circuit
// breaker stopping the session is not an error: the Result is Stopped, and
// nothing is stored. An agent that fails, or a reply that cannot be read
// (the error starts "the session's output: "), stores nothing either; the
// Result then still says what the session used.
func Propose(ctx context.Context, st Store, s Session) (Result, error) {
	appetite := s.Appetite
	if appetite == 0 {
		appetite = DefaultAppetite
	}
	res := Result{Appetite: appetite}
	g, ok, err := st.Gathered()
	if err != nil {
		return res, err
	}
	if !ok {
		return res, ErrNoGather
	}
	in := Read(g)
	prompt := Prompt(in)
	res.Read, res.PromptTokens = in.Report, reading.Tokens(prompt)
	if int64(res.PromptTokens+ReplyReserve) > appetite {
		return res, &TooSmall{Prompt: res.PromptTokens, Reply: ReplyReserve, Appetite: appetite}
	}
	if s.Agent == nil {
		return res, errors.New("no agent to propose with")
	}
	a, err := s.Agent()
	if err != nil {
		return res, err
	}
	sessionID := draft.NewSessionID()
	if s.Progress != nil {
		fmt.Fprintf(s.Progress, "physa init: appetite %s; propose reads its sources whole, ~%s, in a prompt of ~%s plus %d for the reply; session %s\n",
			plural(int(appetite), "token", "tokens"), plural(in.Report.Tokens, "token", "tokens"), plural(res.PromptTokens, "token", "tokens"), ReplyReserve, sessionID)
	}
	out, err := draft.Run(ctx, a, prompt, appetite, sessionID)
	res.SessionID, res.Model, res.Tokens = out.SessionID, out.Model, out.Tokens
	if err != nil {
		return res, err
	}
	if out.Stopped {
		res.Stopped = true
		return res, nil
	}
	reply, malformed, err := Parse(out.Text)
	if err != nil {
		return res, fmt.Errorf("the session's output: %w", err)
	}
	res.Proposal, res.Dropped = Check(reply, g)
	res.Malformed = malformed

	body, err := json.Marshal(Stored{Proposal: res.Proposal, Dropped: res.Dropped, Malformed: orEmpty(malformed), Read: read(res)})
	if err != nil {
		return res, err
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	err = st.PutProposal(store.Proposal{
		GatherAt: g.Run.At, At: now().UTC(),
		SessionID: res.SessionID, Model: res.Model, Appetite: appetite,
		Input: res.Tokens.Input, CachedInput: res.Tokens.CachedInput, Output: res.Tokens.Output,
		Body: body,
	})
	return res, err
}

// orEmpty returns list, or an empty one when it is nil, so that it is
// written [] and never null.
func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
