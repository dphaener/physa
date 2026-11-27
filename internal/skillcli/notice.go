package skillcli

import (
	"errors"
	"os"
	"strings"

	"github.com/dphaener/physa/internal/skillagent"
	"github.com/dphaener/physa/internal/skillstub"
)

// Notice is the line `physa init` prints when the agent it ran with has no
// current Physa skill, naming the command that installs or updates it, or ""
// when the skill is current, its state cannot be read, home is empty or no
// agent has that name. It reads the same agent list as `physa
// skill status` and only reads: it never writes, updates or removes a stub or
// a hook. The line carries no prefix and no trailing newline, and writes the
// stub's path with ~ for home.
func Notice(agent, home string) string {
	if home == "" {
		return ""
	}
	const setup = "`physa skill setup`"
	none := errors.New("not looked up")
	// Only the stub's path is used, and it depends on home alone, so the
	// list is built without reading the environment, PATH or the disk.
	for _, t := range skillagent.List(skillagent.Env{
		Home:     home,
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", none },
		Stat:     func(string) (os.FileInfo, error) { return nil, none },
	}) {
		if t.Name != agent {
			continue
		}
		path := t.Stub
		state, err := skillstub.Read(path)
		if err != nil {
			return ""
		}
		switch state {
		case skillstub.Missing:
			return t.Label + " has no Physa skill on this account: run " + setup + " to install it, so its sessions learn Physa's commands and terms."
		case skillstub.Stale:
			return t.Label + "'s Physa skill is out of date: run " + setup + " to update it."
		case skillstub.Other:
			shown := "~" + strings.TrimPrefix(path, home)
			return shown + " was not written by Physa, so " + t.Label + " has no current Physa skill, and " + setup + " leaves that file alone: move it aside, then run " + setup + "."
		}
		return ""
	}
	return ""
}
