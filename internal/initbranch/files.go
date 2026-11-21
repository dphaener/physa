package initbranch

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/mapfolder"
	"github.com/dphaener/physa/internal/recovering"
)

// The files init writes, relative to the repo root.
const (
	readmePath   = ".physa/README.md"
	glossaryPath = recovering.GlossaryPath
	agentsPath   = "AGENTS.md"
)

// statusFailed is the status the store keeps for a capability whose
// recovery failed.
const statusFailed = "failed"

// The AGENTS.md pointer's markers and the block between them (design 01
// section 7).
const (
	startMarker = "<!-- physa -->"
	endMarker   = "<!-- /physa -->"
	pointer     = startMarker + "\n" +
		"Before changing code, run `physa map index`, then `physa map slice <capabilities>` for the ones your task touches.\n" +
		"No CLI? Read .physa/README.md.\n" +
		endMarker
)

// questionBody is the body of a product-wide question.
const questionBody = "Raised while proposing the map; the sources suggest it and leave it undecided."

var (
	// slugLine is a line starting with a slug in backticks, as a meta line
	// does; a slug it names is taken.
	slugLine = regexp.MustCompile("(?m)^`([a-z0-9]+(?:-[a-z0-9]+)*)`")
	// anchorRef is a PR or change a question may be anchored to.
	anchorRef = regexp.MustCompile(`^(#\d+|[0-9a-f]{7,40})$`)
	// slugWord is a run of letters and digits in a question.
	slugWord = regexp.MustCompile(`[a-z0-9]+`)
)

// slugWords is how many words of a question make its slug.
const slugWords = 6

// writeAll writes m's map and the AGENTS.md pointer under root and returns
// the paths written, sorted, and m with what the write dropped added to it.
func writeAll(root string, m Map) ([]string, Map, error) {
	files, m := mapFiles(m)
	agents, err := os.ReadFile(filepath.Join(root, agentsPath))
	if err != nil && !os.IsNotExist(err) {
		return nil, m, err
	}
	files[agentsPath] = withPointer(string(agents))
	paths, err := writeFiles(root, files)
	return paths, m, err
}

// writeFiles writes each file of files under root and returns their paths,
// sorted.
func writeFiles(root string, files map[string]string) ([]string, error) {
	paths := make([]string, 0, len(files))
	for p, text := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

// mapFiles are the map's files by path: README.md, glossary.md, the product's
// folder and one folder per capability, where one with no recovered text
// holds its promise only. Each folder passes mapfolder.SafeFiles, and m comes
// back with what that took out added to its drops; the caller's m is not
// changed.
func mapFiles(m Map) (map[string]string, Map) {
	files := map[string]string{readmePath: mapfolder.Readme}
	taken := map[string]bool{}
	m.Capabilities = slices.Clone(m.Capabilities)
	for i, c := range m.Capabilities {
		for _, s := range slugLine.FindAllStringSubmatch(c.File, -1) {
			taken[s[1]] = true
		}
		f, err := mapfolder.Split(c.File)
		if c.File == "" || err != nil {
			if c.File != "" {
				c.Dropped = append(slices.Clone(c.Dropped), Drop{"capability", c.Name,
					fmt.Sprintf("its recovered text does not split into a folder (%v), so it holds its promise only", err)})
			}
			f, _ = mapfolder.Split(recovering.File(c.Name, c.Promise, ""))
		}
		safe, unsafe := mapfolder.SafeFiles(f.Files)
		for _, u := range unsafe {
			c.Dropped = append(slices.Clone(c.Dropped), Drop{u.Kind.String(), u.Heading, u.Reason})
		}
		m.Capabilities[i] = c
		put(files, mapfolder.CapabilityDir(c.Name), safe)
	}
	f, _ := mapfolder.Split(product(m, taken))
	safe, unsafe := mapfolder.SafeFiles(f.Files)
	m.Summary.Dropped = slices.Clone(m.Summary.Dropped)
	for _, u := range unsafe {
		m.Summary.Dropped = append(m.Summary.Dropped, Drop{u.Kind.String(), u.Heading, u.Reason})
	}
	put(files, mapfolder.ProductDir, safe)
	files[glossaryPath] = "# Glossary\n"
	if strings.TrimSpace(m.Terms) != "" {
		files[glossaryPath] = recovering.Glossary(m.Terms)
	}
	return files, m
}

// put adds a folder's files to files under dir.
func put(files map[string]string, dir string, folder map[string]string) {
	for name, text := range folder {
		files[dir+"/"+name] = text
	}
}

// product is the product as one text, for mapfolder.Split: the promise in
// front matter, the product's name as its title, and its questions, each with
// a slug no other entry takes and a heading the check accepts.
func product(m Map, taken map[string]bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\npromise: %s\n---\n\n# %s\n", draft.OneLine(m.ProductPromise), draft.OneLine(m.ProductName))
	if len(m.ProductQuestions) == 0 {
		return b.String()
	}
	date := m.Date
	if date.IsZero() {
		date = time.Now()
	}
	b.WriteString("\n## Questions\n")
	for _, q := range m.ProductQuestions {
		text := mapfolder.SafeHeading(draft.OneLine(q.Question))
		meta := []string{"`" + slug(text, taken) + "`", "raised by physa", date.Format("2006-01-02")}
		var anchors []string
		for _, ref := range q.SuggestedBy {
			if ref = strings.TrimSpace(ref); anchorRef.MatchString(ref) {
				anchors = append(anchors, ref)
			}
		}
		if len(anchors) > 0 {
			meta = append(meta, strings.Join(anchors, " "))
		}
		fmt.Fprintf(&b, "\n### %s\n%s\n%s\n", text, strings.Join(meta, " · "), questionBody)
	}
	return b.String()
}

// slug makes a slug from a question's first words that taken does not hold
// yet, and takes it.
func slug(question string, taken map[string]bool) string {
	words := slugWord.FindAllString(strings.ToLower(question), slugWords)
	base := strings.Join(words, "-")
	if base == "" {
		base = "question"
	}
	s := base
	for n := 2; taken[s]; n++ {
		s = fmt.Sprintf("%s-%d", base, n)
	}
	taken[s] = true
	return s
}

// withPointer is agents with the pointer block: put between its markers when
// it has them, else appended after one blank line. Nothing outside the
// markers changes, and an absent file is the block alone.
func withPointer(agents string) string {
	if start := strings.Index(agents, startMarker); start >= 0 {
		if end := strings.Index(agents[start:], endMarker); end >= 0 {
			end += start + len(endMarker)
			return agents[:start] + pointer + agents[end:]
		}
	}
	if agents == "" {
		return pointer + "\n"
	}
	if !strings.HasSuffix(agents, "\n") {
		agents += "\n"
	}
	if !strings.HasSuffix(agents, "\n\n") {
		agents += "\n"
	}
	return agents + pointer + "\n"
}
