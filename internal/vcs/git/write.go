package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dphaener/physa/internal/vcs"
)

// remote is the one remote a Writer fetches from and pushes to.
const remote = "origin"

type writer struct {
	*repo
}

var _ vcs.Writer = (*writer)(nil)

// OpenWriter returns the repository that contains dir, able to write
// learning's branch. Its errors are Open's.
func OpenWriter(ctx context.Context, dir string) (vcs.Writer, error) {
	r, err := Open(ctx, dir)
	if err != nil {
		return nil, err
	}
	return &writer{repo: r.(*repo)}, nil
}

// placeVars are the variables that point git at a repository, index or
// working tree, as a hook sets them. A Writer drops them so that no command
// can reach the person's own index or working tree through them.
var placeVars = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_PREFIX", "GIT_OBJECT_DIRECTORY", "GIT_NAMESPACE"}

// writeEnv is the environment of a writing command: the process's own,
// without placeVars.
func writeEnv() []string {
	env := []string{}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, v := range placeVars {
			drop = drop || name == v
		}
		if !drop {
			env = append(env, kv)
		}
	}
	return env
}

// write runs one writing git command in dir.
func write(ctx context.Context, dir, stdin string, args ...string) (stdout, stderr string, err error) {
	return runGit(ctx, dir, stdin, writeEnv(), args...)
}

// branchRef returns refs/heads/name, or an error when git would not accept
// name as a branch.
func (w *writer) branchRef(ctx context.Context, name string) (string, error) {
	ref := "refs/heads/" + name
	if name == "" || strings.HasPrefix(name, "-") {
		return "", fmt.Errorf("%q is not a branch name", name)
	}
	if _, _, err := w.git(ctx, "check-ref-format", ref); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%q cannot be checked: %w", name, ctx.Err())
		}
		return "", fmt.Errorf("%q is not a branch name", name)
	}
	return ref, nil
}

// Fetch asks origin which of branches it has, fetches those into
// refs/remotes/origin/<branch>, and deletes that ref for the others.
func (w *writer) Fetch(ctx context.Context, branches ...string) error {
	if len(branches) == 0 {
		return nil
	}
	refs := make([]string, len(branches))
	for i, b := range branches {
		ref, err := w.branchRef(ctx, b)
		if err != nil {
			return err
		}
		refs[i] = ref
	}
	out, stderr, err := write(ctx, w.dir, "", append([]string{"ls-remote", "--refs", remote}, refs...)...)
	if err != nil {
		return fmt.Errorf("%s cannot be read: %w", remote, gitError(err, stderr))
	}
	has := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if _, ref, ok := strings.Cut(line, "\t"); ok {
			has[strings.TrimSpace(ref)] = true
		}
	}
	specs := []string{}
	for i, b := range branches {
		tracking := "refs/remotes/" + remote + "/" + b
		if has[refs[i]] {
			specs = append(specs, "+"+refs[i]+":"+tracking)
			continue
		}
		if w.hasRef(ctx, tracking) {
			if _, stderr, err := write(ctx, w.dir, "", "update-ref", "-d", "--end-of-options", tracking); err != nil {
				return gitError(err, stderr)
			}
		}
	}
	if len(specs) == 0 {
		return nil
	}
	args := append([]string{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", remote}, specs...)
	if _, stderr, err := write(ctx, w.dir, "", args...); err != nil {
		return fmt.Errorf("%s cannot be fetched: %w", remote, gitError(err, stderr))
	}
	return nil
}

// CheckedOut reads git's list of worktrees, the main one included.
func (w *writer) CheckedOut(ctx context.Context, branch string) (bool, error) {
	ref, err := w.branchRef(ctx, branch)
	if err != nil {
		return false, err
	}
	out, stderr, err := w.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return false, gitError(err, stderr)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "branch "+ref {
			return true, nil
		}
	}
	return false, nil
}

// NewCopy makes a temp dir under the store and adds a detached git worktree
// at base inside it.
func (w *writer) NewCopy(ctx context.Context, base string) (vcs.Copy, error) {
	id, ok := w.Resolve(ctx, base)
	if !ok {
		return nil, noChange(ctx, base)
	}
	store, err := w.StoreDir(ctx)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(store, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(store, "copy-")
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(tmp, "work")
	if _, stderr, err := write(ctx, w.dir, "", "worktree", "add", "--quiet", "--detach", "--end-of-options", dir, id); err != nil {
		if rmErr := os.Remove(tmp); rmErr != nil {
			return nil, fmt.Errorf("the working copy cannot be made: %w; %s is left behind", gitError(err, stderr), tmp)
		}
		return nil, fmt.Errorf("the working copy cannot be made: %w", gitError(err, stderr))
	}
	return &workCopy{w: w, tmp: tmp, dir: dir}, nil
}

// SetBranch runs update-ref with the old value, and tells a lost race from
// any other failure by reading the branch again.
func (w *writer) SetBranch(ctx context.Context, name, id, old string) error {
	ref, err := w.branchRef(ctx, name)
	if err != nil {
		return err
	}
	if out, err := w.CheckedOut(ctx, name); err != nil || out {
		if err != nil {
			return err
		}
		return fmt.Errorf("%s is checked out; Physa does not move a checked-out branch", name)
	}
	full, ok := w.Resolve(ctx, id)
	if !ok {
		return noChange(ctx, id)
	}
	expect := ""
	if old != "" {
		if expect, ok = w.Resolve(ctx, old); !ok {
			if ctx.Err() != nil {
				return noChange(ctx, old)
			}
			return fmt.Errorf("%s does not hold %s: %w", name, old, vcs.ErrLease)
		}
	}
	_, stderr, err := write(ctx, w.dir, "", "update-ref", "-m", "physa: set "+name, "--end-of-options", ref, full, expect)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return withCtx(ctx, gitError(err, stderr))
	}
	now, exists := w.Resolve(ctx, ref)
	if (expect == "" && exists) || (expect != "" && now != expect) {
		return fmt.Errorf("%s moved: %w", name, vcs.ErrLease)
	}
	return gitError(err, stderr)
}

