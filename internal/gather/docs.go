package gather

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dphaener/physa/internal/store"
)

// The kinds of doc.
const (
	KindAgent        = "agent"
	KindContributing = "contributing"
	KindReadme       = "readme"
	KindDecisions    = "decisions"
	KindOverview     = "overview"
)

// MaxDocBytes is the largest doc gathered; a larger file is skipped.
const MaxDocBytes = 2 << 20

// Skip is a source left out and why. Path is relative to the repo root.
type Skip struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

var ignoredDirs = map[string]bool{"vendor": true, "node_modules": true, "tmp": true, "log": true}

var numbered = regexp.MustCompile(`^[0-9]+[-_]`)

// Docs reads the repo's docs from the working tree under root: agent docs
// (including CONTEXT.md), contributing guides, readmes, decisions with
// decision-record folders, and the plan and overview files (PLAN, ROADMAP,
// OVERVIEW, ARCHITECTURE, CONTEXT, any case) directly under docs/.
// Each doc's path is relative to root with forward slashes, sorted, and its
// Reason says by which rule it was found. A file over MaxDocBytes is left out
// and returned in skipped with its size; a candidate that is missing or not a
// regular file is a guess and is left out silently.
func Docs(root string) (docs []store.Doc, skipped []Skip, err error) {
	seen := map[string]bool{}
	add := func(rel, kind, reason string) error {
		if seen[rel] {
			return nil
		}
		seen[rel] = true
		full := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > MaxDocBytes {
			skipped = append(skipped, Skip{rel, fmt.Sprintf("over %d MB (%d bytes)", MaxDocBytes>>20, info.Size())})
			return nil
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		docs = append(docs, store.Doc{Path: rel, Kind: kind, Reason: reason, Body: string(b)})
		return nil
	}
	const (
		agentWhy    = "an agent instructions file"
		guideWhy    = "the contributing guide"
		readmeWhy   = "the README"
		decideWhy   = "a decisions file"
		folderWhy   = "a decision-record folder"
		overviewWhy = "a plan or overview file under docs/"
		cursorWhy   = "Cursor rules"
	)
	type find struct{ kind, name, reason string }
	fixed := []find{
		{KindAgent, "AGENTS.md", agentWhy}, {KindAgent, "CONTEXT.md", agentWhy}, {KindAgent, "CLAUDE.md", agentWhy},
		{KindAgent, ".claude/CLAUDE.md", agentWhy}, {KindAgent, ".cursorrules", cursorWhy},
		{KindContributing, "CONTRIBUTING.md", guideWhy}, {KindContributing, "CONTRIBUTING", guideWhy},
		{KindContributing, "docs/CONTRIBUTING.md", guideWhy}, {KindContributing, ".github/CONTRIBUTING.md", guideWhy},
		{KindReadme, "README.md", readmeWhy}, {KindReadme, "README", readmeWhy}, {KindReadme, "README.rst", readmeWhy}, {KindReadme, "README.txt", readmeWhy},
	}
	for _, base := range []string{".", "docs", "doc"} {
		for _, n := range []string{"decisions.md", "DECISIONS.md"} {
			fixed = append(fixed, find{KindDecisions, path.Join(base, n), decideWhy})
		}
	}
	for _, f := range fixed {
		if err := add(f.name, f.kind, f.reason); err != nil {
			return nil, nil, err
		}
	}
	top, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	for _, e := range top {
		name := e.Name()
		if e.IsDir() || !strings.EqualFold(path.Ext(name), ".md") {
			continue
		}
		kind, why := KindOverview, overviewWhy
		switch strings.ToUpper(strings.TrimSuffix(name, path.Ext(name))) {
		case "CONTEXT":
			kind, why = KindAgent, agentWhy
		case "PLAN", "ROADMAP", "OVERVIEW", "ARCHITECTURE":
		default:
			continue
		}
		if err := add(path.Join("docs", name), kind, why); err != nil {
			return nil, nil, err
		}
	}
	rules := filepath.Join(root, ".cursor", "rules")
	err = filepath.WalkDir(rules, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			if p == rules {
				return nil // no rules folder
			}
			return werr
		}
		if d.Type().IsRegular() && (strings.HasSuffix(p, ".mdc") || strings.HasSuffix(p, ".md")) {
			rel, _ := filepath.Rel(root, p)
			return add(filepath.ToSlash(rel), KindAgent, cursorWhy)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for _, base := range []string{".", "docs", "doc"} {
		folders, err := recordFolders(root, base, 1)
		if err != nil {
			return nil, nil, err
		}
		for _, dir := range folders {
			entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
			if err != nil {
				return nil, nil, err
			}
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".md") {
					if err := add(path.Join(dir, e.Name()), KindDecisions, folderWhy); err != nil {
						return nil, nil, err
					}
				}
			}
		}
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return docs, skipped, nil
}

// recordFolders returns the decision-record folders below dir (relative to
// root), down to three levels, skipping dot-directories and the ignored ones.
func recordFolders(root, dir string, depth int) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || ignoredDirs[name] {
			continue
		}
		rel := path.Join(dir, name)
		ok, err := isRecordFolder(root, rel)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, rel)
		}
		if depth < 3 {
			sub, err := recordFolders(root, rel, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		}
	}
	return out, nil
}

// isRecordFolder reports whether the folder is named decisions or
// decision-records, or holds two or more markdown files numbered like
// 0001-name.md.
func isRecordFolder(root, rel string) (bool, error) {
	if b := path.Base(rel); b == "decisions" || b == "decision-records" {
		return true, nil
	}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false, err
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") && numbered.MatchString(e.Name()) {
			n++
		}
	}
	return n >= 2, nil
}
