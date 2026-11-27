// Package github is the GitHub adapter of the forge interface: it reads
// GitHub through the gh command, and opens or refreshes a PR only through
// the forge.PRs methods in pulls.go; it never merges. It tells a missing or
// unauthenticated gh, and a repo with no GitHub remote, apart from an answer
// of "not found".
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

var (
	_ forge.Forge       = (*Client)(nil)
	_ forge.PRs         = (*Client)(nil)
	_ forge.Merged      = (*Client)(nil)
	_ forge.MergedPages = (*Client)(nil)
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

// Runner runs gh with args in dir and returns its stdout, also when gh exits
// non-zero (gh api prints GraphQL errors there).
type Runner func(ctx context.Context, dir string, args ...string) (stdout []byte, err error)

// StdinRunner is a Runner that also passes stdin to gh when it is not nil.
type StdinRunner func(ctx context.Context, dir string, stdin []byte, args ...string) (stdout []byte, err error)

// ghTimeout bounds every gh call, so a hung gh never holds up a caller or an
// engine shutdown.
const ghTimeout = 30 * time.Second

// errGHTimeout is the cause of a gh call that ghTimeout ended.
var errGHTimeout = errors.New("gh timed out")

func ghStdinRunner(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	bounded, cancel := context.WithTimeoutCause(ctx, ghTimeout, errGHTimeout)
	defer cancel()
	cmd := exec.CommandContext(bounded, "gh", args...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if errors.Is(context.Cause(bounded), errGHTimeout) {
			return nil, fmt.Errorf("gh %s: timed out after %s", strings.Join(args, " "), ghTimeout)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("gh %s: %w", strings.Join(args, " "), context.Cause(ctx))
		}
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
	dir         string
	run         StdinRunner
	owner, name string // the repo, once ChangePR has asked gh
}

// New returns a Client that runs the real gh in dir.
func New(dir string) *Client { return NewWithStdinRunner(dir, ghStdinRunner) }

// NewWithRunner returns a Client that uses r instead of gh. r is not given
// the body of a PR, so it suits the reads; see NewWithStdinRunner.
func NewWithRunner(dir string, r Runner) *Client {
	return NewWithStdinRunner(dir, func(ctx context.Context, dir string, _ []byte, args ...string) ([]byte, error) {
		return r(ctx, dir, args...)
	})
}

// NewWithStdinRunner returns a Client that uses r instead of gh, and gives r
// what gh would read on stdin.
func NewWithStdinRunner(dir string, r StdinRunner) *Client { return &Client{dir: dir, run: r} }

// Name is "github".
func (c *Client) Name() string { return "github" }

// gh runs gh and classes its failure as a forge.Error when it is one of the
// forge's sentinels. Its stdout is returned whatever the error.
func (c *Client) gh(ctx context.Context, args ...string) ([]byte, error) {
	return c.ghIn(ctx, nil, args...)
}

// ghIn is gh with stdin for the command.
func (c *Client) ghIn(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	out, err := c.run(ctx, c.dir, stdin, args...)
	if err == nil {
		return out, nil
	}
	return out, classify(err)
}

func classify(err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return &forge.Error{Class: forge.ErrUnavailable, Reason: "gh is not installed", Detail: err.Error()}
	}
	var ge *Error
	if !errors.As(err, &ge) {
		return err
	}
	stderr := strings.TrimSpace(ge.Stderr)
	switch {
	case ge.ExitCode == 4, strings.Contains(stderr, "gh auth login"), strings.Contains(stderr, "HTTP 401"):
		return &forge.Error{Class: forge.ErrUnauthenticated, Reason: "gh is not authenticated", Detail: stderr}
	case strings.Contains(stderr, "none of the git remotes"), strings.Contains(stderr, "no git remotes found"),
		strings.Contains(stderr, "not a git repository"):
		return &forge.Error{Class: forge.ErrUnavailable, Reason: "gh finds no GitHub remote", Detail: stderr}
	case strings.Contains(stderr, "HTTP 404"):
		return &forge.Error{Class: forge.ErrNotFound, Reason: "not found on GitHub", Detail: stderr}
	}
	return err
}

// PRExists reports whether the repo has a pull request numbered n.
func (c *Client) PRExists(ctx context.Context, n int) (bool, error) {
	_, err := c.gh(ctx, "api", fmt.Sprintf("repos/{owner}/{repo}/pulls/%d", n), "--jq", ".number")
	if errors.Is(err, forge.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// repo returns the repo's owner and name, asking gh once per Client.
func (c *Client) repo(ctx context.Context) (owner, name string, err error) {
	if c.owner != "" {
		return c.owner, c.name, nil
	}
	out, err := c.gh(ctx, "repo", "view", "--json", "owner,name")
	if err != nil {
		return "", "", err
	}
	var v struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return "", "", fmt.Errorf("gh repo view: %w", err)
	}
	if v.Owner.Login == "" || v.Name == "" {
		return "", "", errors.New("gh repo view: no owner or name")
	}
	c.owner, c.name = v.Owner.Login, v.Name
	return c.owner, c.name, nil
}

// changePRQuery asks for the PR a commit came from, with its conversation,
// its reviews and their inline comments, and its commits.
const changePRQuery = `query($owner: String!, $name: String!, $oid: GitObjectID!) {
  repository(owner: $owner, name: $name) {
    object(oid: $oid) {
      ... on Commit {
        associatedPullRequests(first: 1) {
          nodes {
            number
            author { login }
            comments(first: 100) { nodes { author { login __typename } createdAt body } }
            reviews(first: 100) {
              nodes {
                author { login __typename }
                state
                submittedAt
                body
                comments(first: 100) { nodes { author { login __typename } createdAt body } }
              }
            }
            commits(first: 100) { nodes { commit { oid committedDate } } }
          }
        }
      }
    }
  }
}`

type author struct {
	Login    string `json:"login"`
	Typename string `json:"__typename"`
}

// bot reports whether a is a bot, or nobody GitHub still knows.
func (a *author) bot() bool {
	return a == nil || a.Typename == "Bot" || strings.HasSuffix(a.Login, "[bot]")
}

type comment struct {
	Author    *author   `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	Body      string    `json:"body"`
}

type changePRResponse struct {
	Data struct {
		Repository *struct {
			Object *struct {
				AssociatedPullRequests *struct {
					Nodes []struct {
						Number   int     `json:"number"`
						Author   *author `json:"author"`
						Comments struct {
							Nodes []comment `json:"nodes"`
						} `json:"comments"`
						Reviews struct {
							Nodes []review `json:"nodes"`
						} `json:"reviews"`
						Commits struct {
							Nodes []struct {
								Commit struct {
									OID           string    `json:"oid"`
									CommittedDate time.Time `json:"committedDate"`
								} `json:"commit"`
							} `json:"nodes"`
						} `json:"commits"`
					} `json:"nodes"`
				} `json:"associatedPullRequests"`
			} `json:"object"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ChangePR returns the PR change came from, with its people's review and
// discussion and its commits, oldest first. Its bool is false when change
// came from no PR or GitHub does not know it. Reviews still pending are left
// out: only their author can see them.
func (c *Client) ChangePR(ctx context.Context, change string) (forge.PR, bool, error) {
	owner, name, err := c.repo(ctx)
	if err != nil {
		return forge.PR{}, false, err
	}
	out, err := c.gh(ctx, "api", "graphql", "-f", "query="+changePRQuery,
		"-f", "owner="+owner, "-f", "name="+name, "-f", "oid="+change)
	var resp changePRResponse
	if jerr := json.Unmarshal(out, &resp); jerr == nil && len(resp.Errors) > 0 {
		return forge.PR{}, false, fmt.Errorf("GitHub GraphQL: %s", resp.Errors[0].Message)
	} else if err != nil {
		return forge.PR{}, false, err
	} else if jerr != nil {
		return forge.PR{}, false, fmt.Errorf("GitHub GraphQL: %w", jerr)
	}
	repo := resp.Data.Repository
	if repo == nil || repo.Object == nil || repo.Object.AssociatedPullRequests == nil ||
		len(repo.Object.AssociatedPullRequests.Nodes) == 0 {
		return forge.PR{}, false, nil
	}
	n := repo.Object.AssociatedPullRequests.Nodes[0]
	pr := forge.PR{Number: n.Number, Comments: []forge.Comment{}, Commits: []forge.Commit{}}
	if n.Author != nil {
		pr.Author = n.Author.Login
	}
	pr.Comments = comments(n.Comments.Nodes, n.Reviews.Nodes)
	for _, cm := range n.Commits.Nodes {
		pr.Commits = append(pr.Commits, forge.Commit{ID: cm.Commit.OID, At: cm.Commit.CommittedDate})
	}
	sort.SliceStable(pr.Commits, func(i, j int) bool { return pr.Commits[i].At.Before(pr.Commits[j].At) })
	return pr, true, nil
}

// review is a review node: its state, body and inline comments.
type review struct {
	Author      *author    `json:"author"`
	State       string     `json:"state"`
	SubmittedAt *time.Time `json:"submittedAt"`
	Body        string     `json:"body"`
	Comments    struct {
		Nodes []comment `json:"nodes"`
	} `json:"comments"`
}

// comments converts a PR's conversation and reviews to forge comments, oldest
// first. It drops bots' comments and reviews still pending, and marks a
// CHANGES_REQUESTED review and its inline comments.
func comments(discussion []comment, reviews []review) []forge.Comment {
	out := []forge.Comment{}
	add := func(a *author, at time.Time, kind, body string, requests bool) {
		if !a.bot() {
			out = append(out, forge.Comment{Author: a.Login, At: at, Kind: kind, Body: body, RequestsChanges: requests})
		}
	}
	for _, cm := range discussion {
		add(cm.Author, cm.CreatedAt, forge.KindDiscussion, cm.Body, false)
	}
	for _, r := range reviews {
		if r.SubmittedAt == nil {
			continue
		}
		requests := r.State == "CHANGES_REQUESTED"
		if strings.TrimSpace(r.Body) != "" {
			add(r.Author, *r.SubmittedAt, forge.KindReview, r.Body, requests)
		}
		for _, cm := range r.Comments.Nodes {
			add(cm.Author, cm.CreatedAt, forge.KindReview, cm.Body, requests)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}
