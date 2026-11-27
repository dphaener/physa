// Package state is on the engine's side and may make fresh contexts.
package state

import "context"

// Open makes fresh contexts.
func Open() (context.Context, context.Context) { return context.Background(), context.TODO() }
