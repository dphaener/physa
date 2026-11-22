package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/plan"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
)

// Reads run as commands that write nothing, as work's do, except Current,
// which reads a store opened read-only so the hook never needs the engine.

// Detail is a bet's current plan: its revision number, who wrote it, when
// and why, its changes, and how many times it was replanned. History holds
// every revision, oldest first, when asked for.
type Detail struct {
	Bet            string
	N, Replans     int
	Reason, Person string
	At             time.Time
	Changes        []plan.Change
	History        []Revision
}

// Revision is one stored revision of a plan and the names of its changes.
type Revision struct {
	N              int
	Reason, Person string
	At             time.Time
	Changes        []string
}

// LandedChange is a planned change that has landed on the default branch.
type LandedChange struct {
	Change, Commit string
	At             time.Time
}

// ChangeSpend is the tokens spent on one change; an empty Change is the bet itself.
type ChangeSpend struct {
	Change string
	Tokens int64
}

// Need is an open Needs you item about a bet.
type Need struct {
	Kind, Text string
	At         time.Time
}

// Facts is a bet's progress, as facts and never one number: the changes
// planned, landed and in work, asks open, tokens spent against the appetite,
// replans, spend per change (largest first) and its open Needs you items
// (newest first).
type Facts struct {
	Planned         []string
	Landed          []LandedChange
	InWork          []string
	AsksOpen        int
	Spent, Appetite int64
	Replans         int
	PerChange       []ChangeSpend
	Needs           []Need
}

// Show returns the bet's current plan, and every revision when history is
// asked for. A bet with no plan yet is refused, saying how to set one.
func Show(ctx context.Context, s work.Submitter, slug string, history bool) (Detail, error) {
	var out Detail
	err := s.Submit(ctx, func(tx *state.Tx) error {
		b, err := lookupBet(tx, slug)
		if err != nil {
			return err
		}
		out, err = detail(tx, b, history)
		return err
	})
	return out, err
}

func detail(tx *state.Tx, b bet, history bool) (Detail, error) {
	revs, bodies, err := revisions(tx, b.id)
	if err != nil {
		return Detail{}, err
	}
	if len(revs) == 0 {
		return Detail{}, refuse(ErrNoPlan, "the bet %q has no plan yet; set one with physa bet plan set %s", b.slug, b.slug)
	}
	last := revs[len(revs)-1]
	d := Detail{Bet: b.slug, N: last.N, Replans: last.N - 1, Reason: last.Reason, Person: last.Person, At: last.At}
	if err := json.Unmarshal([]byte(bodies[len(bodies)-1]), &d.Changes); err != nil {
		return Detail{}, err
	}
	if history {
		d.History = revs
	}
	return d, nil
}

// revisions reads every revision of the bet's plan, oldest first, with each
// one's stored changes.
func revisions(tx *state.Tx, betID int64) ([]Revision, []string, error) {
	rows, err := tx.Query(`SELECT r.n, r.reason, r.person, r.created_at, r.changes FROM plan_revisions r
		JOIN plans p ON p.id = r.plan_id WHERE p.bet_id = ? ORDER BY r.n`, betID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var revs []Revision
	var bodies []string
	for rows.Next() {
		var r Revision
		var at, body string
		if err := rows.Scan(&r.N, &r.Reason, &r.Person, &at, &body); err != nil {
			return nil, nil, err
		}
		if r.At, err = parse(at); err != nil {
			return nil, nil, err
		}
		var changes []plan.Change
		if err := json.Unmarshal([]byte(body), &changes); err != nil {
			return nil, nil, err
		}
		r.Changes = plan.Names(plan.Plan{Changes: changes})
		revs = append(revs, r)
		bodies = append(bodies, body)
	}
	return revs, bodies, rows.Err()
}

// Current returns the bet and planned change the session works on, read
// from db, a store opened read-only (store.OpenReadOnly). It never writes.
// No such session, or a store from before plans had changes, gives empty
// names and no error.
func Current(db *sql.DB, session string) (bet, change string, err error) {
	if !sessionPat.MatchString(session) {
		return "", "", nil
	}
	err = db.QueryRow(`SELECT b.slug, c.name FROM session_changes s JOIN changes c ON c.id = s.change_id
		JOIN bets b ON b.id = c.bet_id WHERE s.session = ? AND c.planned = 1`, session).Scan(&bet, &change)
	if errors.Is(err, sql.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "no such table")) {
		return "", "", nil
	}
	return bet, change, err
}

// Progress returns the bet's progress as facts. A bet with no plan has no
// planned changes and no replans. There are no asks yet, so none are open.
func Progress(ctx context.Context, s work.Submitter, slug string) (Facts, error) {
	var f Facts
	err := s.Submit(ctx, func(tx *state.Tx) error {
		b, err := lookupBet(tx, slug)
		if err != nil {
			return err
		}
		f = Facts{Planned: []string{}, Landed: []LandedChange{}, InWork: []string{}, PerChange: []ChangeSpend{}, Needs: []Need{},
			Spent: b.spent, Appetite: b.appetite}
		if err := tx.QueryRow(`SELECT coalesce(max(r.n) - 1, 0) FROM plan_revisions r JOIN plans p ON p.id = r.plan_id WHERE p.bet_id = ?`,
			b.id).Scan(&f.Replans); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT c.name, c.state, coalesce(c.landed_commit, ''), coalesce(c.landed_at, ''),
			EXISTS (SELECT 1 FROM session_changes s WHERE s.change_id = c.id)
			FROM changes c WHERE c.bet_id = ? AND c.planned = 1 ORDER BY c.id`, b.id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name, st, commit, at string
			var inWork bool
			if err := rows.Scan(&name, &st, &commit, &at, &inWork); err != nil {
				return err
			}
			f.Planned = append(f.Planned, name)
			if st == "landed" {
				l := LandedChange{Change: name, Commit: commit}
				if l.At, err = parse(at); err != nil {
					return err
				}
				f.Landed = append(f.Landed, l)
			} else if inWork {
				f.InWork = append(f.InWork, name)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if f.PerChange, err = perChange(tx, b.id); err != nil {
			return err
		}
		f.Needs, err = needs(tx, b.id)
		return err
	})
	return f, err
}

func perChange(tx *state.Tx, betID int64) ([]ChangeSpend, error) {
	rows, err := tx.Query(`SELECT change, sum(tokens) FROM spends WHERE bet_id = ? GROUP BY change
		HAVING sum(tokens) > 0 ORDER BY sum(tokens) DESC, change`, betID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChangeSpend{}
	for rows.Next() {
		var c ChangeSpend
		if err := rows.Scan(&c.Change, &c.Tokens); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func needs(tx *state.Tx, betID int64) ([]Need, error) {
	rows, err := tx.Query(`SELECT kind, text, created_at FROM needs_you WHERE bet_id = ? AND done_at IS NULL ORDER BY id DESC`, betID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Need{}
	for rows.Next() {
		var n Need
		var at string
		if err := rows.Scan(&n.Kind, &n.Text, &at); err != nil {
			return nil, err
		}
		if n.At, err = parse(at); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
