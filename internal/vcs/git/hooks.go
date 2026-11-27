package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/vcs"
)

// hooksKey is git's setting for where hooks live.
const hooksKey = "core.hooksPath"

// noHooks is a hooks path that holds no hook, for Physa's own commits.
const noHooks = "core.hooksPath=/dev/null"

type hooks struct {
	*repo
}

var _ vcs.Hooks = (*hooks)(nil)

// OpenHooks returns the hooks of the repository that contains dir. Its errors
// are Open's; its git commands run in env.
func OpenHooks(ctx context.Context, dir string, env []string) (vcs.Hooks, error) {
	r, err := Open(ctx, dir, env)
	if err != nil {
		return nil, err
	}
	return &hooks{repo: r.(*repo)}, nil
}

// HooksPath reads core.hooksPath from every scope git reads, since a global
// setting is what the repo runs when the local one is unset. git config exits
// 1, printing nothing, when the key is unset.
func (h *hooks) HooksPath(ctx context.Context) (string, bool, error) {
	out, stderr, err := h.git(ctx, "config", "--get", hooksKey)
	var exit *exec.ExitError
	if ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 1 && stderr == "" {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("%s cannot be read: %w", hooksKey, gitError(err, stderr))
	}
	return strings.TrimSuffix(out, "\n"), true, nil
}

// SetHooksPath writes core.hooksPath to the repo's own config only.
func (h *hooks) SetHooksPath(ctx context.Context, p string) error {
	if p == "" {
		return errors.New("a hooks path cannot be empty")
	}
	if _, stderr, err := h.git(ctx, "config", "--local", hooksKey, p); err != nil {
		return fmt.Errorf("%s cannot be set: %w", hooksKey, gitError(err, stderr))
	}
	return nil
}

// ActiveHooks lists the hooks directory under git's common dir, the default
// that every working copy of the repo shares. git rev-parse --git-path hooks
// would follow core.hooksPath instead.
func (h *hooks) ActiveHooks(ctx context.Context) ([]string, error) {
	dir, err := h.HooksDir(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sample") {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// HooksDir is the hooks directory under git's common dir.
func (h *hooks) HooksDir(ctx context.Context) (string, error) {
	out, stderr, err := h.git(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", gitError(err, stderr)
	}
	return filepath.Join(strings.TrimSpace(out), "hooks"), nil
}

// Email reads user.email from every scope git reads, for the repository at
// dir or outside any, with git run in env. It is "" with no error when unset:
// git config exits 1, printing nothing. It never reads user.name.
func Email(ctx context.Context, dir string, env []string) (string, error) {
	out, stderr, err := runGit(ctx, dir, "", env, "config", "--get", "user.email")
	var exit *exec.ExitError
	if ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 1 && stderr == "" {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("user.email cannot be read: %w", gitError(err, stderr))
	}
	return strings.TrimSpace(out), nil
}
