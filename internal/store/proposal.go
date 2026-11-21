package store

import (
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

// PutProposal replaces the stored init proposal with p.
func (s *Store) PutProposal(p Proposal) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO init_proposal (id, gather_at, at, session_id, model, appetite, input, cached_input, output, body) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts(p.GatherAt), ts(p.At), p.SessionID, p.Model, p.Appetite, p.Input, p.CachedInput, p.Output, string(p.Body))
	return err
}

// Proposed returns the stored init proposal, false when there is none.
func (s *Store) Proposed() (Proposal, bool, error) {
	var p Proposal
	var gatherAt, at, body string
	err := s.db.QueryRow(`SELECT gather_at, at, session_id, model, appetite, input, cached_input, output, body FROM init_proposal WHERE id = 1`).
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
