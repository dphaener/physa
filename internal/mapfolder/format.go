package mapfolder

import (
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
)

// Problem is a file FormatMap left unchanged, with the line that stopped it.
type Problem struct {
	Path    string // slash-separated, relative to the root
	Line    int
	Message string
}

func (p Problem) String() string { return fmt.Sprintf("%s:%d: %s", p.Path, p.Line, p.Message) }

// ErrOldLayout says a map is still in the single-file layout, naming the
// first old file found.
type ErrOldLayout struct {
	Path string // slash-separated, relative to the root
}

func (e ErrOldLayout) Error() string {
	return e.Path + ": the map is in the old single-file layout; convert the product and each capability to a folder (docs/architecture/09-capability-folders.md)"
}

// Format rebuilds the folder file named file and normalises its spacing: a
// kind file's contents are regenerated from its entries, and index.md keeps
// its links (FormatIndex sets them). Every byte of an entry, from its heading
// to its last non-blank line, is kept. A file outside its format is a
// *FormatError.
func Format(file, text string) (string, error) {
	name := path.Base(file)
	if k, ok := KindOfFile(name); ok {
		return formatKind(k, text)
	}
	if name != IndexFile {
		return "", fmt.Errorf("%s is not a file of a capability's folder", file)
	}
	ix, err := parseIndex(text)
	if err != nil {
		return "", err
	}
	return renderIndex(ix.fm, ix.title, ix.overview, ix.kinds).String(), nil
}

// FormatIndex formats index.md with a link to each of kinds' files.
func FormatIndex(text string, kinds []Kind) (string, error) {
	ix, err := parseIndex(text)
	if err != nil {
		return "", err
	}
	return renderIndex(ix.fm, ix.title, ix.overview, kinds).String(), nil
}

// formatKind reads a kind file's title and entries and renders it again
// with fresh contents.
func formatKind(k Kind, text string) (string, error) {
	ls := lines(text)
	i := 0
	for i < len(ls)-1 && blank(ls[i]) {
		i++
	}
	if !strings.HasPrefix(ls[i], "# ") {
		return "", lineErr(i, "want the title, a line starting with \"# \", first")
	}
	title := line{text: ls[i]}
	const beforeContents, inContents, inEntries = 0, 1, 2
	stage := beforeContents
	var sc scanner
	for i++; i < len(ls); i++ {
		s := ls[i]
		if stage == inEntries && sc.take(line{text: s}) {
			continue
		}
		switch {
		case blank(s):
		case stage == beforeContents && strings.TrimSpace(s) == "## Contents":
			stage = inContents
		case stage != inEntries && strings.TrimSpace(s) == "## Entries":
			stage = inEntries
		case section(s):
			return "", lineErr(i, "unexpected heading %q: a %s file has its title, ## Contents and ## Entries only", strings.TrimSpace(s), k)
		case stage == inContents && strings.HasPrefix(s, "- "):
		case stage == inEntries:
			return "", lineErr(i, "text under ## Entries before the first entry")
		case stage == inContents:
			return "", lineErr(i, "text in ## Contents that is not a contents line")
		default:
			return "", lineErr(i, "text between the title and ## Contents")
		}
	}
	if stage != inEntries {
		return "", lineErr(len(ls)-1, "missing ## Entries")
	}
	return renderKind(title, sc.flush()).String(), nil
}

// index is index.md, or the head of a single text, taken apart.
type index struct {
	fm       []line
	title    line
	overview []line
	kinds    []Kind
}

// head reads front matter, the title and at most one paragraph, up to the
// first line stop accepts, and returns that line's index.
func head(src []string, stop func(string) bool) (index, int, error) {
	fm, end, err := frontMatter(src)
	if err != nil {
		return index{}, 0, err
	}
	ix, gap, i := index{fm: fm}, false, end+1
	for ; i < len(src); i++ {
		s := src[i]
		switch {
		case blank(s):
			gap = gap || len(ix.overview) > 0
		case ix.title.text == "":
			if !strings.HasPrefix(s, "# ") {
				return ix, i, lineErr(i, "want the title, a line starting with \"# \", after the front matter")
			}
			ix.title = line{text: s, src: i + 1}
		case stop(s):
			return ix, i, nil
		case strings.HasPrefix(s, "#"):
			return ix, i, lineErr(i, "unexpected heading %q: want one title, then the overview", strings.TrimSpace(s))
		case gap:
			return ix, i, lineErr(i, "a second paragraph: the overview is one paragraph")
		default:
			ix.overview = append(ix.overview, line{text: s, src: i + 1})
		}
	}
	if ix.title.text == "" {
		return ix, i, lineErr(len(src)-1, "missing title: want a line starting with \"# \"")
	}
	return ix, i, nil
}

