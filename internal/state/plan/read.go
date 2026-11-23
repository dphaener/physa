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

// Detail is a pursuit's current plan: its revision number, who wrote it, when
// and why, its changes, and how many times it was replanned. History holds
// every revision, oldest first, when asked for.
type Detail struct {
	Pursuit        string
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

// ChangeSpend is the tokens spent on one change; an empty Change is the pursuit itself.
type ChangeSpend struct {
	Change string
	Tokens int64
}

// Need is an open Needs you item about a pursuit.
type Need struct {
	Kind, Text string
	At         time.Time
}

// Facts is a pursuit's progress, as facts and never one number: the changes
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

// Show returns the pursuit's current plan, and every revision when history is
// asked for. A pursuit with no plan yet is refused, saying how to set one.
func Show(ctx context.Context, s work.Submitter, slug string, history bool) (Detail, error) {
	var out Detail
	err := s.Submit(ctx, func(tx *state.Tx) error {
		pu, err := lookupPursuit(tx, slug)
		if err != nil {
			return err
		}
		out, err = detail(tx, pu, history)
		return err
	})
	return out, err
}

func detail(tx *state.Tx, pu pursuit, history bool) (Detail, error) {
	revs, bodies, err := revisions(tx, pu.id)
	if err != nil {
		return Detail{}, err
	}
	if len(revs) == 0 {
		return Detail{}, refuse(ErrNoPlan, "the pursuit %q has no plan yet; set one with physa pursuit plan set %s", pu.slug, pu.slug)
	}
	last := revs[len(revs)-1]
	d := Detail{Pursuit: pu.slug, N: last.N, Replans: last.N - 1, Reason: last.Reason, Person: last.Person, At: last.At}
	if err := json.Unmarshal([]byte(bodies[len(bodies)-1]), &d.Changes); err != nil {
		return Detail{}, err
	}
	if history {
		d.History = revs
	}
	return d, nil
}

// revisions reads every revision of the pursuit's plan, oldest first, with each
// one's stored changes.
func revisions(tx *state.Tx, pursuitID int64) ([]Revision, []string, error) {
	rows, err := tx.Query(`SELECT r.n, r.reason, r.person, r.created_at, r.changes FROM plan_revisions r
		JOIN plans p ON p.id = r.plan_id WHERE p.pursuit_id = ? ORDER BY r.n`, pursuitID)
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

// Current returns the pursuit and planned change the session works on, read
// from db, a store opened read-only (store.OpenReadOnly). It never writes.
// No such session, or a store from before plans had changes, gives empty
// names and no error.
func Current(db *sql.DB, session string) (slug, change string, err error) {
	if !sessionPat.MatchString(session) {
		return "", "", nil
	}
	err = db.QueryRow(`SELECT pu.slug, c.name FROM session_changes s JOIN changes c ON c.id = s.change_id
		JOIN pursuits pu ON pu.id = c.pursuit_id WHERE s.session = ? AND c.planned = 1`, session).Scan(&slug, &change)
	if errors.Is(err, sql.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "no such table")) {
		return "", "", nil
	}
	return slug, change, err
}

// Progress returns the pursuit's progress as facts. A pursuit with no plan has no
// planned changes and no replans. There are no asks yet, so none are open.
func Progress(ctx context.Context, s work.Submitter, slug string) (Facts, error) {
	var f Facts
	err := s.Submit(ctx, func(tx *state.Tx) error {
		pu, err := lookupPursuit(tx, slug)
		if err != nil {
			return err
		}
		f = Facts{Planned: []string{}, Landed: []LandedChange{}, InWork: []string{}, PerChange: []ChangeSpend{}, Needs: []Need{},
			Spent: pu.spent, Appetite: pu.appetite}
		if err := tx.QueryRow(`SELECT coalesce(max(r.n) - 1, 0) FROM plan_revisions r JOIN plans p ON p.id = r.plan_id WHERE p.pursuit_id = ?`,
			pu.id).Scan(&f.Replans); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT c.name, c.state, coalesce(c.landed_commit, ''), coalesce(c.landed_at, ''),
			EXISTS (SELECT 1 FROM session_changes s WHERE s.change_id = c.id)
			FROM changes c WHERE c.pursuit_id = ? AND c.planned = 1 ORDER BY c.id`, pu.id)
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
		if f.PerChange, err = perChange(tx, pu.id); err != nil {
			return err
		}
		f.Needs, err = needs(tx, pu.id)
		return err
	})
	return f, err
}

func perChange(tx *state.Tx, pursuitID int64) ([]ChangeSpend, error) {
	rows, err := tx.Query(`SELECT change, sum(tokens) FROM spends WHERE pursuit_id = ? GROUP BY change
		HAVING sum(tokens) > 0 ORDER BY sum(tokens) DESC, change`, pursuitID)
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

func needs(tx *state.Tx, pursuitID int64) ([]Need, error) {
	rows, err := tx.Query(`SELECT kind, text, created_at FROM needs_you WHERE pursuit_id = ? AND done_at IS NULL ORDER BY id DESC`, pursuitID)
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
