package runner

import (
	"os/exec"
	"testing"
)

func TestRun(t *testing.T) { _ = exec.Command("git", "status").Run() }
