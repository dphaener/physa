package initrecord

import (
	"context"
	"database/sql"
	"time"
)

// Proposal is the one init proposal the store keeps: the capabilities and
// promise proposed from one gather, with the session that made it and what it
// used. GatherAt is the run time of the gather it belongs to. Body is the
// proposal as JSON.
type Proposal struct {
	GatherAt, At time.Time
	SessionID    string
	Model        string
	Appetite     int64
	Input        int64
	CachedInput  int64
	Output       int64
	Body         []byte
}

// PutProposal replaces the stored init proposal with p. It also deletes the
// stored recoveries and what proposing and recovering spent, which belong to
// one proposal.
func (s *Store) PutProposal(ctx context.Context, p Proposal) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM init_recovery`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM init_spend WHERE phase IN ('propose', 'recover')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO init_proposal (id, gather_at, at, session_id, model, appetite, input, cached_input, output, body) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts(p.GatherAt), ts(p.At), p.SessionID, p.Model, p.Appetite, p.Input, p.CachedInput, p.Output, string(p.Body)); err != nil {
		return err
	}
	return tx.Commit()
}

// Proposed returns the stored init proposal, false when there is none.
func (s *Store) Proposed(ctx context.Context) (Proposal, bool, error) {
	var p Proposal
	var gatherAt, at, body string
	err := s.DB().QueryRowContext(ctx, `SELECT gather_at, at, session_id, model, appetite, input, cached_input, output, body FROM init_proposal WHERE id = 1`).
		Scan(&gatherAt, &at, &p.SessionID, &p.Model, &p.Appetite, &p.Input, &p.CachedInput, &p.Output, &body)
	if err == sql.ErrNoRows {
		return Proposal{}, false, nil
	}
	if err != nil {
		return Proposal{}, false, err
	}
	if p.GatherAt, err = time.Parse(time.RFC3339, gatherAt); err != nil {
		return Proposal{}, false, err
	}
	if p.At, err = time.Parse(time.RFC3339, at); err != nil {
		return Proposal{}, false, err
	}
	p.Body = []byte(body)
	return p, true, nil
}
