package propose

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapcheck"
	"github.com/dphaener/physa/internal/mapfile"
	"github.com/dphaener/physa/internal/vcs"
)

// mapDir is the map's directory under the repo root.
const mapDir = ".physa"

// tryOn applies p alone to a scratch copy of the map under root and checks
// the copy, with document anchors resolved in root and commit anchors in its
// version control. It returns the new entry's slug, or why p is dropped. The
// scratch copy is removed before it returns.
func tryOn(root string, m *mapfile.Map, p draft.Proposal, baseline map[string]int, o Options) (string, string) {
	tmp, err := os.MkdirTemp("", "physa-propose-")
	if err != nil {
		return "", "the scratch copy of the map cannot be made: " + err.Error()
	}
	defer os.RemoveAll(tmp)
	if err := copyMap(root, tmp); err != nil {
		return "", "the scratch copy of the map cannot be made: " + err.Error()
	}
	return applyTo(tmp, root, m, p, baseline, o)
}

// ApplyAll applies each of kept, in order, to the map under root itself and
// returns those applied and those dropped. After each one it runs the map
// check against the problems the map had before that proposal; one that adds
// a problem, or does not read back as exactly one new entry citing its
// changes, is undone and dropped with its reason. It writes only the map's
// files under root's .physa/.
func ApplyAll(root string, b draft.Batch, kept []Kept, o Options) ([]Kept, []Dropped) {
	var (
		applied []Kept
		dropped []Dropped
	)
	co := once(o)
	for _, k := range kept {
		slug, reason := applyOne(root, k.Proposal, co)
		if reason != "" {
			dropped = append(dropped, Dropped{Proposal: k.Proposal, Reason: reason})
			continue
		}
		applied = append(applied, Kept{Proposal: k.Proposal, Slug: slug})
	}
	return applied, dropped
}

// applyOne applies p to the map under root, restoring its file when p is
// dropped.
func applyOne(root string, p draft.Proposal, o Options) (string, string) {
	m, _, err := mapfile.Read(root)
	if err != nil {
		return "", "the map cannot be read: " + err.Error()
	}
	before := capability(m, p.Capability)
	if before == nil {
		return "", fmt.Sprintf("the map has no capability `%s`", p.Capability)
	}
	base, err := mapcheck.CheckWith(root, mapcheck.Options{OpenRepo: o.OpenRepo, PRExists: o.PRExists})
	if err != nil {
		return "", "the map cannot be checked: " + err.Error()
	}
	file := filepath.Join(root, filepath.FromSlash(before.Path))
	orig, err := os.ReadFile(file)
	if err != nil {
		return "", "the map cannot be read: " + err.Error()
	}
	slug, reason := applyTo(root, root, m, p, problemSet(base.Problems), o)
	if reason != "" {
		if err := os.WriteFile(file, orig, 0o644); err != nil {
			reason += "; and the file cannot be restored: " + err.Error()
		}
	}
	return slug, reason
}

// applyTo edits the map under dir with p, checks it, with document anchors
// resolved in docRoot, and reads it back. It returns the new entry's slug,
// or why p is dropped; the edit stays in dir either way.
func applyTo(dir, docRoot string, m *mapfile.Map, p draft.Proposal, baseline map[string]int, o Options) (string, string) {
	root, tmp := docRoot, dir
	before := capability(m, p.Capability)
	entry := strings.TrimSpace(strings.ReplaceAll(p.Entry, "\r\n", "\n"))
	file := filepath.Join(tmp, filepath.FromSlash(before.Path))
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", "the scratch copy of the map cannot be made: " + err.Error()
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"), "\n")
	var first, last int // 1-based lines of the new entry in the edited file
	if p.Kind == KindPromise {
		if lines, err = setPromise(lines, entry); err != nil {
			return "", err.Error()
		}
	} else {
		lines, first, last = insert(lines, sections[p.Kind], strings.Split(entry, "\n"))
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return "", "the scratch copy of the map cannot be made: " + err.Error()
	}

	opts := mapcheck.Options{DocRoot: root, PRExists: o.PRExists}
	if o.OpenRepo != nil {
		opts.OpenRepo = func(string) (vcs.Repo, error) { return o.OpenRepo(root) }
	}
	res, err := mapcheck.CheckWith(tmp, opts)
	if err != nil {
		return "", "the scratch copy of the map cannot be checked: " + err.Error()
	}
	if np := newProblems(res.Problems, baseline); len(np) > 0 {
		quoted := make([]string, len(np))
		for i, q := range np {
			quoted[i] = q.String()
		}
		return "", "the map check finds: " + strings.Join(quoted, "; ")
	}
	am, _, err := mapfile.Read(tmp)
	if err != nil {
		return "", "the scratch copy of the map cannot be read: " + err.Error()
	}
	after := capability(am, p.Capability)
	if after == nil {
		return "", fmt.Sprintf("the map no longer has the capability `%s` once it is applied", p.Capability)
	}
	if p.Kind == KindPromise {
		if after.Promise != entry || len(after.Entries) != len(before.Entries) {
			return "", "the promise does not read back as written"
		}
		return "", ""
	}
	kind := entryKinds[p.Kind]
	var added []mapfile.Entry
	for _, e := range after.Entries {
		if e.Line >= first && e.Line <= last {
			added = append(added, e)
		}
	}
	if count(after, kind) != count(before, kind)+1 || len(after.Entries) != len(before.Entries)+1 ||
		len(added) != 1 || added[0].Kind != kind {
		return "", fmt.Sprintf("it does not add exactly one %s to `%s`", p.Kind, p.Capability)
	}
	if !cites(added[0], p.Changes) {
		return "", fmt.Sprintf("its anchors cite none of its changes (%s)", strings.Join(p.Changes, ", "))
	}
	return added[0].Slug, ""
}

