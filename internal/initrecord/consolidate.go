package initrecord

import (
	"database/sql"
	"time"
)

// Consolidated is the one consolidation of the recovered capabilities the
// store keeps, with the sessions' totals and what they used. WalkAt is the
// time of the walk it was consolidated from. Body is the record as JSON: what
// was merged, moved, dropped, lifted and refused, and the consolidated map.
type Consolidated struct {
	WalkAt, At                 time.Time
	Model                      string
	Input, CachedInput, Output int64
	Body                       []byte
}

// PutConsolidated replaces the stored consolidation with c.
func (s *Store) PutConsolidated(c Consolidated) error {
	_, err := s.DB().Exec(`INSERT OR REPLACE INTO init_consolidate (id, walk_at, at, model, input, cached_input, output, body) VALUES (1, ?, ?, ?, ?, ?, ?, ?)`, ts(c.WalkAt), ts(c.At), c.Model, c.Input, c.CachedInput, c.Output, string(c.Body))
	return err
}

// Consolidated returns the stored consolidation, false when there is none.
func (s *Store) Consolidated() (Consolidated, bool, error) {
	var c Consolidated
	var walkAt, at string
	err := s.DB().QueryRow(`SELECT walk_at, at, model, input, cached_input, output, body FROM init_consolidate WHERE id = 1`).Scan(&walkAt, &at, &c.Model, &c.Input, &c.CachedInput, &c.Output, &c.Body)
	if err != nil {
		if err == sql.ErrNoRows {
			err = nil
		}
		return Consolidated{}, false, err
	}
	if c.WalkAt, err = time.Parse(time.RFC3339, walkAt); err == nil {
		c.At, err = time.Parse(time.RFC3339, at)
	}
	return c, err == nil, err
}
