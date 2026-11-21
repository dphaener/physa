package vcs

// Hooks reads and sets where a repo's hooks live, so that Physa can point a
// repo at the hook it ships (the one that stamps Session: on changes). It
// writes only the repo's own setting, never a person's global or system
// settings, and never touches the hook files themselves (writing one is the caller's job).
type Hooks interface {
	// HooksPath returns the repo's hooks path setting (git's core.hooksPath)
	// as written, and whether it is set in any scope.
	HooksPath() (path string, set bool, err error)
	// SetHooksPath sets the repo's own hooks path setting to p.
	SetHooksPath(p string) error
	// ActiveHooks returns the names of the executable files in the repo's
	// default hooks directory, whatever the hooks path says, leaving out
	// samples. It is empty when the directory is absent.
	ActiveHooks() ([]string, error)
	// HooksDir returns the absolute path of the repo's default hooks
	// directory, under git's common dir, whatever the hooks path says. The
	// directory may not exist.
	HooksDir() (string, error)
}
