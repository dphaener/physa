package walk

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/sourceset"
)

func fenced(w io.Writer, label, text string) {
	fmt.Fprintf(w, "%s\n\n", label)
	draft.Fence(w, text)
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Message is the walk session's message for area a: its name and promise
// (and the area it was split from), its source files one per line with line
// counts where sizes knows them, and the folders of the repo's test and doc
// files with counts (and its other files), so the session can Glob them. Everything of the repo's
// sits in a fence, as data.
func Message(a Area, split sourceset.Division, sizes map[string]int) string {
	var w strings.Builder
	fmt.Fprintf(&w, "## The area `%s`\n\n%s\n\n", draft.OneLine(a.Name), draft.OneLine(a.Promise))
	if a.Parent != "" {
		fmt.Fprintf(&w, "It is a part of the area `%s`, split by a session before this one. Walk this part only.\n\n", draft.OneLine(a.Parent))
	}
	rows := make([]string, len(a.Files))
	for i, f := range a.Files {
		rows[i] = draft.OneLine(f)
		if n, ok := sizes[f]; ok {
			rows[i] += fmt.Sprintf(" (%d lines)", n)
		}
	}
	fenced(&w, "### The area's source files\n\nOne file per line. Every one is yours to place.", strings.Join(rows, "\n"))
	for _, k := range []struct {
		kind  sourceset.Kind
		label string
	}{
		{sourceset.Test, "### The repo's test files\n\nFolders with their file counts and extensions; Glob them."},
		{sourceset.Doc, "### The repo's doc files\n\nFolders with their file counts and extensions; Glob them."},
		{sourceset.Other, "### The repo's other files\n\nConfiguration, fixtures, assets and the like: folders with their file counts and extensions; Glob them."},
	} {
		var files []sourceset.File
		for _, p := range split.Files[k.kind] {
			files = append(files, sourceset.File{Path: p})
		}
		w.WriteString("\n")
		if len(files) == 0 {
			w.WriteString(k.label[:strings.Index(k.label, "\n")] + "\n\nnone\n")
			continue
		}
		fenced(&w, k.label, sourceset.Kinds(files))
	}
	w.WriteString("\nWalk the area, then answer as the skill says: the capabilities it keeps, each with its footprint, and the files that are shared.\n")
	return w.String()
}

func list(w *strings.Builder, label string, items []string) {
	if len(items) > 0 {
		fenced(w, "## "+label, strings.Join(items, "\n"))
		w.WriteString("\n")
	}
}

// FixPrompt is the message that sends back everything a check found wrong,
// grouped, and asks for an answer holding only the changes.
func FixPrompt(p Problems) string {
	var w strings.Builder
	w.WriteString("Your answer needs changes. Fix each problem below.\n\n")
	list(&w, "Source files in no capability and not shared", p.Unclaimed)
	var twice []string
	for _, f := range sortedKeys(p.Twice) {
		twice = append(twice, draft.OneLine(f)+" :: "+draft.OneLine(strings.Join(p.Twice[f], ", ")))
	}
	list(&w, "Source files claimed twice, with the claimants", twice)
	list(&w, "Paths that match no file of the area", p.Outside)
	list(&w, "Tests paths that are not test files", p.NotTest)
	list(&w, "Docs paths that are not doc files", p.NotDoc)
	list(&w, "Other paths that are not other files", p.NotOther)
	list(&w, "Owning no source file", p.NoSource)
	list(&w, "Other faults", p.Names)
	w.WriteString("Answer with exactly one fenced block marked `json` and nothing after it, holding only the changes. ")
	if p.Split {
		w.WriteString("`add` maps a part's name to the exact paths that go to it. Use `{}` when empty.\n\n" +
			"```json\n{\"add\":{\"order-refunds\":[\"path/to/file\"]}}\n```\n")
		return w.String()
	}
	w.WriteString("`add` moves the exact paths to a capability's own sources, taking them from every other claim (a file claimed twice goes to one), " +
		"`shared` lists files to share with a `path` and a `reason`, `new` lists capabilities to add, each with `name`, `promise`, `sources`, `tests`, `docs` and `other`, " +
		"`drop` lists paths to take back from every claim, and `remove` lists capabilities to take out by name, such as one to rename: remove the old name and add it under `new`. Use `[]` or `{}` when empty.\n\n" +
		"```json\n{\"add\":{\"order-refunds\":[\"path/to/file\"]},\"shared\":[{\"path\":\"path/to/glue\",\"reason\":\"One line.\"}],\"new\":[],\"drop\":[\"path/to/wrong\"],\"remove\":[]}\n```\n")
	return w.String()
}
