package mapping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapping/shareview"
	"github.com/dphaener/physa/internal/reading"
	"github.com/dphaener/physa/internal/store"
)

// ReplyReserve is what init's estimate counts for propose's reply. It is a
// first guess, to be tuned from real runs (measure-before-budget).
const ReplyReserve = 16_000

// ErrNoGather is returned by Propose when the store holds no gather.
var ErrNoGather = errors.New("nothing gathered yet; run physa init --gather first")

// Read is all of g as propose reads it, whole.
func Read(g store.Gather) reading.Input { return reading.Of(g, reading.Propose) }

// Estimate is the estimated tokens of propose's prompt for g.
func Estimate(g store.Gather) int { return reading.Tokens(Prompt(Read(g))) }

// Session says how Propose runs its session.
type Session struct {
	// Agent resolves the agent to propose with. Propose calls it only when
	// there is a gather.
	Agent func() (adapter.Adapter, error)
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
	// Proposal is the checked proposal.
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
	// Shares and Merge are set only for a proposal made in shares: what each
	// share's session and the merge's read and used. Then Read is the whole
	// gather's, PromptTokens is every prompt's together, and Tokens is every
	// session's together. Both are empty for one session.
	Shares []Share
	Merge  *Share
}

// Share is what one share's session, or the merge's, read and used.
type Share = shareview.Share

// Stored is the JSON form of store.Proposal's Body: the checked proposal
// with what was dropped, what was malformed and what the session read.
type Stored struct {
	Proposal  Proposal    `json:"proposal"`
	Dropped   Dropped     `json:"dropped"`
	Malformed []Malformed `json:"malformed"`
	Read      Reading     `json:"read"`
	// Sessions are every session the proposal ran, the shares' in order and
	// then the merge's; empty for one session, whose id the store holds.
	Sessions []string `json:"sessions,omitempty"`
}

// Ran returns the sessions the proposal ran: Sessions, or just id, the
// proposal's own, when it ran one session.
func (s Stored) Ran(id string) []string {
	if len(s.Sessions) == 0 {
		return []string{id}
	}
	return append([]string{}, s.Sessions...)
}

// Propose is init's second phase end to end. It reads the stored gather,
// all of it whole, runs one prompt-only session with the mapping skill,
// checks the reply against the gather and stores the proposal, replacing any
// earlier one. It writes nothing to the repo. The session has no appetite and
// no circuit breaker: init is never bounded (design 10 Q6).
//
// With no gather it returns ErrNoGather and resolves no agent. An agent that fails, or a reply that cannot be read
// (the error starts "the session's output: "), stores nothing either; the
// Result then still says what the session used.
func Propose(ctx context.Context, st Store, s Session) (Result, error) {
	var res Result
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
	if s.Agent == nil {
		return res, errors.New("no agent to propose with")
	}
	a, err := s.Agent()
	if err != nil {
		return res, err
	}
	sessionID := draft.NewSessionID()
	if s.Progress != nil {
		fmt.Fprintf(s.Progress, "physa init: propose reads its sources whole, ~%s, in a prompt of ~%s; session %s\n",
			plural(in.Report.Tokens, "token", "tokens"), plural(res.PromptTokens, "token", "tokens"), sessionID)
	}
	out, err := draft.Run(ctx, a, prompt, math.MaxInt64, sessionID) // no circuit breaker: init is never bounded
	res.SessionID, res.Model, res.Tokens = out.SessionID, out.Model, out.Tokens
	if err != nil {
		return res, err
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
		SessionID: res.SessionID, Model: res.Model,
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
