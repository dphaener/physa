package evidence

import (
	"cmp"
	"context"
	"database/sql"
	"strings"

	observe "github.com/dphaener/physa/internal/evidence"
	"github.com/dphaener/physa/internal/state"
	"github.com/dphaener/physa/internal/state/work"
)

// Filter narrows List to one capability or one bet, or both; empty is any.
// Limit is clamped to 1..500, and 0 is 50.
type Filter struct {
	Capability, Bet string
	Limit           int
}

// List returns the stored facts that match f, newest first, as a command
// that writes nothing, the way work's reads run.
func List(ctx context.Context, s work.Submitter, f Filter) ([]observe.Fact, error) {
	where, args := []string{"1 = 1"}, []any{}
	if f.Capability != "" {
		where, args = append(where, "capability = ?"), append(args, f.Capability)
	}
	if f.Bet != "" {
		where, args = append(where, "bet = ?"), append(args, f.Bet)
	}
	args = append(args, min(max(cmp.Or(f.Limit, 50), 1), 500))
	out := []observe.Fact{}
	err := s.Submit(ctx, func(tx *state.Tx) error {
		out = out[:0]
		rows, err := tx.Query(`SELECT kind, commit_id, pr, capability, bet, change, idea, at, source, seconds, by_commit
			FROM evidence WHERE `+strings.Join(where, " AND ")+` ORDER BY at DESC, id DESC LIMIT ?`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var fact observe.Fact
			var at string
			var secs sql.NullInt64
			if err := rows.Scan(&fact.Kind, &fact.Commit, &fact.PR, &fact.Capability, &fact.Bet, &fact.Change, &fact.Idea,
				&at, &fact.Source, &secs, &fact.ByCommit); err != nil {
				return err
			}
			if fact.At, err = parse(at); err != nil {
				return err
			}
			if secs.Valid {
				fact.Seconds = &secs.Int64
			}
			out = append(out, fact)
		}
		return rows.Err()
	})
	return out, err
}
