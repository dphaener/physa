// Package git is the git adapter of the version-control interface: it reads
// the history of a git repository for Physa through the git command. It never
// changes history, the index or the working copy.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/vcs"
)

type repo struct {
	dir string
}

var _ vcs.Repo = (*repo)(nil)

// Open returns the repository that contains dir. It returns vcs.ErrNotRepo
// when dir is not in one, and a wrapped error when git cannot run.
func Open(dir string) (vcs.Repo, error) {
	r := &repo{dir: dir}
	_, stderr, err := r.git("rev-parse", "--git-dir")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && strings.Contains(stderr, "not a git repository") {
			return nil, vcs.ErrNotRepo
		}
		return nil, fmt.Errorf("git cannot read %s: %w", dir, gitError(err, stderr))
	}
	return r, nil
}

// logFormat is one record per commit, fields split by NUL, records by RS.
const logFormat = "--format=%H%x00%h%x00%s%x00%cI%x00%B%x1e"

// Changes returns the non-merge commits of the range rng, newest first as git
// orders them. Git's own message is the error when the range is not valid.
func (r *repo) Changes(rng string) ([]vcs.Change, error) {
	out, stderr, err := r.git("log", "--no-merges", logFormat, "--end-of-options", rng, "--")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	return parseLog(out)
}

// Latest returns the newest n non-merge commits reachable from ref.
func (r *repo) Latest(ref string, n int) ([]vcs.Change, error) {
	out, stderr, err := r.git("log", "--no-merges", "-n", strconv.Itoa(n), logFormat, "--end-of-options", ref, "--")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	return parseLog(out)
}

func parseLog(out string) ([]vcs.Change, error) {
	changes := []vcs.Change{}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimPrefix(rec, "\n")
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.SplitN(rec, "\x00", 5)
		if len(f) != 5 {
			return nil, fmt.Errorf("unexpected git log record %q", rec)
		}
		at, err := time.Parse(time.RFC3339, f[3])
		if err != nil {
			return nil, fmt.Errorf("unexpected git commit time %q", f[3])
		}
		changes = append(changes, vcs.Change{ID: f[0], Short: f[1], Subject: f[2], Time: at, Message: f[4]})
	}
	return changes, nil
}

// revertLine matches git's "This reverts commit <id>." line, which may end in
// a comma when a ", reversing changes made to <id>" clause follows.
var revertLine = regexp.MustCompile(`(?m)^This reverts commit ([0-9a-fA-F]{7,64})(?:[.,]|\s*$)`)

// Reverts returns the id in the last "This reverts commit <id>." line of c's
// message.
func (r *repo) Reverts(c vcs.Change) (string, bool) {
	all := revertLine.FindAllStringSubmatch(c.Message, -1)
	if len(all) == 0 {
		return "", false
	}
	return all[len(all)-1][1], true
}

// DefaultBranch returns origin's HEAD when the repo knows it, else the first
// of origin/main, origin/master, main and master that exists.
func (r *repo) DefaultBranch() (string, error) {
	if out, _, err := r.git("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master", "refs/heads/main", "refs/heads/master"} {
		if r.hasRef(ref) {
			return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/"), nil
		}
	}
	return "", errors.New("the default branch cannot be found: no origin/HEAD, origin/main, origin/master, main or master")
}

// Branch returns name and origin/name, each only when it exists.
func (r *repo) Branch(name string) []string {
	refs := []string{}
	if r.hasRef("refs/heads/" + name) {
		refs = append(refs, name)
	}
	if r.hasRef("refs/remotes/origin/" + name) {
		refs = append(refs, "origin/"+name)
	}
	return refs
}

func (r *repo) hasRef(ref string) bool {
	_, _, err := r.git("rev-parse", "--verify", "--quiet", "--end-of-options", ref)
	return err == nil
}

// HasChange reports whether id, full or abbreviated, names exactly one commit.
func (r *repo) HasChange(id string) bool {
	if id == "" {
		return false
	}
	_, _, err := r.git("rev-parse", "--verify", "--quiet", "--end-of-options", id+"^{commit}")
	return err == nil
}

// Partial reports whether the repo is a shallow clone.
func (r *repo) Partial() bool {
	out, _, err := r.git("rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(out) == "true"
}

// StoreDir returns "physa" inside git's common directory, which every
// worktree of the repo shares.
func (r *repo) StoreDir() (string, error) {
	out, stderr, err := r.git("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", gitError(err, stderr)
	}
	return filepath.Join(strings.TrimSpace(out), "physa"), nil
}

// Files lists the files directly under dir in commit id, not its subdirectories.
func (r *repo) Files(id, dir string) ([]string, error) {
	dir = strings.TrimSuffix(dir, "/")
	out, stderr, err := r.git("ls-tree", "-z", "--format=%(objecttype) %(path)", "--end-of-options", id, "--", dir+"/")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	files := []string{}
	for _, entry := range strings.Split(out, "\x00") {
		if name, ok := strings.CutPrefix(entry, "blob "); ok {
			files = append(files, name)
		}
	}
	return files, nil
}

// git runs one read-only git command in the repo's directory.
func (r *repo) git(args ...string) (stdout, stderr string, err error) {
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
