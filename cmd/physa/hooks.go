package main

import (
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/sessionhook"
	"github.com/dphaener/physa/internal/vcs/git"
)

// hooks is physa map hooks: it makes the repo at dir stamp Session:, by
// pointing its hooks at the hook the repo ships or by writing Physa's embedded
// hook beside its own. It returns 0 when it did so or found it done, and
// 2 when the repo cannot be opened or Install refuses, saying why.
func hooks(dir string, stdout, stderr io.Writer) int {
	h, err := git.OpenHooks(dir)
	if err != nil {
		return failed(stderr, "hooks", err)
	}
	out, err := sessionhook.Install(dir, h)
	if err != nil {
		return failed(stderr, "hooks", err)
	}
	switch {
	case out.Written:
		fmt.Fprintf(stdout, "prepare-commit-msg written to %s: commits made in Claude Code now carry Session:.\n", out.Path)
	case out.Already && out.Path != "":
		fmt.Fprintf(stdout, "%s already holds the Session: hook.\n", out.Path)
	case out.Already:
		fmt.Fprintf(stdout, "core.hooksPath is already %s.\n", sessionhook.Dir)
	default:
		fmt.Fprintf(stdout, "core.hooksPath set to %s: commits made in Claude Code now carry Session:.\n", sessionhook.Dir)
	}
	return 0
}
