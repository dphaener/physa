// Package git is the git adapter of the version-control interface: it reads
// the history of a git repository for Physa through the git command. A Repo
// only reads. Writes go only through a Writer, for learning's branch: it
// fetches from origin, makes isolated working copies under Physa's store,
// moves a branch by compare-and-swap and pushes with a lease. It never
// changes the person's working copy, index or checked-out branch.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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

// logFormat is one record per commit, fields split by NUL (the author's name before the message, which may hold anything), records by RS.
const logFormat = "--format=%H%x00%h%x00%s%x00%cI%x00%an%x00%B%x1e"

// Changes returns the non-merge commits of the range rng, newest first as git
// orders them. Git's own message is the error when the range is not valid.
func (r *repo) Changes(rng string) ([]vcs.Change, error) {
	out, stderr, err := r.git("log", "--no-merges", logFormat, "--end-of-options", rng, "--")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	return parseLog(out)
}

// Since returns the non-merge commits reachable from ref committed after t,
// newest first. A zero t means all of them.
func (r *repo) Since(ref string, t time.Time) ([]vcs.Change, error) {
	args := []string{"log", "--no-merges"}
	if !t.IsZero() {
		args = append(args, "--since="+t.UTC().Format(time.RFC3339))
	}
	out, stderr, err := r.git(append(args, logFormat, "--end-of-options", ref, "--")...)
	if err != nil {
		return nil, gitError(err, stderr)
	}
	return parseLog(out)
}

// Altered returns the paths commit id altered, relative to the repo root and
// sorted. A merge gives its paths against the first parent.
func (r *repo) Altered(id string) ([]string, error) {
	out, stderr, err := r.git("diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--root", "--end-of-options", id)
	if err != nil {
		return nil, gitError(err, stderr)
	}
	paths := []string{}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
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
		f := strings.SplitN(rec, "\x00", 6)
		if len(f) != 6 {
			return nil, fmt.Errorf("unexpected git log record %q", rec)
		}
		at, err := time.Parse(time.RFC3339, f[3])
		if err != nil {
			return nil, fmt.Errorf("unexpected git commit time %q", f[3])
		}
		changes = append(changes, vcs.Change{ID: f[0], Short: f[1], Subject: f[2], Time: at, Author: f[4], Message: f[5]})
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

// Resolve returns the full id of the commit ref names.
func (r *repo) Resolve(ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	out, _, err := r.git("rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

// trailerKey is the form of a trailer key: it is matched in git's grep, so
// nothing else may reach it.
var trailerKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// Trailer asks git for the non-merge commits reachable from ref with a line
// starting "key:", newest first, and returns the first whose trailers carry
// key.
func (r *repo) Trailer(ref, key string) (vcs.Change, bool, error) {
	if !trailerKey.MatchString(key) {
		return vcs.Change{}, false, fmt.Errorf("%q is not a trailer key", key)
	}
	out, stderr, err := r.git("log", "--no-merges", "-i", "--grep=^"+key+":", logFormat, "--end-of-options", ref, "--")
	if err != nil {
		return vcs.Change{}, false, gitError(err, stderr)
	}
	changes, err := parseLog(out)
	if err != nil {
		return vcs.Change{}, false, err
	}
	for _, c := range changes {
		if hasTrailer(c.Message, key) {
			return c, true, nil
		}
	}
	return vcs.Change{}, false, nil
}

// IsAncestor reports whether commit a is b or reachable from b.
func (r *repo) IsAncestor(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	_, _, err := r.git("merge-base", "--is-ancestor", "--end-of-options", a, b)
	return err == nil
}

// trailerLine is one "Key: value" line of a trailer paragraph.
var trailerLine = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*:\s*\S`)

// hasTrailer reports whether message carries a trailer whose key is key,
// case-insensitively, read as the history package reads trailers: never in
// the subject paragraph; from the last paragraph back, every paragraph made
// only of trailer lines and lines continuing them.
func hasTrailer(message, key string) bool {
	lines := strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n")
	var blocks [][]string
	var cur []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if cur != nil {
				blocks, cur = append(blocks, cur), nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if cur != nil {
		blocks = append(blocks, cur)
	}
	for i := len(blocks) - 1; i > 0 && isTrailerBlock(blocks[i]); i-- {
		for _, line := range blocks[i] {
			if k, _, ok := strings.Cut(line, ":"); ok && line[0] != ' ' && line[0] != '\t' && strings.EqualFold(k, key) {
				return true
			}
		}
	}
	return false
}

// isTrailerBlock reports whether every line of the paragraph is a trailer
// line or continues the one above.
func isTrailerBlock(para []string) bool {
	for i, line := range para {
		if line[0] == ' ' || line[0] == '\t' {
			if i == 0 {
				return false
			}
			continue
		}
		if !trailerLine.MatchString(line) {
			return false
		}
	}
	return true
}

// git runs one read-only git command in the repo's directory.
func (r *repo) git(args ...string) (stdout, stderr string, err error) {
	return runGit(r.dir, "", os.Environ(), args...)
}

// runGit runs one git command in dir with env, stdin as its input, git's
// messages in C.
func runGit(dir, stdin string, env []string, args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(env, "LC_ALL=C")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
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
