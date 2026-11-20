// Package other runs gh and a computed program, which are not flagged.
package other

import "os/exec"

// Run runs gh and a program that is not a literal.
func Run(prog string) error {
	if err := exec.Command("gh", "pr", "list").Run(); err != nil {
		return err
	}
	return exec.Command(prog, "status").Run()
}
