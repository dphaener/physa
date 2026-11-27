// Package repos keeps the repos registered on this machine, the ones the engine serves, one absolute path per line in the repos file of Physa's settings directory, and opens their state.
package repos

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/vcs/git"
)

// File returns the repos file inside the settings directory dir.
func File(dir string) string {
	return filepath.Join(dir, "repos")
}

// List returns the registered repo paths in file order. A missing file is an
// empty list. Blank lines and duplicates are ignored.
func List(dir string) ([]string, error) {
	f, err := os.Open(File(dir))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []string{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", File(dir), err)
	}
	return out, nil
}

// Add registers the absolute path root. It reports false when root was
// already registered. Entries are only ever appended.
func Add(dir, root string) (added bool, err error) {
	if !filepath.IsAbs(root) {
		return false, fmt.Errorf("repo path %q is not absolute", root)
	}
	root = filepath.Clean(root)
	if strings.ContainsAny(root, "\r\n") {
		return false, fmt.Errorf("repo path %q holds a newline", root)
	}
	have, err := List(dir)
	if err != nil {
		return false, err
	}
	for _, p := range have {
		if p == root {
			return false, nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(File(dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(root + "\n"); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

// StoreDir returns the store directory of the repo at path. A path that is
// not a repo is an error naming it.
func StoreDir(path string) (string, error) {
	ctx := context.TODO()
	r, err := git.Open(ctx, path)
	if err != nil {
		return "", fmt.Errorf("%s: not a repo: %w", path, err)
	}
	dir, err := r.StoreDir(ctx)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return dir, nil
}
