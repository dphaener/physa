// Package evidence is the engine's record of engineering evidence: a durable
// poll that observes version control, and the forge when there is one, and
// keeps each fact once in the store, with the facts' retention and their
// reads. The poll's timer only writes an intent; the intent reads the repo
// and the forge with no transaction open, and its Record stores the facts and
// moves the watermark in the transaction that marks it done, so a kill at any
// moment loses nothing and doubles nothing. Facts are observed, never
// reported, and hold no weight (design 05 section 6.5). The engine polls at
// once on open, every 5 minutes while a bet is active and daily otherwise,
// and backs off from a forge that fails.
package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dphaener/physa/internal/forge"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
)

// The kinds this package registers, and the keys of its timers.
const (
	PollKind   = "evidence.poll"
	PollKey    = "poll"
	ExpireKind = "evidence.expire"
	ExpireKey  = "retention"
)

const (
	activeEvery = 5 * time.Minute      // the poll's cadence while a bet is active
	idleEvery   = 24 * time.Hour       // and otherwise
	expireEvery = 24 * time.Hour       // the retention timer's cadence
	keepFacts   = 365 * 24 * time.Hour // evidence is kept a year (05 Q14)
	keepPolls   = 7 * 24 * time.Hour   // done poll intents are kept a week
	forgeFirst  = 5 * time.Minute      // the forge's first backoff, doubling
	forgeMost   = 6 * time.Hour        // to at most this
)

// Options returns the timer and intent kinds of the evidence poll, to pass
// in state.Options. newForge gives the forge of a repo root; nil, or a forge
// that cannot list merged PRs, means version control only.
func Options(newForge func(root string) forge.Forge) (timers []state.TimerKind, intents []state.IntentKind) {
	timers = []state.TimerKind{
		{Name: PollKind, Key: PollKey, Delay: 0, Fire: firePoll},
		{Name: ExpireKind, Key: ExpireKey, Delay: expireEvery, Fire: expire},
	}
	intents = []state.IntentKind{{
		Name:   PollKind,
		Repeat: "A poll only reads, and its facts are stored with INSERT OR IGNORE and its watermark moved only from the one it started at, so running it twice records nothing twice.",
		Check:  checkPoll,
		Do:     func(ctx context.Context, in state.Intent) (string, error) { return doPoll(ctx, in, newForge) },
		Record: recordPoll,
	}}
	return timers, intents
}

// Wake makes the poll due now, as one command. When this engine does not
// register the poll there is no timer to move, and Wake does nothing.
func Wake(ctx context.Context, s work.Submitter) error {
	return s.Submit(ctx, func(tx *state.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM timers WHERE kind = ? AND key = ?`, PollKind, PollKey).Scan(&n); err != nil || n == 0 {
			return err
		}
		return tx.SetTimer(PollKind, PollKey, tx.Now(), nil)
	})
}

// pollIn is a poll intent's payload: the store, the watermark it starts
// from, the forge's next try and the time the poll was asked for.
type pollIn struct {
	Store     string    `json:"store"`
	Mark      mark      `json:"mark"`
	ForgeNext time.Time `json:"forge_next,omitzero"`
	At        time.Time `json:"at"`
}

func checkPoll(payload json.RawMessage) error {
	var in pollIn
	if err := json.Unmarshal(payload, &in); err != nil {
		return err
	}
	if in.Store == "" || in.At.IsZero() {
		return errors.New("a poll needs the store directory and the time it was asked for")
	}
	return nil
}
