// Package history reads the repo's git history: the commits of a range with
// their Capability and Loaded trailers, whether a commit exists, and whether
// each commit names capabilities the map has, a change's diff and commit
// messages, and the default branch. It runs git; it never changes history.
package history

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNotRepo is returned by Open when the directory is not in a git repository.
var ErrNotRepo = errors.New("not a git repository")

// Repo is a git repository read through the git command.
type Repo struct {
	dir string
}

// Commit is one commit of a range with the capabilities its trailers name.
// Capabilities holds the slugs of its Capability trailers and Loaded those of
// its Loaded trailers, each in the order written.
type Commit struct {
	Hash         string   `json:"hash"`
	Short        string   `json:"short"`
	Subject      string   `json:"subject"`
	Capabilities []string `json:"capabilities"`
	Loaded       []string `json:"loaded"`
}

// Open returns the repository that contains dir. It returns ErrNotRepo when
// dir is not in one, and a wrapped error when git cannot run.
func Open(dir string) (*Repo, error) {
	r := &Repo{dir: dir}
	_, stderr, err := r.git("rev-parse", "--git-dir")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && strings.Contains(stderr, "not a git repository") {
			return nil, ErrNotRepo
		}
		return nil, fmt.Errorf("git cannot read %s: %w", dir, gitError(err, stderr))
	}
	return r, nil
}

// Log returns the non-merge commits of the range rng, newest first as git
// orders them. Git's own message is the error when the range is not valid.
func (r *Repo) Log(rng string) ([]Commit, error) {
	const format = "--format=%H%x00%h%x00%s%x00%B%x1e"
	out, stderr, err := r.git("log", "--no-merges", format, "--end-of-options", rng, "--")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	commits := []Commit{}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimPrefix(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.SplitN(rec, "\x00", 4)
		if len(f) != 4 {
			return nil, fmt.Errorf("unexpected git log record %q", rec)
		}
		capabilities, loaded := readTrailers(f[3])
		commits = append(commits, Commit{
			Hash: f[0], Short: f[1], Subject: f[2],
			Capabilities: capabilities, Loaded: loaded,
		})
	}
	return commits, nil
}

// Diff returns the diff of the change rng as a PR shows it, against the merge
// base. A..B and A...B both mean git diff A...B; a single rev A means A...HEAD.
// Git's own message is the error when the range is not valid.
func (r *Repo) Diff(rng string) (string, error) {
	var spec string
	if a, b, ok := strings.Cut(rng, "..."); ok {
		spec = a + "..." + orHead(b)
	} else if a, b, ok := strings.Cut(rng, ".."); ok {
		spec = orHead(a) + "..." + orHead(b)
	} else {
		spec = rng + "...HEAD"
	}
	out, stderr, err := r.git("diff", "--no-color", "--no-ext-diff", "--end-of-options", spec, "--")
	if err != nil {
		return "", gitError(err, stderr)
	}
	return out, nil
}

func orHead(rev string) string {
	if rev == "" {
		return "HEAD"
	}
	return rev
}

// Message returns the subject and the body of the commit rev, the body
// trimmed. Git's own message is the error when rev is not a commit.
func (r *Repo) Message(rev string) (subject, body string, err error) {
	out, stderr, err := r.git("show", "--no-patch", "--format=%s%x00%b", "--end-of-options", rev, "--")
	if err != nil {
		return "", "", gitError(err, stderr)
	}
	subject, body, _ = strings.Cut(out, "\x00")
	return strings.TrimSpace(subject), strings.TrimSpace(body), nil
}

// DefaultBranch returns the repo's default branch: the one origin/HEAD points
// at (such as origin/main), else a local main, else a local master. It never
// fetches.
func (r *Repo) DefaultBranch() (string, error) {
	if out, _, err := r.git("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name := strings.TrimSpace(out); name != "" {
			return name, nil
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, _, err := r.git("show-ref", "--verify", "--quiet", "refs/heads/"+name); err == nil {
			return name, nil
		}
	}
	return "", errors.New("no default branch was found: origin/HEAD, main and master are all missing")
}

// HasCommit reports whether hash, full or abbreviated, names exactly one
// commit in the repo. A missing or ambiguous prefix is false.
func (r *Repo) HasCommit(hash string) bool {
	if hash == "" {
		return false
	}
	_, _, err := r.git("rev-parse", "--verify", "--quiet", "--end-of-options", hash+"^{commit}")
	return err == nil
}

// Shallow reports whether the repo is a shallow clone.
func (r *Repo) Shallow() bool {
	out, _, err := r.git("rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(out) == "true"
}

// CommonDir returns the absolute path of git's common directory, which every
// worktree of the repo shares.
func (r *Repo) CommonDir() (string, error) {
	out, stderr, err := r.git("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", gitError(err, stderr)
	}
	return strings.TrimSpace(out), nil
}

// git runs one read-only git command in the repo's directory.
func (r *Repo) git(args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	err = cmd.Run()
	return out.String(), strings.TrimSpace(errBuf.String()), err
}

// gitError makes git's stderr the error text, or the run error when git
// printed nothing.
func gitError(err error, stderr string) error {
	if stderr != "" {
		return errors.New(stderr)
	}
	return err
}
