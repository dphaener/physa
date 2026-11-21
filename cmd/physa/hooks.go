package main

import (
	"fmt"
	"io"

	"github.com/dphaener/physa/internal/sessionhook"
	"github.com/dphaener/physa/internal/vcs/git"
)

// hooks is physa map hooks: it points the hooks of the repo at dir to the
// hook the repo ships. It returns 0 when it set the path or found it set, and
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
	if out.Already {
		fmt.Fprintf(stdout, "core.hooksPath is already %s.\n", sessionhook.Dir)
	} else {
		fmt.Fprintf(stdout, "core.hooksPath set to %s: commits made in Claude Code now carry Session:.\n", sessionhook.Dir)
	}
	return 0
}
