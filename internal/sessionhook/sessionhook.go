// Package sessionhook points a repo's hooks at the hook the repo ships, the
// one that stamps Session: on changes made in a Claude Code session, so that
// learning can join each change to the session that made it (design 07
// section 4.1). It sets the repo's hooks path only when nothing would be
// lost: it never overwrites a hooks path already set and never turns off
// hooks already active, saying instead what it found and how to add the hook
// by hand.
package sessionhook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/vcs"
)

// Dir is where a repo ships its hooks, relative to its root, and the hooks
// path Install sets.
const Dir = "tools/hooks"

// Hook is the shipped hook that stamps Session:, relative to the root.
const Hook = Dir + "/prepare-commit-msg"

// Outcome says what Install did.
type Outcome struct {
	Set     bool // the hooks path now points at Dir; it was unset
	Already bool // the hooks path already pointed at Dir; nothing changed
}

// Install points the hooks of the repo at root to Dir, through h. It is an
// error, changing nothing, when the repo ships no executable Hook, when the
// hooks path is set to anything else, or when the repo's default hooks
// directory holds active hooks that the new path would turn off.
func Install(root string, h vcs.Hooks) (Outcome, error) {
	hook := filepath.Join(root, filepath.FromSlash(Hook))
	info, err := os.Stat(hook)
	if errors.Is(err, os.ErrNotExist) {
		return Outcome{}, fmt.Errorf("the repo ships no hook: %s is missing", Hook)
	}
	if err != nil {
		return Outcome{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return Outcome{}, fmt.Errorf("%s is not an executable file, so git would not run it", Hook)
	}
	path, set, err := h.HooksPath()
	if err != nil {
		return Outcome{}, err
	}
	if set {
		if pointsAtDir(root, path) {
			return Outcome{Already: true}, nil
		}
		return Outcome{}, fmt.Errorf("core.hooksPath is already %q, so it was left alone; to stamp Session: on changes, call %s from that directory's prepare-commit-msg hook", path, Hook)
	}
	active, err := h.ActiveHooks()
	if err != nil {
		return Outcome{}, err
	}
	if len(active) > 0 {
		return Outcome{}, fmt.Errorf("the repo has active hooks (%s) that pointing core.hooksPath at %s would turn off, so nothing was changed; to stamp Session: on changes, call %s from your own prepare-commit-msg hook", strings.Join(active, ", "), Dir, Hook)
	}
	if err := h.SetHooksPath(Dir); err != nil {
		return Outcome{}, err
	}
	return Outcome{Set: true}, nil
}

// pointsAtDir reports whether a hooks path names Dir under root, written
// relative to it or absolute.
func pointsAtDir(root, path string) bool {
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path)) == Dir
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	return filepath.Clean(path) == filepath.Join(abs, filepath.FromSlash(Dir))
}
