package mapfolder

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Snapshot is the files directly in a folder, by name, as they were when
// Take read them.
type Snapshot map[string][]byte

// Take reads every regular file directly in the folder dir.
func Take(dir string) (Snapshot, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	s := Snapshot{}
	for _, e := range ents {
		if e.Type().IsRegular() {
			if s[e.Name()], err = os.ReadFile(filepath.Join(dir, e.Name())); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

// Restore puts the folder dir back as s found it: every file s holds is
// written byte for byte, and every other regular file is removed.
func (s Snapshot) Restore(dir string) error {
	ents, err := os.ReadDir(dir)
	for _, e := range ents {
		if _, ok := s[e.Name()]; !ok && e.Type().IsRegular() && err == nil {
			err = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	for name, b := range s {
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name), b, 0o644)
		}
	}
	return err
}

// AddEntry appends entry, one entry's text, last under ## Entries of the
// kind file of the capability folder dir whose title is title, and
// regenerates that file's contents. A kind file the folder lacks is made,
// and index.md then links it. It returns the first and last line (from 1)
// the entry takes in its file. A file outside its format, with the entry
// appended, is an error naming the file and wrapping its *FormatError, whose
// line counts in that appended text; the folder is then as it was. When
// index.md cannot link a kind file just made, that file is left for the
// caller's Snapshot to remove.
func AddEntry(dir, title string, k Kind, entry string) (int, int, error) {
	at := filepath.Join(dir, k.File())
	b, err := os.ReadFile(at)
	created := errors.Is(err, os.ErrNotExist)
	text := KindFile(title, k, []string{entry})
	switch {
	case err == nil:
		if text, err = Format(k.File(), strings.TrimRight(string(b), "\n")+"\n\n"+entry+"\n"); err != nil {
			return 0, 0, fmt.Errorf("%s: %w", k.File(), err)
		}
	case !created:
		return 0, 0, err
	}
	if err := os.WriteFile(at, []byte(text), 0o644); err != nil {
		return 0, 0, err
	}
	if created {
		if err := linkIndex(dir, k); err != nil {
			return 0, 0, err
		}
	}
	n := len(lines(text))
	return n - len(lines(entry)) + 1, n, nil
}

// linkIndex adds to the folder's index.md the link to k's file.
func linkIndex(dir string, k Kind) error {
	at := filepath.Join(dir, IndexFile)
	b, err := os.ReadFile(at)
	if err != nil {
		return err
	}
	ix, err := parseIndex(string(b))
	if err != nil {
		return fmt.Errorf("%s: %w", IndexFile, err)
	}
	return os.WriteFile(at, []byte(renderIndex(ix.fm, ix.title, ix.overview, append(ix.kinds, k)).String()), 0o644)
}
