// Package sessionhook installs the hook that stamps Session: on changes made
// in a Claude Code session, so that learning can join each change to the
// session that made it (design 07 section 4.1), and it never disables a hook.
// A repo that ships the hook gets its hooks path pointed at the shipped
// directory, only when no active hook would be turned off. Any other repo
// gets Physa's embedded copy written into its default hooks directory, where
// every hook already there keeps running and the hooks path stays as it was.
// It never overwrites a hooks path or a hook file: it says instead what it
// found and how to add the hook by hand.
package sessionhook

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/vcs"
)

// Dir is where a repo may ship its hooks, relative to its root, and the hooks
// path Install sets for such a repo.
const Dir = "tools/hooks"

// Hook is the shipped hook that stamps Session:, relative to the root.
const Hook = Dir + "/prepare-commit-msg"

// name is the hook's file name in a hooks directory.
const name = "prepare-commit-msg"

// script is the hook Physa embeds, a copy of Hook in the Physa repo.
//
//go:embed prepare-commit-msg
var script []byte

// Outcome says what Install did.
type Outcome struct {
	Set     bool   // the hooks path now points at Dir; it was unset
	Already bool   // the hook was already in place; nothing changed
	Written bool   // the embedded hook was written to Path
	Path    string // the hook file, when Written or when found there Already
}

// Install makes the repo at root stamp Session:, through h, changing nothing
// when it refuses. In order: when the hooks path is set, it is Already when it
// names Dir and the repo ships the hook, and an error otherwise. When the repo
// ships an executable Hook, the hooks path is set to Dir, unless active hooks
// in the default hooks directory would be turned off, which is an error. For
// any other repo the embedded hook is written, executable, to the default
// hooks directory, leaving the hooks path alone; it is Already when the file
// holds exactly that hook, and an error, leaving the file, when it holds
// anything else.
func Install(root string, h vcs.Hooks) (Outcome, error) {
	shipped := ships(root)
	path, set, err := h.HooksPath()
	if err != nil {
		return Outcome{}, err
	}
	if set {
		if shipped && pointsAtDir(root, path) {
			return Outcome{Already: true}, nil
		}
		return Outcome{}, fmt.Errorf("core.hooksPath is already %q, so it was left alone; to stamp Session: on changes, call Physa's hook (%s in the Physa repo) from that directory's prepare-commit-msg hook", path, Hook)
	}
	if shipped {
		return setPath(h)
	}
	return write(h)
}

// ships reports whether the repo at root ships an executable Hook.
func ships(root string) bool {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(Hook)))
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// setPath points the hooks path at Dir unless that would turn off a hook.
func setPath(h vcs.Hooks) (Outcome, error) {
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

// write puts the embedded hook in the default hooks directory, never over a
// file that is already there.
func write(h vcs.Hooks) (Outcome, error) {
	dir, err := h.HooksDir()
	if err != nil {
		return Outcome{}, err
	}
	file := filepath.Join(dir, name)
	existing, err := os.ReadFile(file)
	if err == nil {
		if bytes.Equal(existing, script) {
			return Outcome{Already: true, Path: file}, nil
		}
		return Outcome{}, fmt.Errorf("%s already holds a different hook, so it was left alone; to stamp Session: on changes, call Physa's hook (%s in the Physa repo) from your own prepare-commit-msg hook", file, Hook)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Outcome{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Outcome{}, err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return Outcome{}, err
	}
	_, werr := f.Write(script)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return Outcome{}, werr
	}
	// The creation mode is cut by the umask; this file is ours, so set it.
	if err := os.Chmod(file, 0o755); err != nil {
		return Outcome{}, err
	}
	return Outcome{Written: true, Path: file}, nil
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
