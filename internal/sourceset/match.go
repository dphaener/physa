package sourceset

import (
	"math"
	"path"
	"strings"
)

// Kind says what a file is for init.
type Kind string

// The kinds, in the order a tie between patterns is settled.
const (
	Test   Kind = "test"
	Doc    Kind = "doc"
	Other  Kind = "other"
	Source Kind = "source"
)

// exact is the specificity of a pattern that is the file's own path.
const exact = math.MaxInt32

// Division is the result of Split.
type Division struct {
	// Tests, Docs and Other are the patterns as given.
	Tests, Docs, Other []string
	// Files holds the paths of each kind, in input order.
	Files map[Kind][]string
	// Unused are the patterns that matched no file, kept and reported.
	Unused []string
	// Dropped are the patterns that matched every listed file, left out
	// before the split.
	Dropped []string
}

// clean trims a leading "./" or "/" from a pattern.
func clean(pattern string) string {
	pattern = strings.TrimPrefix(pattern, "./")
	return strings.TrimLeft(pattern, "/")
}

func hasGlob(s string) bool { return strings.ContainsAny(s, "*?[") }

// Match reports whether pattern names file. A leading "./" or "/" is
// trimmed and an empty pattern matches nothing. "dir/" matches every file
// under a folder named dir at any depth, as in .gitignore, and "a/dir/"
// every file under that path from the root. A pattern with no "/" matches the base name in any folder, and
// a glob-free one also names a folder at the root. Otherwise segments are
// matched one by one with path.Match, and a "**" segment matches zero or more
// segments. A literal path matches that file, or every file under it when it
// names a folder. A malformed pattern matches nothing.
func Match(pattern, file string) bool {
	pattern = clean(pattern)
	file = strings.TrimLeft(file, "/")
	if pattern == "" || file == "" {
		return false
	}
	dir := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimRight(pattern, "/")
	if pattern == "" {
		return false
	}
	if !dir && !strings.Contains(pattern, "/") {
		if ok, err := path.Match(pattern, path.Base(file)); err == nil && ok {
			return true
		}
		return !hasGlob(pattern) && strings.HasPrefix(file, pattern+"/")
	}
	if dir && !strings.Contains(pattern, "/") {
		// As in .gitignore, "name/" names a folder of that name at any depth.
		segs := strings.Split(file, "/")
		for _, seg := range segs[:len(segs)-1] {
			if ok, err := path.Match(pattern, seg); err == nil && ok {
				return true
			}
		}
		return false
	}
	if dir {
		pattern += "/**"
	}
	return matchSegs(strings.Split(pattern, "/"), strings.Split(file, "/"), !hasGlob(pattern))
}

// matchSegs matches pattern segments against the file's. When the pattern is
// used up and file segments remain, the pattern names a folder, which counts
// only when under is true.
func matchSegs(pat, file []string, under bool) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return len(file) > 0
			}
			for i := 0; i <= len(file); i++ {
				if matchSegs(rest, file[i:], under) {
					return true
				}
			}
			return false
		}
		if len(file) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], file[0])
		if err != nil || !ok {
			return false
		}
		pat, file = pat[1:], file[1:]
	}
	return len(file) == 0 || under
}

// Specificity says how closely pattern names file: 0 for no match, the
// highest value for the file's exact path, otherwise the length of the
// pattern's literal prefix before its first "*", "?" or "[" (for "dir/", its
// length) plus 1.
func Specificity(pattern, file string) int {
	if !Match(pattern, file) {
		return 0
	}
	p := clean(pattern)
	if p == strings.TrimLeft(file, "/") {
		return exact
	}
	if i := strings.IndexAny(p, "*?["); i >= 0 {
		p = p[:i]
	}
	return len(p) + 1
}

// best is the highest specificity of any pattern on file.
func best(patterns []string, file string) int {
	top := 0
	for _, p := range patterns {
		if s := Specificity(p, file); s > top {
			top = s
		}
	}
	return top
}

// Split sorts files into tests, docs, other and source. Each file goes to the
// kind with the most specific matching pattern, a tie going to the first kind
// in that order, and a file matching none is source. A pattern that matches
// no file is reported in Unused; one that matches every file is dropped and
// reported in Dropped, since it would leave no source.
func Split(files []File, tests, docs, other []string) Division {
	d := Division{Tests: tests, Docs: docs, Other: other, Files: map[Kind][]string{
		Test: {}, Doc: {}, Other: {}, Source: {},
	}}
	kinds := []Kind{Test, Doc, Other}
	sets := [][]string{tests, docs, other}
	kept := make([][]string, len(sets))
	for i, set := range sets {
		for _, p := range set {
			n := 0
			for _, f := range files {
				if Match(p, f.Path) {
					n++
				}
			}
			switch {
			case n == 0:
				d.Unused = append(d.Unused, p)
				kept[i] = append(kept[i], p)
			case n == len(files):
				d.Dropped = append(d.Dropped, p)
			default:
				kept[i] = append(kept[i], p)
			}
		}
	}
	for _, f := range files {
		kind, top := Source, 0
		for i, k := range kinds {
			if s := best(kept[i], f.Path); s > top {
				kind, top = k, s
			}
		}
		d.Files[kind] = append(d.Files[kind], f.Path)
	}
	return d
}
