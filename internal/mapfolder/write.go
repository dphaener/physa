package mapfolder

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Products are where a map's product is, in the old layout and in folders;
// either means a map.
var Products = []string{Dir + "/product.md", ProductDir + "/" + IndexFile}

// Unsafe is an entry SafeFiles took out of a folder, with why.
type Unsafe struct {
	Kind            Kind
	Heading, Reason string
}

// SafeFiles is a folder's files by name, as Split gives them, with each
// entry taken out whose heading the map check would reject: an empty one,
// one HeadingProblem names, and one with the anchor of the title, a section
// or an earlier heading of its file. A kind file left with no entries goes,
// and index.md's links follow. Every other byte is kept.
func SafeFiles(files map[string]string) (map[string]string, []Unsafe) {
	out := maps.Clone(files)
	var drops []Unsafe
	var kinds []Kind
	for _, k := range Kinds {
		src := lines(files[k.File()])
		if at := slices.Index(src, "## Entries"); at >= 0 {
			var kept [][]line
			kept, drops = writeKeep(k, src, at, drops)
			if delete(out, k.File()); len(kept) == 0 {
				continue
			}
			out[k.File()] = renderKind(line{text: src[0]}, kept).String()
		} else if _, ok := files[k.File()]; !ok {
			continue
		}
		kinds = append(kinds, k)
	}
	if ix, err := FormatIndex(files[IndexFile], kinds); err == nil && len(drops) > 0 {
		out[IndexFile] = ix
	}
	return out, drops
}

// writeKeep is the entries after line at of kind file src that the check
// accepts; the others it adds to drops.
func writeKeep(k Kind, src []string, at int, drops []Unsafe) ([][]line, []Unsafe) {
	var sc scanner
	for _, s := range src[at+1:] {
		sc.take(line{text: s})
	}
	seen := map[string]string{}
	for _, h := range []string{strings.TrimPrefix(src[0], "# "), "Contents", "Entries"} {
		seen[Anchor(h)] = h
	}
	var kept [][]line
	for _, e := range sc.flush() {
		h := strings.TrimSpace(strings.TrimLeft(e[0].text, "#"))
		reason := HeadingProblem(h)
		switch first, dup := seen[Anchor(h)]; {
		case h == "":
			reason = "an entry needs a heading"
		case dup:
			reason = fmt.Sprintf("its heading is the same as, or has the anchor of, %q in its file", first)
		}
		if reason != "" {
			drops = append(drops, Unsafe{k, h, reason})
			continue
		}
		seen[Anchor(h)] = h
		kept = append(kept, e)
	}
	return kept, drops
}

// SafeHeading is s without the characters HeadingProblem rejects, its words
// one space apart.
func SafeHeading(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("[", "", "]", "", "<", "", ">", "", "*", "").Replace(s)), " ")
}
