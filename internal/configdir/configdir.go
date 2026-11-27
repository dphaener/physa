// Package configdir finds the machine's Physa settings directory: $XDG_CONFIG_HOME/physa when that is an absolute path, else ~/.config/physa.
package configdir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Dir returns the settings directory, reading XDG_CONFIG_HOME and HOME
// through getenv, the command's lookup of its environment. It does not
// create it; see Ensure.
func Dir(getenv func(string) string) (string, error) {
	if x := getenv("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "physa"), nil
	}
	h := getenv("HOME")
	if h == "" {
		return "", errors.New("no XDG_CONFIG_HOME and no HOME")
	}
	return filepath.Join(h, ".config", "physa"), nil
}

// Ensure creates dir and its missing parents with mode 0700. An existing
// directory named physa is set to 0700. Any other existing directory that
// is group- or other-accessible is refused and left as it is, so a mistaken
// dir such as the home directory is never tightened. dir must be absolute.
func Ensure(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("configdir: %s is not an absolute path", dir)
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("configdir: %s is not a directory", dir)
	}
	if filepath.Base(dir) == "physa" {
		return os.Chmod(dir, 0o700)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("configdir: refusing to change %s (mode %o): only a directory named physa is set to 0700", dir, st.Mode().Perm())
	}
	return nil
}