// parseIndex reads index.md's one grammar: front matter, the title, one
// overview paragraph, then links to kind files, and nothing else.
func parseIndex(text string) (index, error) {
	ls := lines(text)
	ix, i, err := head(ls, func(s string) bool { return strings.HasPrefix(s, "- ") })
	if err != nil {
		return ix, err
	}
	if len(ix.overview) == 0 {
		return ix, lineErr(min(i, len(ls)-1), "missing overview: want one paragraph after the title")
	}
	for ; i < len(ls); i++ {
		if blank(ls[i]) {
			continue
		}
		k := slices.IndexFunc(Kinds, func(k Kind) bool { return strings.TrimSpace(ls[i]) == IndexLink(k) })
		if k < 0 {
			return ix, lineErr(i, "%q after the links: index.md ends with links to rules.md, decisions.md and questions.md", strings.TrimSpace(ls[i]))
		}
		ix.kinds = append(ix.kinds, Kinds[k])
	}
	return ix, nil
}

// FormatMap formats every file in the map's folders under root: the product
// and each capability, with index links to the kind files each folder has.
// It returns the slash-separated paths whose text would change, and writes
// them only when write is true; it never creates or deletes a file. A file
// outside its format is left as it is and reported as a Problem, and the
// others are still formatted. A map still in the single-file layout is an
// ErrOldLayout, found before anything is formatted.
func FormatMap(root string, write bool) (changed []string, problems []Problem, err error) {
	dirs, err := folders(root)
	if err != nil {
		return nil, nil, err
	}
	for _, dir := range dirs {
		var kinds []Kind
		names := []string{IndexFile}
		for _, k := range Kinds {
			if info, err := os.Stat(path.Join(root, dir, k.File())); err == nil && info.Mode().IsRegular() {
				kinds, names = append(kinds, k), append(names, k.File())
			}
		}
		for _, name := range names {
			p := dir + "/" + name
			b, err := os.ReadFile(path.Join(root, p))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return changed, problems, err
			}
			var out string
			if name == IndexFile {
				out, err = FormatIndex(string(b), kinds)
			} else {
				out, err = Format(name, string(b))
			}
			var fe *FormatError
			if errors.As(err, &fe) {
				problems = append(problems, Problem{Path: p, Line: fe.Line, Message: fe.Message})
				continue
			}
			if err != nil || out == string(b) {
				continue
			}
			changed = append(changed, p)
			if write {
				if err := os.WriteFile(path.Join(root, p), []byte(out), 0o644); err != nil {
					return changed, problems, err
				}
			}
		}
	}
	return changed, problems, nil
}

// folders lists the map's folders under root, the product first, once no
// file of the single-file layout is left.
func folders(root string) ([]string, error) {
	if _, err := os.Stat(path.Join(root, Dir)); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path.Join(root, Dir, "product.md")); err == nil {
		return nil, ErrOldLayout{Path: Dir + "/product.md"}
	}
	ents, err := os.ReadDir(path.Join(root, CapabilitiesDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var out []string
	if info, err := os.Stat(path.Join(root, ProductDir)); err == nil && info.IsDir() {
		out = append(out, ProductDir)
	}
	for _, e := range ents {
		switch {
		case !e.IsDir() && strings.HasSuffix(e.Name(), ".md"):
			return nil, ErrOldLayout{Path: CapabilitiesDir + "/" + e.Name()}
		case e.IsDir():
			out = append(out, CapabilitiesDir+"/"+e.Name())
		}
	}
	return out, nil
}
