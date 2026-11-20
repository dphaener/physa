// Package git is the adapter: it may run git.
package git

import "os/exec"

// Status runs git.
func Status() error { return exec.Command("git", "status").Run() }
