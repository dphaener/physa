// Package skillstub is the stub of Physa's skill that setup writes into an
// agent's skills directory: its text, and whether the stub at a path is
// current, stale, missing or not Physa's. It writes and removes only a stub
// Physa wrote, at the one path it is given, and never follows a symlink.
package skillstub

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Version is the stub's version. A stub written under another version reads
// as stale.
const Version = "1"

// marker is the frontmatter key that says Physa wrote the stub.
const marker = "physa-stub:"

// ErrNotPhysas is returned when the file at the path is not a stub Physa
// wrote, so it is left alone.
var ErrNotPhysas = errors.New("not a stub Physa wrote")

// State is what is at a stub's path.
type State int

const (
	// Missing means there is no file.
	Missing State = iota
	// Current means Physa's stub, exactly as this Physa writes it.
	Current
	// Stale means Physa's stub with other bytes: an older version, or edited.
	Stale
	// Other means something Physa did not write, or a symlink.
	Other
)

// String names the state as the commands print it.
func (s State) String() string {
	switch s {
	case Missing:
		return "missing"
	case Current:
		return "current"
	case Stale:
		return "stale"
	default:
		return "other"
	}
}

// Text is the stub: the skill's name, a description that makes the agent load
// it in a repo with a .physa folder, the stub's version, and one instruction
// to run physa skill show and follow it.
func Text() string {
	return `---
name: physa
description: "Use Physa's map and commands in a repo with a .physa/ folder: read the index and the slice before changing code, anchor each commit to a capability, check the map, and work a pursuit's planned change. Load it whenever the repo has a .physa/ folder."
metadata:
  physa-stub: "` + Version + `"
---

<!-- Written by "physa skill setup". Remove it with "physa skill remove". -->

Run ` + "`physa skill show`" + ` and follow the guide it prints. The guide comes from the installed Physa, so it describes that version's commands.

If that command fails, the installed Physa predates the guide: start with ` + "`physa --help`" + `, then ` + "`physa <command> --help`" + `.
`
}

// Read says what is at path. It never follows a symlink, in the file or in
// its directory.
func Read(path string) (State, error) {
	dir, err := os.Lstat(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return Other, err
	}
	if !dir.IsDir() {
		return Other, nil
	}
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return Other, err
	}
	if !fi.Mode().IsRegular() {
		return Other, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Other, err
	}
	if !physas(b) {
		return Other, nil
	}
	if bytes.Equal(b, []byte(Text())) {
		return Current, nil
	}
	return Stale, nil
}

// physas reports whether the frontmatter carries the stub marker.
func physas(b []byte) bool {
	rest, ok := bytes.CutPrefix(b, []byte("---\n"))
	if !ok {
		return false
	}
	for _, line := range bytes.Split(rest, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if string(line) == "---" {
			return false
		}
		if bytes.HasPrefix(line, []byte(marker)) {
			return true
		}
	}
	return false
}

// Write puts the current stub at path, replacing a stale one. A current stub
// is left as it is; a file Physa did not write returns ErrNotPhysas.
func Write(path string) (err error) {
	st, err := Read(path)
	if err != nil {
		return err
	}
	switch st {
	case Other:
		return ErrNotPhysas
	case Current:
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".SKILL.md.*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.WriteString(Text()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Remove deletes the stub at path, then its directory if that is now empty.
// A file Physa did not write returns ErrNotPhysas. It never touches the
// skills directory above.
func Remove(path string) error {
	st, err := Read(path)
	if err != nil {
		return err
	}
	switch st {
	case Missing:
		return nil
	case Other:
		return ErrNotPhysas
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	if len(entries) == 0 {
		return os.Remove(dir)
	}
	return nil
}