// copyMap copies the regular files of root's map into the same place under
// dst, leaving out anything else (links, devices).
func copyMap(root, dst string) error {
	src := filepath.Join(root, mapDir)
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, mapDir, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(to, 0o755)
		case d.Type().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(to, b, 0o644)
		}
		return nil
	})
}

// sectionOrder is the order of a map file's sections.
var sectionOrder = []string{"Rules", "Decisions", "Questions"}

// insert puts entry at the end of the section named section, creating the
// section in the map's order when the file lacks it. It returns the new
// lines and the first and last line (1-based) the entry now takes.
func insert(lines []string, section string, entry []string) ([]string, int, int) {
	start, end, later := -1, len(lines), -1
	rank := func(name string) int {
		for i, s := range sectionOrder {
			if s == name {
				return i
			}
		}
		return -1
	}
	want := rank(section)
	fenced := false
	for i := frontMatterEnd(lines); i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced || !(strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ")) {
			continue
		}
		if start >= 0 && end == len(lines) {
			end = i
		}
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		name := strings.TrimSpace(line[3:])
		if name == section && start < 0 {
			start = i
		}
		if r := rank(name); r > want && later < 0 && start < 0 {
			later = i
		}
	}
	var out []string
	switch {
	case start >= 0:
		k := end
		for k > start+1 && strings.TrimSpace(lines[k-1]) == "" {
			k--
		}
		out = append(out, lines[:k]...)
		out = append(out, "")
		first := len(out) + 1
		out = append(out, entry...)
		if end < len(lines) {
			out = append(out, "")
		}
		out = append(out, lines[end:]...)
		return out, first, first + len(entry) - 1
	case later >= 0:
		out = append(out, lines[:later]...)
		out = append(out, "## "+section, "")
		first := len(out) + 1
		out = append(out, entry...)
		out = append(out, "")
		out = append(out, lines[later:]...)
		return out, first, first + len(entry) - 1
	}
	k := len(lines)
	for k > 0 && strings.TrimSpace(lines[k-1]) == "" {
		k--
	}
	out = append(out, lines[:k]...)
	out = append(out, "", "## "+section, "")
	first := len(out) + 1
	out = append(out, entry...)
	return out, first, first + len(entry) - 1
}

// frontMatterEnd returns the index of the first line after the leading ---
// block, or 0 when there is none.
func frontMatterEnd(lines []string) int {
	if len(lines) == 0 || lines[0] != "---" {
		return 0
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			return i + 1
		}
	}
	return 0
}

// setPromise replaces the promise: value of the front matter with promise,
// adding the key when the front matter lacks it.
func setPromise(lines []string, promise string) ([]string, error) {
	end := frontMatterEnd(lines)
	if end == 0 {
		return nil, errors.New("the capability file has no front matter to hold a promise")
	}
	out := append([]string(nil), lines...)
	for i := 1; i < end-1; i++ {
		if key, _, ok := strings.Cut(out[i], ":"); ok && strings.TrimSpace(key) == "promise" {
			out[i] = "promise: " + promise
			return out, nil
		}
	}
	return append(out[:1], append([]string{"promise: " + promise}, out[1:]...)...), nil
}

// count is how many entries of kind f holds.
func count(f *mapfile.File, kind mapfile.Kind) int {
	n := 0
	for _, e := range f.Entries {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// cites reports whether one of e's commit anchors names one of changes,
// either id possibly abbreviated.
func cites(e mapfile.Entry, changes []string) bool {
	for _, a := range e.Anchors {
		if a.Kind != mapfile.Commit {
			continue
		}
		for _, c := range changes {
			x, y := strings.ToLower(a.Value), strings.ToLower(strings.TrimSpace(c))
			if y != "" && (strings.HasPrefix(x, y) || strings.HasPrefix(y, x)) {
				return true
			}
		}
	}
	return false
}
