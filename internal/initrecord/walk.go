package initrecord

import (
	"context"
	"database/sql"
	"errors"
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

// PutWalk replaces the stored walk with w, and clears the placement and
// consolidation made from the old one, in one transaction.
func (s *Store) PutWalk(ctx context.Context, w Walk) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM init_place`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM init_consolidate`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO init_walk (id, areas_at, at, model, input, cached_input, output, body) VALUES (1, ?, ?, ?, ?, ?, ?, ?)`, ts(w.AreasAt), ts(w.At), w.Model, w.Input, w.CachedInput, w.Output, string(w.Body)); err != nil {
		return err
	}
	return tx.Commit()
}

// Walk returns the stored walk, false when there is none.
func (s *Store) Walk(ctx context.Context) (Walk, bool, error) {
	var w Walk
	var areasAt, at string
	err := s.DB().QueryRowContext(ctx, `SELECT areas_at, at, model, input, cached_input, output, body FROM init_walk WHERE id = 1`).Scan(&areasAt, &at, &w.Model, &w.Input, &w.CachedInput, &w.Output, &w.Body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		return Walk{}, false, err
	}
	if w.AreasAt, err = time.Parse(time.RFC3339, areasAt); err == nil {
		w.At, err = time.Parse(time.RFC3339, at)
	}
	return w, err == nil, err
}
