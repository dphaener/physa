package plan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/dphaener/physa/internal/pitch"
	"github.com/dphaener/physa/internal/progress"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
)

// SpendIn reports a session's tokens on a bet and one of its changes (an empty
// Change is the bet itself). Tokens is the session's running total there, not an
// increase, so a report repeated or arriving late changes nothing.
type SpendIn struct {
	Bet, Change, Session string
	Tokens               int64
}

// SpendOut is the bet's spend after the report. Warned and Broke are true
// only when this report raised the appetite's notice or the circuit breaker.
type SpendOut struct {
	Spent, Appetite int64
	State           string
	Warned, Broke   bool
}

// By is who stops a bet when its spend reaches its appetite.
const By = "circuit breaker"

// Spend adds the increase over the session's held total to the bet and the
// change, with event spend.reported, and checks the appetite in the same
// transaction. On an active bet: the first time spend reaches 80% of the
// appetite the bet's person gets an appetite Needs you item (appetite.warned);
// when it reaches the appetite the circuit breaker stops the bet with its
// reason and a breaker item (bet.stopped), and the bet goes back to shaping.
// Spend on a done or stopped bet is still recorded. No appetite is raised.
func Spend(ctx context.Context, s work.Submitter, in SpendIn) (SpendOut, error) {
	if in.Tokens < 0 {
		return SpendOut{}, refuse(ErrBadTokens, "a session's tokens are a running total of 0 or more, not %d", in.Tokens)
	}
	sess, err := session(in.Session)
	if err != nil {
		return SpendOut{}, err
	}
	var out SpendOut
	err = s.Submit(ctx, func(tx *state.Tx) error {
		b, err := lookupBet(tx, in.Bet)
		if err != nil {
			return err
		}
		out = SpendOut{Spent: b.spent, Appetite: b.appetite, State: b.state}
		if in.Change != "" {
			var one int
			err := tx.QueryRow(`SELECT 1 FROM changes WHERE bet_id = ? AND name = ?`, b.id, in.Change).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				return refuse(ErrUnknownChange, "the bet %q has no change %q", b.slug, in.Change)
			} else if err != nil {
				return err
			}
		}
		var held int64
		err = tx.QueryRow(`SELECT tokens FROM spends WHERE bet_id = ? AND change = ? AND session = ?`, b.id, in.Change, sess).Scan(&held)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		delta := in.Tokens - held
		if delta <= 0 {
			return nil
		}
		at := stamp(tx.Now())
		if _, err := tx.Exec(`INSERT INTO spends (bet_id, change, session, tokens, at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (bet_id, change, session) DO UPDATE SET tokens = excluded.tokens, at = excluded.at`,
			b.id, in.Change, sess, in.Tokens, at); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE bets SET spent_tokens = spent_tokens + ? WHERE id = ?`, delta, b.id); err != nil {
			return err
		}
		if in.Change != "" {
			if _, err := tx.Exec(`UPDATE changes SET spent_tokens = spent_tokens + ?, updated_at = ? WHERE bet_id = ? AND name = ?`,
				delta, at, b.id, in.Change); err != nil {
				return err
			}
		}
		out.Spent = b.spent + delta
		if err := tx.Event("spend.reported", "bet", b.id, "", map[string]any{
			"bet": b.slug, "change": in.Change, "session": sess, "tokens": delta, "spent": out.Spent}); err != nil {
			return err
		}
		if b.state != stateActive {
			return nil
		}
		if out.Warned, err = warn(tx, b, out.Spent, at); err != nil {
			return err
		}
		if out.Spent >= b.appetite {
			out.Broke, out.State = true, "stopped"
			return breaker(tx, b, out.Spent, at)
		}
		return nil
	})
	return out, err
}

// warn raises the appetite notice once per bet, when spend reaches 80%.
func warn(tx *state.Tx, b bet, spent int64, at string) (bool, error) {
	if spent*5 < b.appetite*4 {
		return false, nil
	}
	var one int
	switch err := tx.QueryRow(`SELECT 1 FROM needs_you WHERE bet_id = ? AND kind = 'appetite'`, b.id).Scan(&one); {
	case err == nil:
		return false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	text := fmt.Sprintf("the bet %q has spent %s of its %s appetite", b.slug, pitch.Tokens(spent), pitch.Tokens(b.appetite))
	if _, err := tx.Exec(`INSERT INTO needs_you (person, kind, bet_id, text, created_at) VALUES (?, 'appetite', ?, ?, ?)`,
		b.person, b.id, text, at); err != nil {
		return false, err
	}
	return true, tx.Event("appetite.warned", "bet", b.id, "", map[string]any{"bet": b.slug, "spent": spent, "appetite": b.appetite, "person": b.person})
}

// breaker stops the bet, records why and tells its person.
func breaker(tx *state.Tx, b bet, spent int64, at string) error {
	of := fmt.Sprintf("spent %s of %s tokens", pitch.Tokens(spent), pitch.Tokens(b.appetite))
	reason := "circuit breaker: " + of
	if _, err := tx.Exec(`UPDATE bets SET state = 'stopped', ended_at = ?, stop_reason = ? WHERE id = ?`, at, reason, b.id); err != nil {
		return err
	}
	text := fmt.Sprintf("the circuit breaker stopped the bet %q: %s; it goes back to shaping", b.slug, of)
	if _, err := tx.Exec(`INSERT INTO needs_you (person, kind, bet_id, text, created_at) VALUES (?, 'breaker', ?, ?, ?)`,
		b.person, b.id, text, at); err != nil {
		return err
	}
	return tx.Event("bet.stopped", "bet", b.id, "", map[string]any{"bet": b.slug, "slug": b.slug, "idea": b.idea, "reason": reason, "by": By})
}

// Land records each planned open change named in landed as landed, with
// event change.landed, then sets an active bet done, with event bet.done,
// once every change in its plan has landed. Repeating it changes nothing.
func Land(ctx context.Context, s work.Submitter, slug string, landed map[string]progress.Landing) error {
	return s.Submit(ctx, func(tx *state.Tx) error {
		b, err := lookupBet(tx, slug)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(landed))
		for name := range landed {
			names = append(names, name)
		}
		slices.Sort(names)
		at := stamp(tx.Now())
		for _, name := range names {
			l := landed[name]
			var id int64
			err := tx.QueryRow(`SELECT id FROM changes WHERE bet_id = ? AND name = ? AND planned = 1 AND state = 'open'`, b.id, name).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			} else if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE changes SET state = 'landed', landed_commit = ?, landed_at = ?, updated_at = ? WHERE id = ?`,
				l.Commit, stamp(l.At), at, id); err != nil {
				return err
			}
			if err := tx.Event("change.landed", "change", id, "", map[string]any{"bet": b.slug, "change": name, "commit": l.Commit}); err != nil {
				return err
			}
		}
		if b.state != stateActive {
			return nil
		}
		var planned, open int
		if err := tx.QueryRow(`SELECT count(*), coalesce(sum(state = 'open'), 0) FROM changes WHERE bet_id = ? AND planned = 1`, b.id).
			Scan(&planned, &open); err != nil {
			return err
		}
		if planned == 0 || open > 0 {
			return nil
		}
		if _, err := tx.Exec(`UPDATE bets SET state = 'done', ended_at = ? WHERE id = ?`, at, b.id); err != nil {
			return err
		}
		return tx.Event("bet.done", "bet", b.id, "", map[string]any{"bet": b.slug})
	})
}
