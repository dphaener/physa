package store

import (
	"database/sql"
	"time"
)

// Areas is the one division of a repo into areas the store keeps, with the
// session that made it and what it used. GatherAt is the run time of the
// gather it belongs to. Body is the areas and the source split as JSON.
type Areas struct {
	GatherAt, At               time.Time
	Model                      string
	Input, CachedInput, Output int64
	Body                       []byte
}

// PutAreas replaces the stored areas with a.
func (s *Store) PutAreas(a Areas) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO init_areas (id, gather_at, at, model, input, cached_input, output, body) VALUES (1, ?, ?, ?, ?, ?, ?, ?)`, ts(a.GatherAt), ts(a.At), a.Model, a.Input, a.CachedInput, a.Output, string(a.Body))
	return err
}

// Areas returns the stored areas, false when there are none.
func (s *Store) Areas() (Areas, bool, error) {
	var a Areas
	var gatherAt, at string
	err := s.db.QueryRow(`SELECT gather_at, at, model, input, cached_input, output, body FROM init_areas WHERE id = 1`).Scan(&gatherAt, &at, &a.Model, &a.Input, &a.CachedInput, &a.Output, &a.Body)
	if err != nil {
		if err == sql.ErrNoRows {
			err = nil
		}
		return Areas{}, false, err
	}
	if a.GatherAt, err = time.Parse(time.RFC3339, gatherAt); err == nil {
		a.At, err = time.Parse(time.RFC3339, at)
	}
	return a, err == nil, err
}
