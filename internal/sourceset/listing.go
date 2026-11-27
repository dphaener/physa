package sourceset

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/draft"
)

// Part is one cut of the source listing: its text and the files it names.
type Part struct {
	Text  string
	Files []string
}

// folder is one folder's files, by base name, with their line count.
type folder struct {
	dir   string
	names []string
	lines int
}

// folders groups files by folder, folders sorted, names sorted. The repo
// root is the folder ".".
func folders(files []File) []folder {
	by := map[string]*folder{}
	for _, f := range files {
		d := path.Dir(f.Path)
		g := by[d]
		if g == nil {
			g = &folder{dir: d}
			by[d] = g
		}
		g.names = append(g.names, path.Base(f.Path))
		g.lines += f.Lines
	}
	out := make([]folder, 0, len(by))
	for _, g := range by {
		sort.Strings(g.names)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out
}

// Label is a folder as the listings write it: root files sit under "./".
func Label(dir string) string {
	if dir == "." {
		return "./"
	}
	return draft.OneLine(dir) + "/"
}

// head is the start of a folder's line, "dir/ (N files, L lines)".
func (g folder) head() string {
	return fmt.Sprintf("%s (%s, %d lines)", Label(g.dir), FileCount(len(g.names)), g.lines)
}

// FileCount is n files as the listings write it: "1 file", "3 files".
func FileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// treeLine is one folder's line of the Tree.
func (g folder) treeLine() string {
	names := make([]string, len(g.names))
	for i, n := range g.names {
		names[i] = draft.OneLine(n)
	}
	return g.head() + ": " + strings.Join(names, " ")
}

// Tree lists every folder on one line: "dir/ (N files, L lines): a.go b.go".
// Folders are sorted and root files sit under "./". The same files always
// give the same text.
func Tree(files []File) string {
	var lines []string
	for _, g := range folders(files) {
		lines = append(lines, g.treeLine())
	}
	return strings.Join(lines, "\n")
}

// Overview lists every folder with its counts and no file names.
func Overview(files []File) string {
	var lines []string
	for _, g := range folders(files) {
		lines = append(lines, g.head())
	}
	return strings.Join(lines, "\n")
}

// Kinds lists every folder with its file count and a tally of the file
// extensions in it, such as "spec/models/ (40 files: .rb 40)", for the
// session that names the files that are not source.
func Kinds(files []File) string {
	tally := map[string]map[string]int{}
	for _, f := range files {
		d := path.Dir(f.Path)
		if tally[d] == nil {
			tally[d] = map[string]int{}
		}
		ext := path.Ext(f.Path)
		if ext == "" {
			ext = "(none)"
		}
		tally[d][draft.OneLine(ext)]++
	}
	var lines []string
	for _, g := range folders(files) {
		t := tally[g.dir]
		exts := make([]string, 0, len(t))
		for e := range t {
			exts = append(exts, e)
		}
		sort.Slice(exts, func(i, j int) bool {
			if t[exts[i]] != t[exts[j]] {
				return t[exts[i]] > t[exts[j]]
			}
			return exts[i] < exts[j]
		})
		var b strings.Builder
		fmt.Fprintf(&b, "%s (%s:", Label(g.dir), FileCount(len(g.names)))
		for _, e := range exts {
			fmt.Fprintf(&b, " %s %d", e, t[e])
		}
		b.WriteString(")")
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

// group is the key that keeps neighbouring folders together: a folder's
// first two path segments.
func group(dir string) string {
	segs := strings.Split(dir, "/")
	return strings.Join(segs[:min(2, len(segs))], "/")
}

// run is consecutive folders of one group, cut apart only for size.
type run struct {
	lines []string
	dirs  []string
	size  int
}

// Cut cuts the Tree's lines into parts of at most size characters. It
// never cuts inside a folder's line, keeps consecutive folders sharing their
// first two path segments together when they fit, and gives a line larger
// than size a part of its own. Every file is in exactly one part. It only
// cuts for size: it never groups files by what they do.
func Cut(files []File, size int) []Part {
	var runs []*run
	last := ""
	for _, g := range folders(files) {
		line := g.treeLine()
		if k := group(g.dir); len(runs) > 0 && k == last {
			r := runs[len(runs)-1]
			r.lines, r.dirs, r.size = append(r.lines, line), append(r.dirs, g.dir), r.size+len(line)+1
		} else {
			runs = append(runs, &run{lines: []string{line}, dirs: []string{g.dir}, size: len(line) + 1})
			last = k
		}
	}
	var cuts []*run
	cur := &run{}
	flush := func() {
		if len(cur.lines) > 0 {
			cuts = append(cuts, cur)
		}
		cur = &run{}
	}
	for _, r := range runs {
		if cur.size+r.size > size {
			flush()
		}
		if r.size <= size {
			cur.lines, cur.dirs, cur.size = append(cur.lines, r.lines...), append(cur.dirs, r.dirs...), cur.size+r.size
			continue
		}
		for i, l := range r.lines {
			if cur.size+len(l)+1 > size {
				flush()
			}
			cur.lines, cur.dirs, cur.size = append(cur.lines, l), append(cur.dirs, r.dirs[i]), cur.size+len(l)+1
		}
	}
	flush()

	which := map[string]int{}
	for i, c := range cuts {
		for _, d := range c.dirs {
			which[d] = i
		}
	}
	parts := make([]Part, len(cuts))
	for i, c := range cuts {
		parts[i].Text = strings.Join(c.lines, "\n")
	}
	for _, f := range files {
		i := which[path.Dir(f.Path)]
		parts[i].Files = append(parts[i].Files, f.Path)
	}
	return parts
}

// Only is the files whose paths are in paths, in the order of files.
func Only(files []File, paths []string) []File {
	is := make(map[string]bool, len(paths))
	for _, p := range paths {
		is[p] = true
	}
	var out []File
	for _, f := range files {
		if is[f.Path] {
			out = append(out, f)
		}
	}
	return out
}

// Brief is one line saying what an area holds: its name, promise, file count
// and the six folders holding most of its files.
func Brief(name, promise string, files []string) string {
	by := map[string]int{}
	for _, f := range files {
		by[path.Dir(f)]++
	}
	dirs := make([]string, 0, len(by))
	for d := range by {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if by[dirs[i]] != by[dirs[j]] {
			return by[dirs[i]] > by[dirs[j]]
		}
		return dirs[i] < dirs[j]
	})
	dirs = dirs[:min(6, len(dirs))]
	for i, d := range dirs {
		dirs[i] = "`" + Label(d) + "`"
	}
	s := fmt.Sprintf("- `%s`: %s (%s", draft.OneLine(name), draft.OneLine(promise), FileCount(len(files)))
	if len(dirs) > 0 {
		s += "; mostly in " + strings.Join(dirs, ", ")
	}
	return s + ")"
}
