// Package runner runs git when it should not.
package runner

import (
	"context"
	osexec "os/exec"
)

// Status runs git directly.
func Status() error { return osexec.Command("git", "status").Run() }

// Log runs git with a context.
func Log(ctx context.Context) error { return osexec.CommandContext(ctx, "git", "log").Run() }
