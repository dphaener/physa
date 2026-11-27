// Package runner makes fresh contexts when it should not.
package runner

import (
	"context"
	stdctx "context"
)

// Start makes a fresh context.
func Start() context.Context { return context.Background() }

// Later makes a fresh context through an alias.
func Later() context.Context { return stdctx.TODO() }

// Bounded derives from the caller's context, which is allowed.
func Bounded(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }
