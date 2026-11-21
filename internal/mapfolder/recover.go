package mapfolder

import (
	"os"
	"path/filepath"
	"strings"
)

// Origin says which line of the single text the line numbered line (from 1)
// of the folder's file named file came from: 0 for a generated line, the
// whole file (line 0), or a line or file the folder lacks.
func (f Folder) Origin(file string, line int) int {
	src := f.Source[file]
	if line < 1 || line > len(src) {
		return 0
	}
	return src[line-1]
}

// Write writes every file of the folder into dir, a slash-separated folder
// under root such as CapabilityDir(slug), making the folder when needed.
func (f Folder) Write(root, dir string) error {
	at := filepath.Join(root, filepath.FromSlash(strings.Trim(dir, "/")))
	if err := os.MkdirAll(at, 0o755); err != nil {
		return err
	}
	for name, text := range f.Files {
		if err := os.WriteFile(filepath.Join(at, name), []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}
