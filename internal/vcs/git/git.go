// Package git is the git adapter of the version-control interface: it reads
// the history of a git repository for Physa through the git command. A Repo
// only reads. Writes go only through a Writer, for learning's branch: it
// fetches from origin, makes isolated working copies under Physa's store,
// moves a branch by compare-and-swap and pushes with a lease. It never
// changes the person's working copy, index or checked-out branch.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/vcs"
)

type repo struct {
	dir string
	env []string // the environment git's children run in
}

var _ vcs.Repo = (*repo)(nil)
var _ vcs.Tracker = (*repo)(nil)

// Open returns the repository that contains dir, whose git commands run in
// env. It returns vcs.ErrNotRepo when dir is not in one, and a wrapped error
// when git cannot run, as when env is empty.
func Open(ctx context.Context, dir string, env []string) (vcs.Repo, error) {
	r := &repo{dir: dir, env: slices.Clone(env)}
	_, stderr, err := r.git(ctx, "rev-parse", "--git-dir")
	if err != nil {
		var exit *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exit) && strings.Contains(stderr, "not a git repository") {
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
func (r *repo) Changes(ctx context.Context, rng string) ([]vcs.Change, error) {
	return r.log(ctx, "--no-merges", logFormat, "--end-of-options", rng, "--")
}

// Since returns the non-merge commits reachable from ref committed after t,
// newest first. A zero t means all of them.
func (r *repo) Since(ctx context.Context, ref string, t time.Time) ([]vcs.Change, error) {
	args := []string{"--no-merges"}
	if !t.IsZero() {
		args = append(args, "--since="+t.UTC().Format(time.RFC3339))
	}
	return r.log(ctx, append(args, logFormat, "--end-of-options", ref, "--")...)
}

// Merges returns the merge commits on ref's first-parent line committed
// after t, newest first. A zero t means all of them.
func (r *repo) Merges(ctx context.Context, ref string, t time.Time) ([]vcs.Change, error) {
	args := []string{"--merges", "--first-parent"}
	if !t.IsZero() {
		args = append(args, "--since="+t.UTC().Format(time.RFC3339))
	}
	return r.log(ctx, append(args, logFormat, "--end-of-options", ref, "--")...)
}

// Altered returns the paths commit id altered, relative to the repo root and
// sorted. A merge gives its paths against the first parent.
func (r *repo) Altered(ctx context.Context, id string) ([]string, error) {
	out, stderr, err := r.git(ctx, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--root", "--diff-merges=first-parent", "--end-of-options", id)
	if err != nil {
		return nil, gitError(err, stderr)
	}
	return nulPaths(out), nil
}

// Latest returns the newest n non-merge commits reachable from ref.
func (r *repo) Latest(ctx context.Context, ref string, n int) ([]vcs.Change, error) {
	return r.log(ctx, "--no-merges", "-n", strconv.Itoa(n), logFormat, "--end-of-options", ref, "--")
}

// log runs git log with args and reads the changes it prints.
func (r *repo) log(ctx context.Context, args ...string) ([]vcs.Change, error) {
	out, stderr, err := r.git(ctx, append([]string{"log"}, args...)...)
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
func (r *repo) DefaultBranch(ctx context.Context) (string, error) {
	if out, _, err := r.git(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master", "refs/heads/main", "refs/heads/master"} {
		if r.hasRef(ctx, ref) {
			return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/"), nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("the default branch cannot be found: %w", err)
	}
	return "", errors.New("the default branch cannot be found: no origin/HEAD, origin/main, origin/master, main or master")
}

// Branch returns name and origin/name, each only when it exists.
func (r *repo) Branch(ctx context.Context, name string) []string {
	refs := []string{}
	if r.hasRef(ctx, "refs/heads/"+name) {
		refs = append(refs, name)
	}
	if r.hasRef(ctx, "refs/remotes/origin/"+name) {
		refs = append(refs, "origin/"+name)
	}
	return refs
}

func (r *repo) hasRef(ctx context.Context, ref string) bool {
	_, _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", ref)
	return err == nil
}

// HasChange reports whether id, full or abbreviated, names exactly one commit.
func (r *repo) HasChange(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	_, _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", id+"^{commit}")
	return err == nil
}

// Partial reports whether the repo is a shallow clone.
func (r *repo) Partial(ctx context.Context) bool {
	out, _, err := r.git(ctx, "rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(out) == "true"
}

// StoreDir returns "physa" inside git's common directory, which every
// worktree of the repo shares.
func (r *repo) StoreDir(ctx context.Context) (string, error) {
	out, stderr, err := r.git(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", gitError(err, stderr)
	}
	return filepath.Join(strings.TrimSpace(out), "physa"), nil
}

// Files lists the files directly under dir in commit id, not its subdirectories.
func (r *repo) Files(ctx context.Context, id, dir string) ([]string, error) {
	dir = strings.TrimSuffix(dir, "/")
	out, stderr, err := r.git(ctx, "ls-tree", "-z", "--format=%(objecttype) %(path)", "--end-of-options", id, "--", dir+"/")
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

// Listed lists the working tree's files git tracks or does not ignore, as
// slash paths from the root. The pathspec ":/" keeps the list whole when the
// repo is read from a subdirectory.
func (r *repo) Listed(ctx context.Context) ([]string, error) {
	out, stderr, err := r.git(ctx, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--full-name", "--", ":/")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	return nulPaths(out), nil
}

// nulPaths returns the paths git printed split by NUL, sorted and without
// duplicates.
func nulPaths(out string) []string {
	paths := []string{}
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return slices.Compact(paths)
}

// Tracked lists the files git tracks with their linguist-vendored and
// linguist-generated marks. Git reads the marks from every .gitattributes, in
// one call run from the root, since git reads paths against its directory.
func (r *repo) Tracked(ctx context.Context) ([]vcs.TrackedFile, error) {
	out, stderr, err := r.git(ctx, "ls-files", "-z", "--cached", "--full-name", "--", ":/")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	paths := nulPaths(out)
	files := make([]vcs.TrackedFile, len(paths))
	for i, p := range paths {
		files[i].Path = p
	}
	if len(files) == 0 {
		return files, nil
	}
	top, stderr, err := r.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	index := make(map[string]int, len(files))
	for i, f := range files {
		index[f.Path] = i
	}
	attrs, stderr, err := runGit(ctx, strings.TrimSpace(top), strings.Join(paths, "\x00")+"\x00", r.env,
		"check-attr", "-z", "--stdin", "linguist-vendored", "linguist-generated")
	if err != nil {
		return nil, gitError(err, stderr)
	}
	// With -z, each answer is path, attribute and value, each ended by NUL.
	f := strings.Split(attrs, "\x00")
	for i := 0; i+2 < len(f); i += 3 {
		n, ok := index[f[i]]
		if !ok || (f[i+2] != "set" && f[i+2] != "true") {
			continue
		}
		switch f[i+1] {
		case "linguist-vendored":
			files[n].Vendored = true
		case "linguist-generated":
			files[n].Generated = true
		}
	}
	return files, nil
}

// Resolve returns the full id of the commit ref names.
func (r *repo) Resolve(ctx context.Context, ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	out, _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
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
func (r *repo) Trailer(ctx context.Context, ref, key string) (vcs.Change, bool, error) {
	if !trailerKey.MatchString(key) {
		return vcs.Change{}, false, fmt.Errorf("%q is not a trailer key", key)
	}
	changes, err := r.log(ctx, "--no-merges", "-i", "--grep=^"+key+":", logFormat, "--end-of-options", ref, "--")
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
func (r *repo) IsAncestor(ctx context.Context, a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	_, _, err := r.git(ctx, "merge-base", "--is-ancestor", "--end-of-options", a, b)
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
func (r *repo) git(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	return runGit(ctx, r.dir, "", r.env, args...)
}

// errNoEnv refuses git an empty environment, which would leave it no HOME or PATH.
var errNoEnv = errors.New("git cannot run with an empty environment")

// runGit runs one git command in dir with a copy of env, stdin as its input,
// git's messages in C. An empty env is refused.
func runGit(ctx context.Context, dir, stdin string, env []string, args ...string) (stdout, stderr string, err error) {
	if len(env) == 0 {
		return "", "", errNoEnv
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(slices.Clip(env), "LC_ALL=C")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	err = cmd.Run()
	return out.String(), strings.TrimSpace(errBuf.String()), withCtx(ctx, err)
}

// withCtx adds ctx's error to err, a failed run's, when ctx has ended and err
// does not already carry it.
func withCtx(ctx context.Context, err error) error {
	if err == nil || ctx.Err() == nil || errors.Is(err, ctx.Err()) {
		return err
	}
	return fmt.Errorf("%w (%w)", err, ctx.Err())
}

// gitError makes git's stderr the error text, or the run error when git
// printed nothing. A run its ctx ended also carries the ctx's error.
func gitError(err error, stderr string) error {
	if stderr == "" {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", stderr, err)
	}
	return errors.New(stderr)
}
