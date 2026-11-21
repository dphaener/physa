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
	"github.com/dphaener/physa/internal/fit"
	"github.com/dphaener/physa/internal/store"
)

// DefaultAppetite is how many tokens the session may use when the run names
// no appetite. It is a first guess, to be tuned from real runs
// (measure-before-budget).
const DefaultAppetite = 150_000

// ErrNoGather is returned by Propose when the store holds no gather.
var ErrNoGather = errors.New("nothing gathered yet; run physa init --gather first")

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
	// Fit says what fitting kept and cut of the gather.
	Fit fit.Report
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
// with what was dropped, what was malformed and what fitting cut.
type Stored struct {
	Proposal  Proposal    `json:"proposal"`
	Dropped   Dropped     `json:"dropped"`
	Malformed []Malformed `json:"malformed"`
	Fit       Fitting     `json:"fit"`
}

// Propose is init's second phase end to end. It reads the stored gather,
// fits it to the input budget the appetite leaves, runs one prompt-only
// session with the mapping skill, checks the reply against the gather and
// stores the proposal, replacing any earlier one. It writes nothing to the
// repo.
//
// With no gather it returns ErrNoGather, and with an appetite too small for
// any input fit.Budget's error; neither resolves an agent. The circuit
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
	budget, err := fit.Budget(appetite, Overhead())
	if err != nil {
		return res, err
	}
	in := fit.Fit(g, budget)
	res.Fit = in.Report
	if s.Agent == nil {
		return res, errors.New("no agent to propose with")
	}
	a, err := s.Agent()
	if err != nil {
		return res, err
	}
	sessionID := draft.NewSessionID()
	if s.Progress != nil {
		sources := 0
		for _, src := range in.Report.Sources {
			sources += src.Tokens
		}
		fmt.Fprintf(s.Progress, "physa init: appetite %s; sources ~%s fitted to ~%d of a %d-token budget; session %s\n",
			plural(int(appetite), "token", "tokens"), plural(sources, "token", "tokens"), in.Report.Used, in.Report.Budget, sessionID)
	}
	out, err := draft.Run(ctx, a, Prompt(in), appetite, sessionID)
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

	body, err := json.Marshal(Stored{Proposal: res.Proposal, Dropped: res.Dropped, Malformed: orEmpty(malformed), Fit: fitting(res.Fit)})
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
