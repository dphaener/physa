package plan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/dphaener/physa/internal/progress"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
	"github.com/dphaener/physa/internal/tokens"
)

// SpendIn reports a session's tokens on a pursuit and one of its changes (an empty
// Change is the pursuit itself). Tokens is the session's running total there, not an
// increase, so a report repeated or arriving late changes nothing.
type SpendIn struct {
	Pursuit, Change, Session string
	Tokens                   int64
}

// SpendOut is the pursuit's spend after the report. Warned and Broke are true
// only when this report raised the appetite's notice or the circuit breaker.
type SpendOut struct {
	Spent, Appetite int64
	State           string
	Warned, Broke   bool
}

// By is who stops a pursuit when its spend reaches its appetite.
const By = "circuit breaker"

// Spend adds the increase over the session's held total to the pursuit and the
// change, with event spend.reported, and checks the appetite in the same
// transaction. On an active pursuit: the first time spend reaches 80% of the
// appetite the pursuit's person gets an appetite Needs you item (appetite.warned);
// when it reaches the appetite the circuit breaker stops the pursuit with its
// reason and a breaker item (pursuit.stopped), and the idea may be pursued
// again. A pursuit with no appetite has no notice and no circuit breaker; its
// spend, like spend on a done or stopped pursuit, is still recorded. No
// appetite is raised.
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
		pu, err := lookupPursuit(tx, in.Pursuit)
		if err != nil {
			return err
		}
		out = SpendOut{Spent: pu.spent, Appetite: pu.appetite, State: pu.state}
		if in.Change != "" {
			var one int
			err := tx.QueryRow(`SELECT 1 FROM changes WHERE pursuit_id = ? AND name = ?`, pu.id, in.Change).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				return refuse(ErrUnknownChange, "the pursuit %q has no change %q", pu.slug, in.Change)
			} else if err != nil {
				return err
			}
		}
		var held int64
		err = tx.QueryRow(`SELECT tokens FROM spends WHERE pursuit_id = ? AND change = ? AND session = ?`, pu.id, in.Change, sess).Scan(&held)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		delta := in.Tokens - held
		if delta <= 0 {
			return nil
		}
		at := stamp(tx.Now())
		if _, err := tx.Exec(`INSERT INTO spends (pursuit_id, change, session, tokens, at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (pursuit_id, change, session) DO UPDATE SET tokens = excluded.tokens, at = excluded.at`,
			pu.id, in.Change, sess, in.Tokens, at); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE pursuits SET spent_tokens = spent_tokens + ? WHERE id = ?`, delta, pu.id); err != nil {
			return err
		}
		if in.Change != "" {
			if _, err := tx.Exec(`UPDATE changes SET spent_tokens = spent_tokens + ?, updated_at = ? WHERE pursuit_id = ? AND name = ?`,
				delta, at, pu.id, in.Change); err != nil {
				return err
			}
		}
		out.Spent = pu.spent + delta
		if err := tx.Event("spend.reported", "pursuit", pu.id, "", map[string]any{
			"pursuit": pu.slug, "change": in.Change, "session": sess, "tokens": delta, "spent": out.Spent}); err != nil {
			return err
		}
		if pu.state != stateActive || pu.appetite == 0 {
			return nil
		}
		if out.Warned, err = warn(tx, pu, out.Spent, at); err != nil {
			return err
		}
		if out.Spent >= pu.appetite {
			out.Broke, out.State = true, "stopped"
			return breaker(tx, pu, out.Spent, at)
		}
		return nil
	})
	return out, err
}

// warn raises the appetite notice once per pursuit, when spend reaches 80%.
func warn(tx *state.Tx, pu pursuit, spent int64, at string) (bool, error) {
	if spent*5 < pu.appetite*4 {
		return false, nil
	}
	var one int
	switch err := tx.QueryRow(`SELECT 1 FROM needs_you WHERE pursuit_id = ? AND kind = 'appetite'`, pu.id).Scan(&one); {
	case err == nil:
		return false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	text := fmt.Sprintf("the pursuit %q has spent %s of its %s appetite", pu.slug, tokens.Format(spent), tokens.Format(pu.appetite))
	if _, err := tx.Exec(`INSERT INTO needs_you (person, kind, pursuit_id, text, created_at) VALUES (?, 'appetite', ?, ?, ?)`,
		pu.person, pu.id, text, at); err != nil {
		return false, err
	}
	return true, tx.Event("appetite.warned", "pursuit", pu.id, "", map[string]any{"pursuit": pu.slug, "spent": spent, "appetite": pu.appetite, "person": pu.person})
}

// breaker stops the pursuit, records why and tells its person.
func breaker(tx *state.Tx, pu pursuit, spent int64, at string) error {
	of := fmt.Sprintf("spent %s of %s tokens", tokens.Format(spent), tokens.Format(pu.appetite))
	reason := "circuit breaker: " + of
	if _, err := tx.Exec(`UPDATE pursuits SET state = 'stopped', ended_at = ?, stop_reason = ? WHERE id = ?`, at, reason, pu.id); err != nil {
		return err
	}
	text := fmt.Sprintf("the circuit breaker stopped the pursuit %q: %s; the idea may be pursued again", pu.slug, of)
	if _, err := tx.Exec(`INSERT INTO needs_you (person, kind, pursuit_id, text, created_at) VALUES (?, 'breaker', ?, ?, ?)`,
		pu.person, pu.id, text, at); err != nil {
		return err
	}
	return tx.Event("pursuit.stopped", "pursuit", pu.id, "", map[string]any{"pursuit": pu.slug, "slug": pu.slug, "idea": pu.idea, "reason": reason, "by": By})
}

// Land records each planned open change named in landed as landed, with
// event change.landed, then sets an active pursuit done, with event pursuit.done,
// once every change in its plan has landed. Repeating it changes nothing.
func Land(ctx context.Context, s work.Submitter, slug string, landed map[string]progress.Landing) error {
	return s.Submit(ctx, func(tx *state.Tx) error {
		pu, err := lookupPursuit(tx, slug)
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
			err := tx.QueryRow(`SELECT id FROM changes WHERE pursuit_id = ? AND name = ? AND planned = 1 AND state = 'open'`, pu.id, name).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			} else if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE changes SET state = 'landed', landed_commit = ?, landed_at = ?, updated_at = ? WHERE id = ?`,
				l.Commit, stamp(l.At), at, id); err != nil {
				return err
			}
			if err := tx.Event("change.landed", "change", id, "", map[string]any{"pursuit": pu.slug, "change": name, "commit": l.Commit}); err != nil {
				return err
			}
		}
		if pu.state != stateActive {
			return nil
		}
		var planned, open int
		if err := tx.QueryRow(`SELECT count(*), coalesce(sum(state = 'open'), 0) FROM changes WHERE pursuit_id = ? AND planned = 1`, pu.id).
			Scan(&planned, &open); err != nil {
			return err
		}
		if planned == 0 || open > 0 {
			return nil
		}
		if _, err := tx.Exec(`UPDATE pursuits SET state = 'done', ended_at = ? WHERE id = ?`, at, pu.id); err != nil {
			return err
		}
		return tx.Event("pursuit.done", "pursuit", pu.id, "", map[string]any{"pursuit": pu.slug})
	})
}
