// Package github reads GitHub through the gh command: whether a PR exists,
// the repo's open and merged PRs, and the check runs on a commit. It never
// writes to GitHub, and it tells a missing or unauthenticated gh apart from
// an answer of "not found".
package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
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

// PR is an open or merged pull request.
type PR struct {
	Number int
	Head   string
	State  string // "open" or "merged"
}

// CheckRun is one check run on a commit.
type CheckRun struct {
	ID          int64
	HeadSHA     string
	Status      string
	Conclusion  string
	CompletedAt string
	Title       string
	Text        string
}

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

// PullRequests lists the repo's open and merged PRs, sorted by number.
func (c *Client) PullRequests() ([]PR, error) {
	out, err := c.gh("api", "--paginate", "repos/{owner}/{repo}/pulls?state=all&per_page=100",
		"--jq", ".[] | {number, head: .head.sha, state, merged_at}")
	if err != nil {
		return nil, err
	}
	var prs []PR
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var raw struct {
			Number   int     `json:"number"`
			Head     string  `json:"head"`
			State    string  `json:"state"`
			MergedAt *string `json:"merged_at"`
		}
		if err := dec.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading PRs from gh: %w", err)
		}
		switch {
		case raw.MergedAt != nil:
			prs = append(prs, PR{raw.Number, raw.Head, "merged"})
		case raw.State == "open":
			prs = append(prs, PR{raw.Number, raw.Head, "open"})
		}
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].Number < prs[j].Number })
	return prs, nil
}

// CheckRuns lists the check runs called name on commit sha.
func (c *Client) CheckRuns(sha, name string) ([]CheckRun, error) {
	out, err := c.gh("api",
		fmt.Sprintf("repos/{owner}/{repo}/commits/%s/check-runs?check_name=%s&per_page=100", sha, name),
		"--jq", ".check_runs[] | {id, head_sha, status, conclusion, completed_at, output: {title: .output.title, text: .output.text}}")
	if err != nil {
		return nil, err
	}
	var runs []CheckRun
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var raw struct {
			ID          int64   `json:"id"`
			HeadSHA     string  `json:"head_sha"`
			Status      string  `json:"status"`
			Conclusion  *string `json:"conclusion"`
			CompletedAt *string `json:"completed_at"`
			Output      struct {
				Title *string `json:"title"`
				Text  *string `json:"text"`
			} `json:"output"`
		}
		if err := dec.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading check runs from gh: %w", err)
		}
		runs = append(runs, CheckRun{
			ID: raw.ID, HeadSHA: raw.HeadSHA, Status: raw.Status,
			Conclusion: str(raw.Conclusion), CompletedAt: str(raw.CompletedAt),
			Title: str(raw.Output.Title), Text: str(raw.Output.Text),
		})
	}
	return runs, nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
