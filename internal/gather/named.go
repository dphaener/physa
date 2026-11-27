package gather

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/dphaener/physa/internal/store"
)

// MaxNamed is the most named sources read in one gather.
const MaxNamed = 200

// textProbe is how much of a file's start is checked for being text.
const textProbe = 8 << 10

// Source is a file someone named as worth reading: a path relative to the
// repo root, one of the five kinds, and the reason it was named.
type Source struct{ Path, Kind, Reason string }

var kinds = map[string]bool{KindAgent: true, KindContributing: true, KindReadme: true, KindDecisions: true, KindOverview: true}

// Named reads the named sources under root. A source is refused, and never
// read past the checks that need its bytes, when its path is outside the
// repo, under .git, under an ignored folder (or, when listed is not nil, not
// among the repo's listed files), missing, not a regular file (a symlink is
// not), over MaxDocBytes, of an unknown kind, a duplicate of a doc in have or
// of an earlier source, over MaxNamed accepted, or not text (a NUL byte or
// invalid UTF-8 in its first 8 KB). Each refusal is a Skip with its why.
func Named(root string, listed map[string]bool, srcs []Source, have []store.Doc) (docs []store.Doc, skipped []Skip) {
	return named(os.Open, root, listed, srcs, have)
}

func named(open func(string) (*os.File, error), root string, listed map[string]bool, srcs []Source, have []store.Doc) (docs []store.Doc, skipped []Skip) {
	seen := map[string]bool{}
	for _, d := range have {
		seen[d.Path] = true
	}
	for _, src := range srcs {
		rel := path.Clean(filepath.ToSlash(src.Path))
		body, why := readNamed(open, root, rel, src, listed, seen, len(docs))
		if why != "" {
			skipped = append(skipped, Skip{rel, why})
			continue
		}
		seen[rel] = true
		docs = append(docs, store.Doc{Path: rel, Kind: src.Kind, Reason: src.Reason, Body: body})
	}
	return docs, skipped
}

// readNamed returns the body of the named source, or why it is refused. The
// checks that need no bytes come first, so a refused file is not opened
// unless the refusal is that it is not text.
func readNamed(open func(string) (*os.File, error), root, rel string, src Source, listed, seen map[string]bool, accepted int) (string, string) {
	if filepath.IsAbs(src.Path) || path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", "outside the repo"
	}
	segs := strings.Split(rel, "/")
	const ignored = "under an ignored folder"
	for _, s := range segs {
		if s == ".git" {
			return "", "under .git"
		}
		if ignoredDirs[s] {
			return "", ignored
		}
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil {
		return "", "missing"
	}
	if listed != nil && !listed[rel] {
		return "", ignored
	}
	if !info.Mode().IsRegular() {
		return "", "not a regular file"
	}
	if !within(root, filepath.Dir(full)) {
		return "", "outside the repo"
	}
	if info.Size() > MaxDocBytes {
		return "", "over 2 MB"
	}
	if !kinds[src.Kind] {
		return "", "unknown kind"
	}
	if seen[rel] {
		return "", "a duplicate"
	}
	if accepted >= MaxNamed {
		return "", "over the limit of 200 named sources"
	}
	f, err := open(full)
	if err != nil {
		return "", "missing"
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxDocBytes+1))
	if err != nil {
		return "", "not a regular file"
	}
	head := b[:min(len(b), textProbe)]
	for i := 0; i < utf8.UTFMax-1 && len(b) > textProbe && !utf8.Valid(head); i++ {
		head = head[:len(head)-1] // a rune cut by the probe's end is not invalid
	}
	if strings.ContainsRune(string(head), 0) || !utf8.Valid(head) {
		return "", "not text"
	}
	return string(b), ""
}

// within reports whether dir, with its symlinks resolved, is inside root.
func within(root, dir string) bool {
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	d, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(r, d)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
