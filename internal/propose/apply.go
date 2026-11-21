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
	"github.com/dphaener/physa/internal/mapfolder"
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

// applyOne applies p to the map under root. When p is dropped, every file
// of its capability's folder is put back as it was, and a kind file p made
// is removed.
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
	dir := filepath.Join(root, filepath.FromSlash(before.Path))
	snap, err := mapfolder.Take(dir)
	if err != nil {
		return "", "the map cannot be read: " + err.Error()
	}
	slug, reason := applyTo(root, root, m, p, problemSet(base.Problems), o)
	if reason != "" {
		if err := snap.Restore(dir); err != nil {
			reason += "; and the folder cannot be restored: " + err.Error()
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
	folder := filepath.Join(tmp, filepath.FromSlash(before.Path))
	var (
		file        string // the entry's file, slash-separated, relative to the root
		first, last int    // 1-based lines of the new entry in file
	)
	if p.Kind == KindPromise {
		if reason := editPromise(filepath.Join(folder, mapfolder.IndexFile), entry); reason != "" {
			return "", reason
		}
	} else {
		k := entryKinds[p.Kind]
		file = before.Path + "/" + k.File()
		var err error
		if first, last, err = mapfolder.AddEntry(folder, before.Title, k, entry); err != nil {
			return "", "it cannot be written into " + before.Path + "/" + err.Error()
		}
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
		if e.Path == file && e.Line >= first && e.Line <= last {
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

// editPromise sets the promise in the index.md at file, or says why it
// cannot.
func editPromise(file, promise string) string {
	raw, err := os.ReadFile(file)
	if err != nil {
		return "the map cannot be read: " + err.Error()
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"), "\n")
	if lines, err = setPromise(lines, promise); err != nil {
		return err.Error()
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return "the map cannot be written: " + err.Error()
	}
	return ""
}

// setPromise replaces the promise: value of the front matter with promise,
// adding the key when the front matter lacks it.
func setPromise(lines []string, promise string) ([]string, error) {
	end := frontMatterEnd(lines)
	if end == 0 {
		return nil, errors.New("the capability's index.md has no front matter to hold a promise")
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
