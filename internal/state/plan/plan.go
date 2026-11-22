// Package plan is the engine's commands and reads on a bet's plan and its
// progress: each revision kept with its reason, which change a session works
// on, tokens spent added and checked against the appetite in the same
// transaction, the circuit breaker, and the Needs you items they raise; each
// change one command with its event.
package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/plan"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
)

// The kinds of Refusal this package adds to work's. Each is a *work.Refusal,
// so the API replies bad_request with its message.
var (
	ErrPlanInvalid   = errors.New("plan invalid")
	ErrNoPlan        = errors.New("no plan")
	ErrNoReason      = errors.New("no reason")
	ErrUnknownChange = errors.New("unknown change")
	ErrNoSession     = errors.New("no session")
	ErrBadTokens     = errors.New("bad tokens")
)

func refuse(kind error, format string, args ...any) error {
	return &work.Refusal{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// SetIn stores Plan as the bet's next revision. Reason may be empty only for
// the first.
type SetIn struct {
	Bet, Reason, Person string
	Plan                plan.Plan
}

// WorkIn records that Session works on the bet's planned change.
type WorkIn struct {
	Session, Bet, Change string
}

const stateActive = "active"

var (
	sessionPat = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	slugPat    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// parse reads a stored time; an empty one is the zero time.
func parse(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}

func session(s string) (string, error) {
	switch s = strings.TrimSpace(s); {
	case s == "":
		return "", refuse(ErrNoSession, "no session is named; the session's id is needed")
	case !sessionPat.MatchString(s):
		return "", refuse(ErrNoSession, "the session %q is not a session id: letters, digits and dashes only", s)
	}
	return s, nil
}

// bet is a bet's row, as the commands here need it.
type bet struct {
	id                int64
	slug, idea, state string
	person            string
	appetite, spent   int64
}

func lookupBet(tx *state.Tx, slug string) (bet, error) {
	b := bet{slug: slug}
	err := tx.QueryRow(`SELECT b.id, i.slug, b.state, b.person, b.appetite_tokens, b.spent_tokens
		FROM bets b JOIN ideas i ON i.id = b.idea_id WHERE b.slug = ?`, slug).
		Scan(&b.id, &b.idea, &b.state, &b.person, &b.appetite, &b.spent)
	if errors.Is(err, sql.ErrNoRows) {
		return b, refuse(work.ErrUnknownBet, "no bet has the slug %q", slug)
	}
	return b, err
}

func active(b bet, what string) error {
	if b.state != stateActive {
		return refuse(work.ErrNotActive, "the bet %q is %s, not active; only an active bet %s", b.slug, b.state, what)
	}
	return nil
}

// checkShape refuses a plan that internal/plan would never have passed: the
// engine stores names and capabilities as rows, so it checks them itself.
func checkShape(p plan.Plan) error {
	if len(p.Changes) == 0 {
		return refuse(ErrPlanInvalid, "the plan has no changes")
	}
	seen := map[string]bool{}
	for _, c := range p.Changes {
		switch {
		case !slugPat.MatchString(c.Name):
			return refuse(ErrPlanInvalid, "the plan's change %q is not named by a slug", c.Name)
		case seen[c.Name]:
			return refuse(ErrPlanInvalid, "the plan names the change %q twice", c.Name)
		case strings.TrimSpace(c.Capability) == "":
			return refuse(ErrPlanInvalid, "the plan's change %q names no capability", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

// Set stores in.Plan, already checked by internal/plan, as the bet's next
// revision with event plan.revised. Only an active bet takes a plan; a
// revision after the first needs a reason. The current plan's changes are
// rows; one a revision drops stays, no longer planned, with its spend.
func Set(ctx context.Context, s work.Submitter, in SetIn) (Detail, error) {
	who := strings.TrimSpace(in.Person)
	if who == "" {
		return Detail{}, refuse(work.ErrNoPerson, "a person must be named for this change")
	}
	if err := checkShape(in.Plan); err != nil {
		return Detail{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	body, err := json.Marshal(in.Plan.Changes)
	if err != nil {
		return Detail{}, err
	}
	var out Detail
	err = s.Submit(ctx, func(tx *state.Tx) error {
		b, err := lookupBet(tx, in.Bet)
		if err != nil {
			return err
		}
		if err := active(b, "takes a plan"); err != nil {
			return err
		}
		at := stamp(tx.Now())
		var planID int64
		var n int
		switch err := tx.QueryRow(`SELECT p.id, coalesce((SELECT max(n) FROM plan_revisions r WHERE r.plan_id = p.id), 0)
			FROM plans p WHERE p.bet_id = ?`, b.id).Scan(&planID, &n); {
		case errors.Is(err, sql.ErrNoRows):
			res, err := tx.Exec(`INSERT INTO plans (bet_id, created_at) VALUES (?, ?)`, b.id, at)
			if err != nil {
				return err
			}
			if planID, err = res.LastInsertId(); err != nil {
				return err
			}
		case err != nil:
			return err
		}
		n++
		if n > 1 && reason == "" {
			return refuse(ErrNoReason, "a revision of the plan needs a reason: what shaping missed")
		}
		if _, err := tx.Exec(`INSERT INTO plan_revisions (plan_id, n, changes, reason, person, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			planID, n, string(body), reason, who, at); err != nil {
			return err
		}
		names := plan.Names(in.Plan)
		for _, c := range in.Plan.Changes {
			if _, err := tx.Exec(`INSERT INTO changes (bet_id, name, capability, planned, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)
				ON CONFLICT (bet_id, name) DO UPDATE SET capability = excluded.capability, planned = 1, updated_at = excluded.updated_at`,
				b.id, c.Name, c.Capability, at, at); err != nil {
				return err
			}
		}
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ")
		args := []any{at, b.id}
		for _, name := range names {
			args = append(args, name)
		}
		if _, err := tx.Exec(`UPDATE changes SET planned = 0, updated_at = ? WHERE bet_id = ? AND planned = 1 AND name NOT IN (`+marks+`)`, args...); err != nil {
			return err
		}
		if err := tx.Event("plan.revised", "plan", planID, who, map[string]any{"bet": b.slug, "n": n, "reason": reason, "changes": names}); err != nil {
			return err
		}
		out, err = detail(tx, b, false)
		return err
	})
	return out, err
}

// Work records that in.Session works on the bet's planned change, replacing
// the change it worked on before, with event change.in_work. The bet must be
// active and the change planned in its current revision.
func Work(ctx context.Context, s work.Submitter, in WorkIn) error {
	sess, err := session(in.Session)
	if err != nil {
		return err
	}
	return s.Submit(ctx, func(tx *state.Tx) error {
		b, err := lookupBet(tx, in.Bet)
		if err != nil {
			return err
		}
		if err := active(b, "is worked on"); err != nil {
			return err
		}
		var changeID int64
		err = tx.QueryRow(`SELECT id FROM changes WHERE bet_id = ? AND name = ? AND planned = 1`, b.id, in.Change).Scan(&changeID)
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrUnknownChange, "the bet %q has no planned change %q", b.slug, in.Change)
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO session_changes (session, change_id, at) VALUES (?, ?, ?)
			ON CONFLICT (session) DO UPDATE SET change_id = excluded.change_id, at = excluded.at`, sess, changeID, stamp(tx.Now())); err != nil {
			return err
		}
		return tx.Event("change.in_work", "change", changeID, "", map[string]any{"bet": b.slug, "change": in.Change, "session": sess})
	})
}
