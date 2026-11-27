package main

import (
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/command"
)

// TestUsageNamesDeclaredFlags ties every declared command's usage lines to
// the flags it declares: help prints the usage lines, so a flag they don't
// name is hidden from the reader, and a --flag they name but the command
// doesn't declare is a bad flag.
func TestUsageNamesDeclaredFlags(t *testing.T) {
	t.Parallel()
	if problems := command.Drift(rootCommand(testDeps(t))); len(problems) > 0 {
		t.Fatalf("usage lines and declared flags drift:\n%s", strings.Join(problems, "\n"))
	}
}
