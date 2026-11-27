// Package sourceset says which of a repo's tracked files are source for init: it leaves out what git or the repo marks (vendored, generated), binary files and lock files, applies the test, doc and other patterns a session named, and resolves path claims so each file goes to the most specific one. It also writes the listings of those files that init's sessions read, and cuts them into parts by size.
package sourceset

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/dphaener/physa/internal/vcs"
)

// Reasons a file is left out of a Listing.
const (
	LeftVendored  = "vendored"
	LeftGenerated = "generated"
	LeftBinary    = "binary"
	LeftLock      = "lock"
	LeftNotAFile  = "not a file"
)

// binarySniff is how many leading bytes are searched for a NUL, which is
// git's own rule for calling a file binary.
const binarySniff = 8000

// lockFiles are the base names of well-known lock files.
var lockFiles = map[string]bool{
	"package-lock.json": true, "npm-shrinkwrap.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "bun.lockb": true, "bun.lock": true, "deno.lock": true,
	"Gemfile.lock": true, "Cargo.lock": true, "go.sum": true, "go.work.sum": true,
	"poetry.lock": true, "Pipfile.lock": true, "pdm.lock": true, "uv.lock": true,
	"composer.lock": true, "mix.lock": true, "pubspec.lock": true,
	"Podfile.lock": true, "Package.resolved": true, "flake.lock": true,
	"packages.lock.json": true, "gradle.lockfile": true, "conan.lock": true,
	"Manifest.toml": true, "renv.lock": true,
}

// File is a source candidate: a tracked file that was not left out.
type File struct {
	// Path is the slash path from the repo root.
	Path string
	// Lines is the number of newlines in the file.
	Lines int
}

// Listing is the files kept and, by reason, the paths left out.
type Listing struct {
	Files []File
	// Left maps a reason (the Left constants) to the paths left out for it.
	Left map[string][]string
}

// List reads the tracked files under root and keeps those that are text, not
// marked vendored or generated, not lock files and regular. Paths stay slash
// paths and the order of files is kept.
func List(root string, files []vcs.TrackedFile) (Listing, error) {
	out := Listing{Left: map[string][]string{}}
	left := func(reason, p string) { out.Left[reason] = append(out.Left[reason], p) }
	for _, f := range files {
		switch {
		case f.Vendored:
			left(LeftVendored, f.Path)
			continue
		case f.Generated:
			left(LeftGenerated, f.Path)
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(f.Path))
		info, err := os.Lstat(full)
		if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
			left(LeftNotAFile, f.Path)
			continue
		}
		if err != nil {
			return Listing{}, err
		}
		if lockFiles[path.Base(f.Path)] {
			left(LeftLock, f.Path)
			continue
		}
		lines, binary, err := scan(full)
		if err != nil {
			return Listing{}, err
		}
		if binary {
			left(LeftBinary, f.Path)
			continue
		}
		out.Files = append(out.Files, File{Path: f.Path, Lines: lines})
	}
	return out, nil
}

// scan reads the file once, counting newlines and watching for a NUL in the
// first binarySniff bytes, where it stops reading.
func scan(full string) (lines int, binary bool, err error) {
	f, err := os.Open(full)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	buf := make([]byte, 32*1024)
	seen := 0
	for {
		n, rerr := f.Read(buf)
		chunk := buf[:n]
		if seen < binarySniff {
			head := chunk
			if len(head) > binarySniff-seen {
				head = head[:binarySniff-seen]
			}
			if bytes.IndexByte(head, 0) >= 0 {
				return 0, true, nil
			}
		}
		seen += n
		lines += bytes.Count(chunk, []byte{'\n'})
		if rerr == io.EOF {
			return lines, false, nil
		}
		if rerr != nil {
			return 0, false, rerr
		}
	}
}