// noChange is the error for a ref that names no change, or ctx's error when
// ctx has ended, since then git could not tell.
func noChange(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%q cannot be resolved: %w", ref, err)
	}
	return fmt.Errorf("%q names no change", ref)
}

// hexID is a full or abbreviated change id as git writes it.
var hexID = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// Push runs git push with --force-with-lease naming the expected id; git's
// "stale info" rejection is ErrLease.
func (w *writer) Push(ctx context.Context, branch, id, expect string) error {
	ref, err := w.branchRef(ctx, branch)
	if err != nil {
		return err
	}
	full, ok := w.Resolve(ctx, id)
	if !ok {
		return noChange(ctx, id)
	}
	if expect != "" && !hexID.MatchString(expect) {
		return fmt.Errorf("%q is not a change id", expect)
	}
	out, stderr, err := write(ctx, w.dir, "", "push", "--porcelain", "--force-with-lease="+ref+":"+expect, remote, full+":"+ref)
	if err == nil {
		return nil
	}
	if ctx.Err() == nil && strings.Contains(out, "stale info") || strings.Contains(stderr, "stale info") {
		return fmt.Errorf("%s on %s moved: %w", branch, remote, vcs.ErrLease)
	}
	return fmt.Errorf("%s cannot be pushed: %w", branch, withCtx(ctx, gitError(err, stderr)))
}

// workCopy is a detached git worktree in dir, inside the temp dir tmp.
type workCopy struct {
	w        *writer
	tmp, dir string
}

func (c *workCopy) Dir() string { return c.dir }

// Replay cherry-picks id without committing, tests the staged diff, and
// commits reusing id's message and author when it holds anything. The commit
// runs no hook (--no-verify still runs prepare-commit-msg), so the message
// stays as id wrote it.
func (c *workCopy) Replay(ctx context.Context, id string) (bool, error) {
	full, ok := c.w.Resolve(ctx, id)
	if !ok {
		return false, noChange(ctx, id)
	}
	if _, stderr, err := write(ctx, c.dir, "", "cherry-pick", "--no-commit", "--end-of-options", full); err != nil {
		conflicted, _, _ := write(ctx, c.dir, "", "ls-files", "--unmerged")
		if rErr := c.reset(ctx); rErr != nil {
			return false, rErr
		}
		if ctx.Err() != nil {
			return false, withCtx(ctx, gitError(err, stderr))
		}
		if strings.TrimSpace(conflicted) != "" {
			return false, fmt.Errorf("%s: %w", full, vcs.ErrConflict)
		}
		return false, gitError(err, stderr)
	}
	if _, _, err := write(ctx, c.dir, "", "diff", "--cached", "--quiet"); err == nil {
		return false, c.reset(ctx)
	}
	if _, stderr, err := write(ctx, c.dir, "", "-c", noHooks, "commit", "--quiet", "--no-verify", "--reuse-message="+full); err != nil {
		if rErr := c.reset(ctx); rErr != nil {
			return false, rErr
		}
		return false, gitError(err, stderr)
	}
	return true, nil
}

// reset puts the copy back at its own last change, forgetting a stopped
// cherry-pick. It runs to the end even when ctx is cancelled.
func (c *workCopy) reset(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	_, _, _ = write(ctx, c.dir, "", "cherry-pick", "--quit")
	if _, stderr, err := write(ctx, c.dir, "", "reset", "--quiet", "--hard", "HEAD"); err != nil {
		return gitError(err, stderr)
	}
	return nil
}

// Commit stages every file in the copy and commits with --allow-empty,
// running no hook so the message stays as written: --no-verify skips only
// some, so the hooks path is pointed at nothing.
func (c *workCopy) Commit(ctx context.Context, message string) (vcs.Change, error) {
	if strings.TrimSpace(message) == "" {
		return vcs.Change{}, errors.New("a change needs a message")
	}
	if _, stderr, err := write(ctx, c.dir, "", "add", "--all"); err != nil {
		return vcs.Change{}, gitError(err, stderr)
	}
	if _, stderr, err := write(ctx, c.dir, message, "-c", noHooks, "commit", "--quiet", "--no-verify", "--allow-empty", "--file=-"); err != nil {
		return vcs.Change{}, gitError(err, stderr)
	}
	out, stderr, err := write(ctx, c.dir, "", "log", "-n", "1", logFormat, "HEAD", "--")
	if err != nil {
		return vcs.Change{}, gitError(err, stderr)
	}
	changes, err := parseLog(out)
	if err != nil || len(changes) != 1 {
		return vcs.Change{}, fmt.Errorf("the new change cannot be read: %v", err)
	}
	return changes[0], nil
}

// Remove runs git worktree remove --force, then removes the temp dir, which
// is empty by then.
func (c *workCopy) Remove(ctx context.Context) error {
	if _, stderr, err := write(ctx, c.w.dir, "", "worktree", "remove", "--force", "--end-of-options", c.dir); err != nil {
		return fmt.Errorf("the working copy %s cannot be removed: %w", c.dir, gitError(err, stderr))
	}
	if err := os.Remove(c.tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
