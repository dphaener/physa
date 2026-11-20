// Package github reads GitHub through the gh command: whether a PR exists. It
// never writes to GitHub, and it tells a missing or unauthenticated gh apart
// from an answer of "not found".
package github

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Sentinel errors, checked with errors.Is.
var (
	// ErrNoGH means gh is not on PATH.
	ErrNoGH = errors.New("gh is not installed")
	// ErrUnauthenticated means gh has no usable login.
	ErrUnauthenticated = errors.New("gh is not authenticated")
	// ErrNotFound means GitHub answered 404.
	ErrNotFound = errors.New("not found on GitHub")
)

// Error is a gh command that exited non-zero.
type Error struct {
	Args     []string
	Stderr   string
	ExitCode int
}

func (e *Error) Error() string {
	return fmt.Sprintf("gh %s: exit %d: %s", strings.Join(e.Args, " "), e.ExitCode, strings.TrimSpace(e.Stderr))
}

// Runner runs gh with args in dir and returns its stdout.
type Runner func(dir string, args ...string) (stdout []byte, err error)

func ghRunner(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out.Bytes(), &Error{Args: args, Stderr: errb.String(), ExitCode: ee.ExitCode()}
		}
		return nil, err
	}
	return out.Bytes(), nil
}

// Client reads GitHub for the repo in its directory.
type Client struct {
	dir string
	run Runner
}

// New returns a Client that runs the real gh in dir.
func New(dir string) *Client { return NewWithRunner(dir, ghRunner) }

// NewWithRunner returns a Client that uses r instead of gh.
func NewWithRunner(dir string, r Runner) *Client { return &Client{dir: dir, run: r} }

func (c *Client) gh(args ...string) ([]byte, error) {
	out, err := c.run(c.dir, args...)
	if err == nil {
		return out, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("%w: %v", ErrNoGH, err)
	}
	var ge *Error
	if errors.As(err, &ge) {
		switch {
		case ge.ExitCode == 4, strings.Contains(ge.Stderr, "gh auth login"), strings.Contains(ge.Stderr, "HTTP 401"):
			return nil, fmt.Errorf("%w: %s", ErrUnauthenticated, strings.TrimSpace(ge.Stderr))
		case strings.Contains(ge.Stderr, "HTTP 404"):
			return nil, fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(ge.Stderr))
		}
	}
	return nil, err
}

// PRExists reports whether the repo has a pull request numbered n.
func (c *Client) PRExists(n int) (bool, error) {
	_, err := c.gh("api", fmt.Sprintf("repos/{owner}/{repo}/pulls/%d", n), "--jq", ".number")
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
