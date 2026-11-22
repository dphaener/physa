//go:build !(linux || darwin)

package engine

import (
	"context"
	"errors"
)

// Run refuses: the engine needs flock and Unix sockets as Linux and macOS
// have them.
func Run(ctx context.Context, o Options) error {
	return errors.New("the engine runs on Linux and macOS only")
}
