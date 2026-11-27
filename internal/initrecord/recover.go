package initrecord

import (
	"context"
	"database/sql"
	"time"
)

// PutRecoverSeed stores the seed of recovery's order, chosen once for a
// gather, and the time it was chosen. Every uint64 round-trips: the seed is
// kept as its int64 bit pattern.
func (s *Store) PutRecoverSeed(ctx context.Context, seed uint64, at time.Time) error {
	_, err := s.DB().ExecContext(ctx, `INSERT OR REPLACE INTO init_recover (id, seed, at) VALUES (1, ?, ?)`, int64(seed), ts(at))
	return err
}

// RecoverSeed returns the stored seed of recovery's order, false when none is
// stored for the current gather.
func (s *Store) RecoverSeed(ctx context.Context) (seed uint64, ok bool, err error) {
	var v int64
	var at string
	err = s.DB().QueryRowContext(ctx, `SELECT seed, at FROM init_recover WHERE id = 1`).Scan(&v, &at)
	if err != nil {
		if err == sql.ErrNoRows {
			err = nil
		}
		return 0, false, err
	}
	return uint64(v), true, nil
}
