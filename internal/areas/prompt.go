package areas

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/productpromise"
)

func skill(s string) string { return strings.TrimRight(s, "\n") }

// fenced writes a label of Physa's own, then text in a fence nothing inside
// can close.
func fenced(w io.Writer, label, text string) {
	fmt.Fprintf(w, "%s\n\n", label)
	draft.Fence(w, text)
}

// SplitPrompt returns the split session's system prompt (the sources skill)
// and its message: every folder with its file count and extensions. The
// folders are the repo's, so they sit in a fence; the labels are Physa's.
func SplitPrompt(kinds string) (system, message string) {
	var w strings.Builder
	fenced(&w, "## The repo's folders\n\nEach line is a folder, its file count and the extensions in it.", kinds)
	return skill(Sources), w.String()
}

// AreasPrompt returns the system prompt (the areas skill) and the message of
// one session placing every source file: the whole listing, one line per
// folder with its file names.
func AreasPrompt(tree string) (system, message string) {
	var w strings.Builder
	fenced(&w, "## The source files\n\nEach line is a folder, its counts and its files.", tree)
	return skill(Areas), w.String()
}

// lead is the promise's block and a blank line, or nothing for no promise.
func lead(promise string) string {
	if promise == "" {
		return ""
	}
	return productpromise.Block(promise) + "\n"
}

// PartPrompt returns the system prompt and the message of part i of n. The
// system is the areas skill followed by the parts skill, so it holds
// AcrossParts. The message gives the product's promise an earlier part
// stated (promise, "" for none), every source folder of the repo (overview),
// the areas so far (briefs) and this part's files (partTree).
func PartPrompt(i, n int, promise, overview, briefs, partTree string) (system, message string) {
	var w strings.Builder
	fmt.Fprintf(&w, "## Part %d of %d\n\n%s", i, n, lead(promise))
	fenced(&w, "### Every source folder of the repo\n\nCounts only, so you see the product whole.", overview)
	w.WriteString("\n")
	if strings.TrimSpace(briefs) == "" {
		w.WriteString("### Areas so far\n\nnone\n")
	} else {
		fenced(&w, "### Areas so far", briefs)
	}
	w.WriteString("\n")
	fenced(&w, "### The files of this part", partTree)
	return skill(Areas) + "\n\n" + skill(Parts), w.String()
}

// MergePrompt returns the system prompt (the merge skill) and the message of
// the session that finds areas keeping one family of promises: the product's
// promise as the first part stated it ("" for none), then the briefs.
func MergePrompt(promise, briefs string) (system, message string) {
	var w strings.Builder
	w.WriteString(lead(promise))
	fenced(&w, "## The areas", briefs)
	return skill(Merge), w.String()
}

// FixPrompt is the message that sends back the files that fall in no area
// and the files two areas claim. It asks for an answer holding only those
// files.
func FixPrompt(missing []string, twice map[string][]string) string {
	var w strings.Builder
	w.WriteString("Some source files did not land in exactly one area. Place each file below in exactly one area.\n\n")
	if len(missing) > 0 {
		fenced(&w, fmt.Sprintf("## In no area (%d)", len(missing)), lines(missing, func(p string) string { return draft.OneLine(p) }))
		w.WriteString("\n")
	}
	if len(twice) > 0 {
		paths := make([]string, 0, len(twice))
		for p := range twice {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		fenced(&w, fmt.Sprintf("## In two areas (%d), each with both areas", len(paths)), lines(paths, func(p string) string {
			return draft.OneLine(p) + " :: " + draft.OneLine(strings.Join(twice[p], ", "))
		}))
		w.WriteString("\n")
	}
	w.WriteString("Answer with exactly one fenced block marked `json` and nothing after it, holding only these files: " +
		"`add` maps an area's name to the exact paths that go to it, and `new_areas` lists any area you need to add, each with `name`, `promise` and `sources`. Use `[]` or `{}` when empty.\n\n" +
		"```json\n{\"add\":{\"checkout\":[\"path/to/file\"]},\"new_areas\":[{\"name\":\"notifications\",\"promise\":\"One sentence on one line.\",\"sources\":[\"path/to/other-file\"]}]}\n```\n")
	return w.String()
}

func lines(items []string, f func(string) string) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = f(s)
	}
	return strings.Join(out, "\n")
}
