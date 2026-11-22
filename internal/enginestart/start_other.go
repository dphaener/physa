//go:build !(linux || darwin)

package enginestart

import (
	"context"
	"errors"
)

// errUnix is the refusal on platforms the engine does not run on.
var errUnix = errors.New("the engine runs on Linux and macOS only")

// Start refuses: the engine runs on Linux and macOS only.
func Start(ctx context.Context, o Options) (started bool, err error) {
	return false, errUnix
}

// Stop refuses: the engine runs on Linux and macOS only.
func Stop(ctx context.Context, dir string) (stopped bool, err error) {
	return false, errUnix
}
