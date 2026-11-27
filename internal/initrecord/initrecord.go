// Package initrecord keeps init's record in .git/physa: the areas a repo was
// divided into, the one init proposal, the capabilities' recoveries and what
// each init phase spent. It sits beside the gather the store keeps, in the
// same database, and a new gather clears it. None of it reaches the map. The
// tables' schema is the store's: every migration lives in the store's one
// numbered order.
package initrecord

import (
	"time"

	"github.com/dphaener/physa/internal/store"
)

// Store is an open store that also keeps init's record. Everything the
// store keeps, such as the gather, is reached through it as well.
type Store struct {
	*store.Store
}

// Of gives st's init record.
func Of(st *store.Store) *Store { return &Store{st} }

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }
