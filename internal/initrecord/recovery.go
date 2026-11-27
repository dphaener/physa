package initrecord

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Recovered is one capability's recovery the store keeps: the capability text
// and glossary terms its session wrote, how it ended (Status is stored as
// given), the session and what it used. ProposalAt is the time of the
// proposal or the walk it was recovered from. Details is JSON.
type Recovered struct {
	Capability  string
	ProposalAt  time.Time
	At          time.Time
	Status      string
	File        string
	Terms       string
	Details     []byte
	SessionID   string
	Model       string
	Attempts    int
	Appetite    int64
	Input       int64
	CachedInput int64
	Output      int64
	// KeepEarlier, when true, keeps an earlier stored row of the capability
	// that holds a recovered file (a non-empty File) and does not store this
	// row: a re-run that ends not yet recovered or failed loses nothing.
	KeepEarlier bool
}

// PutRecovered stores rs in one transaction: a capability already stored is
// replaced, every other stored capability is kept. A row with KeepEarlier
// leaves an earlier row that holds a file as it is.
func (s *Store) PutRecovered(ctx context.Context, rs []Recovered) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range rs {
		details := r.Details
		if details == nil {
			details = []byte("{}")
		}
		if r.KeepEarlier {
			var file string
			err := tx.QueryRowContext(ctx, `SELECT file FROM init_recovery WHERE capability = ?`, r.Capability).Scan(&file)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && file != "" {
				continue
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO init_recovery (capability, proposal_at, at, status, file, terms, details, session_id, model, attempts, appetite, input, cached_input, output) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.Capability, ts(r.ProposalAt), ts(r.At), r.Status, r.File, r.Terms, string(details), r.SessionID, r.Model, r.Attempts, r.Appetite, r.Input, r.CachedInput, r.Output); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Recovered returns the stored recoveries ordered by capability.
func (s *Store) Recovered(ctx context.Context) ([]Recovered, error) {
	rows, err := s.DB().QueryContext(ctx, `SELECT capability, proposal_at, at, status, file, terms, details, session_id, model, attempts, appetite, input, cached_input, output FROM init_recovery ORDER BY capability`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recovered
	for rows.Next() {
		var r Recovered
		var pat, at, details string
		if err := rows.Scan(&r.Capability, &pat, &at, &r.Status, &r.File, &r.Terms, &details, &r.SessionID, &r.Model, &r.Attempts, &r.Appetite, &r.Input, &r.CachedInput, &r.Output); err != nil {
			return nil, err
		}
		if r.ProposalAt, err = time.Parse(time.RFC3339, pat); err != nil {
			return nil, err
		}
		if r.At, err = time.Parse(time.RFC3339, at); err != nil {
			return nil, err
		}
		r.Details = []byte(details)
		out = append(out, r)
	}
	return out, rows.Err()
}
