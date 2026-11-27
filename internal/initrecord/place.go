package initrecord

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Place is the one placement of the gathered PRs and changes the store
// keeps. WalkAt is the time of the walk it was placed from. Body is the
// placements as JSON: file paths stay here, in init's record.
type Place struct {
	WalkAt, At time.Time
	Body       []byte
}

// PutPlace replaces the stored placement with p.
func (s *Store) PutPlace(ctx context.Context, p Place) error {
	_, err := s.DB().ExecContext(ctx, `INSERT OR REPLACE INTO init_place (id, walk_at, at, body) VALUES (1, ?, ?, ?)`, ts(p.WalkAt), ts(p.At), string(p.Body))
	return err
}

// Place returns the stored placement, false when there is none.
func (s *Store) Place(ctx context.Context) (Place, bool, error) {
	var p Place
	var walkAt, at string
	err := s.DB().QueryRowContext(ctx, `SELECT walk_at, at, body FROM init_place WHERE id = 1`).Scan(&walkAt, &at, &p.Body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		return Place{}, false, err
	}
	if p.WalkAt, err = time.Parse(time.RFC3339, walkAt); err == nil {
		p.At, err = time.Parse(time.RFC3339, at)
	}
	return p, err == nil, err
}
