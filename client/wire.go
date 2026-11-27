package client

import (
	"os"
	"path/filepath"

	"github.com/dphaener/physa/internal/configdir"
)

const (
	Subprotocol = "physa.v1" // the WebSocket subprotocol both sides require
	Path        = "/api"     // the only HTTP path the engine answers, with GET
	SocketName  = "engine.sock"
)

// The codes an error reply carries.
const (
	CodeBadRequest    = "bad_request"
	CodeUnknownMethod = "unknown_method"
	CodeInternal      = "internal"
)

// Error is the error a call replies with: a code from the list above and
// a message for a person.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Socket returns the path of the machine's engine socket: SocketName in
// Physa's settings directory, found from the process's environment. It
// does not check that an engine listens.
func Socket() (string, error) {
	dir, err := configdir.Dir(os.Getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SocketName), nil
}
