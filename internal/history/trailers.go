package history

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/dphaener/physa/internal/vcs"
)

// Kinds of finding Check reports.
const (
	KindMissing  = "missing"  // the commit has no Capability trailer
	KindMultiple = "multiple" // the commit names more than one capability
	KindUnknown  = "unknown"  // a trailer names a slug the map doesn't have
)

// Finding is one problem with a commit's trailers.
type Finding struct {
	Commit  string `json:"commit"` // the commit's short hash
	Kind    string `json:"kind"`
	Slug    string `json:"slug,omitempty"`
	Message string `json:"message"`
}

var trailerLine = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*:\s*\S`)

// readTrailers returns the capabilities of the Capability trailers in a
// commit message; every other trailer is ignored.
func readTrailers(message string) []string {
	return Trailers(message, "Capability")
}

// Trailers returns the comma-split values of every trailer in a commit
// message whose key equals key, case-insensitively, in the order written. It
// reads trailers only, never the body: the subject paragraph never holds
// trailers; from the last paragraph back, every paragraph made only of
// `Key: value` lines does, so a squash merge's own trailers after a blank
// line read like the branch commit's. A line that continues a trailer joins
// its value.
func Trailers(message, key string) []string {
	values := []string{}
	paras := paragraphs(message)
	first := len(paras)
	for first > 1 && isTrailerBlock(paras[first-1]) {
		first--
	}
	for _, para := range paras[first:] {
		for _, t := range joinContinuations(para) {
			k, value, _ := strings.Cut(t, ":")
			if strings.EqualFold(k, key) {
				values = append(values, splitSlugs(value)...)
			}
		}
	}
	return values
}

// Sessions returns the values of the Session trailers of a commit message,
// whether written as `Session: a, b` or on several lines, without
// duplicates, in the order written.
func Sessions(message string) []string {
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range Trailers(message, "Session") {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// paragraphs splits a message into runs of lines divided by blank lines.
func paragraphs(message string) [][]string {
	var paras [][]string
	var cur []string
	for _, line := range strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			if cur != nil {
				paras = append(paras, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if cur != nil {
		paras = append(paras, cur)
	}
	return paras
}

// isTrailerBlock reports whether every line of the paragraph is a trailer
// line or continues the one above.
func isTrailerBlock(para []string) bool {
	for i, line := range para {
		if line[0] == ' ' || line[0] == '\t' {
			if i == 0 {
				return false
			}
			continue
		}
		if !trailerLine.MatchString(line) {
			return false
		}
	}
	return true
}

// joinContinuations folds each continuation line into the trailer above it.
func joinContinuations(para []string) []string {
	var out []string
	for _, line := range para {
		if line[0] == ' ' || line[0] == '\t' {
			out[len(out)-1] += " " + strings.TrimSpace(line)
			continue
		}
		out = append(out, line)
	}
	return out
}

// splitSlugs splits a comma-separated value, trimming and dropping empties.
func splitSlugs(value string) []string {
	var slugs []string
	for _, s := range strings.Split(value, ",") {
		if s = strings.TrimSpace(s); s != "" {
			slugs = append(slugs, s)
		}
	}
	return slugs
}

// Check reports the trailer findings of commits. A change made before the
// map serves no capability yet, so a change whose parent leaves no map (no
// .physa/product/ folder and no old .physa/product.md, or no parent at all)
// gets no finding, and that covers the change that adds the map. A slug is
// known when its capability is in the map as that commit leaves it: its
// folder under .physa/capabilities/ holds a file, or, at a commit whose map
// has no .physa/product/ folder, an old <slug>.md file does. A commit that
// adds a capability may name it, and a slug only a later commit adds is
// unknown. A trailer value that is not a slug names nothing and is unknown.
// Findings follow commit order; for each commit, missing or multiple come
// before unknown slugs, each in the order written. The error is for a commit
// whose files version control cannot list.
func Check(repo vcs.Repo, commits []Commit) ([]Finding, error) {
	findings := []Finding{}
	for _, c := range commits {
		before, err := beforeTheMap(repo, c.Hash)
		if err != nil {
			return nil, err
		}
		if before {
			continue
		}
		add := func(kind, slug, format string, args ...any) {
			findings = append(findings, Finding{Commit: c.Short, Kind: kind, Slug: slug, Message: fmt.Sprintf(format, args...)})
		}
		switch {
		case len(c.Capabilities) == 0:
			add(KindMissing, "", "no Capability trailer")
		case len(c.Capabilities) > 1:
			add(KindMultiple, "", "Capability names %d capabilities (%s); a commit serves one", len(c.Capabilities), strings.Join(c.Capabilities, ", "))
		}
		if len(c.Capabilities) == 0 {
			continue
		}
		known, err := slugsAt(repo, c.Hash, c.Capabilities)
		if err != nil {
			return nil, err
		}
		for _, s := range c.Capabilities {
			if !known[s] {
				add(KindUnknown, s, "Capability %q is not in the map", s)
			}
		}
	}
	return findings, nil
}

// Where the map keeps its parts, relative to the repo's working directory,
// and the file of the old single-file layout that marks a map in it.
const (
	mapDir     = ".physa"
	productDir = ".physa/product"
	oldProduct = "product.md"
)

// beforeTheMap reports whether the parent of change id leaves no map: neither
// the folder layout (a file under .physa/product/) nor the old one
// (.physa/product.md). A root change has no parent, so it is before the map.
func beforeTheMap(repo vcs.Repo, id string) (bool, error) {
	parent, ok := repo.Resolve(id + "^")
	if !ok {
		if _, found := repo.Resolve(id); found {
			return true, nil
		}
		parent = id // not a change: list its files for the adapter's error
	}
	// Files lists only the files directly under a directory, never its
	// subdirectories, so each layout's marker needs its own listing.
	files, err := repo.Files(parent, mapDir)
	if err != nil {
		return false, fmt.Errorf("cannot list the map at %s: %w", id, err)
	}
	for _, f := range files {
		if path.Base(f) == oldProduct {
			return false, nil
		}
	}
	product, err := repo.Files(parent, productDir)
	if err != nil {
		return false, fmt.Errorf("cannot list the map at %s: %w", id, err)
	}
	return len(product) == 0, nil
}

// validSlug reports whether s can name a capability: lowercase letters,
// digits and hyphens only, so it never holds a path separator or "..".
var validSlug = regexp.MustCompile(`^[a-z0-9-]+$`)

// slugsAt returns which of the named slugs are capabilities in the map as
// change id leaves it. Files lists only the files directly under a
// directory, so a capability's folder is found by listing that folder, once
// for each valid slug named; the old <slug>.md listing is read only when a
// slug is still unknown and the map has no .physa/product/ folder.
func slugsAt(repo vcs.Repo, id string, named []string) (map[string]bool, error) {
	known := map[string]bool{}
	var pending []string
	for _, s := range named {
		if !validSlug.MatchString(s) || known[s] {
			continue
		}
		files, err := repo.Files(id, CapabilitiesDir+"/"+s)
		if err != nil {
			return nil, fmt.Errorf("cannot list the map's capabilities at %s: %w", id, err)
		}
		if len(files) > 0 {
			known[s] = true
		} else {
			pending = append(pending, s)
		}
	}
	if len(pending) == 0 {
		return known, nil
	}
	product, err := repo.Files(id, productDir)
	if err != nil {
		return nil, fmt.Errorf("cannot list the map's capabilities at %s: %w", id, err)
	}
	if len(product) > 0 {
		return known, nil
	}
	old, err := repo.Files(id, CapabilitiesDir)
	if err != nil {
		return nil, fmt.Errorf("cannot list the map's capabilities at %s: %w", id, err)
	}
	for _, f := range old {
		if stem, ok := strings.CutSuffix(path.Base(f), ".md"); ok {
			known[stem] = true
		}
	}
	return known, nil
}

// TrailerBlock returns every key and value of the message's trailers, in the
// order written, read the way Trailers reads them: from the last paragraph
// back, every paragraph made only of `Key: value` lines. Values are kept
// whole, never split on commas; a continuation line joins its value. A
// message with no trailers gives an empty list.
func TrailerBlock(message string) [][2]string {
	out := [][2]string{}
	paras := paragraphs(message)
	first := len(paras)
	for first > 1 && isTrailerBlock(paras[first-1]) {
		first--
	}
	for _, para := range paras[first:] {
		for _, t := range joinContinuations(para) {
			k, v, _ := strings.Cut(t, ":")
			out = append(out, [2]string{k, strings.TrimSpace(v)})
		}
	}
	return out
}
