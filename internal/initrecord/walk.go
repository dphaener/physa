package initrecord

import (
	"database/sql"
	"time"
)

// Walk is the one walk of the areas the store keeps, with the session
// totals that made it and what they used. AreasAt is the time of the areas
// it was walked from. Body is the footprints as JSON: file paths stay here,
// in init's record, and never reach the map.
type Walk struct {
	AreasAt, At                time.Time
	Model                      string
	Input, CachedInput, Output int64
	Body                       []byte
}

// PutWalk replaces the stored walk with w.
func (s *Store) PutWalk(w Walk) error {
	_, err := s.DB().Exec(`INSERT OR REPLACE INTO init_walk (id, areas_at, at, model, input, cached_input, output, body) VALUES (1, ?, ?, ?, ?, ?, ?, ?)`, ts(w.AreasAt), ts(w.At), w.Model, w.Input, w.CachedInput, w.Output, string(w.Body))
	return err
}

// Walk returns the stored walk, false when there is none.
func (s *Store) Walk() (Walk, bool, error) {
	var w Walk
	var areasAt, at string
	err := s.DB().QueryRow(`SELECT areas_at, at, model, input, cached_input, output, body FROM init_walk WHERE id = 1`).Scan(&areasAt, &at, &w.Model, &w.Input, &w.CachedInput, &w.Output, &w.Body)
	if err != nil {
		if err == sql.ErrNoRows {
			err = nil
		}
		return Walk{}, false, err
	}
	if w.AreasAt, err = time.Parse(time.RFC3339, areasAt); err == nil {
		w.At, err = time.Parse(time.RFC3339, at)
	}
	return w, err == nil, err
}
