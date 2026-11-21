package initbranch

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/recovering"
)

// The files init writes, relative to the repo root.
const (
	readmePath   = ".physa/README.md"
	productPath  = ".physa/product.md"
	glossaryPath = recovering.GlossaryPath
	capDir       = ".physa/capabilities"
	agentsPath   = "AGENTS.md"
)

// statusFailed is the status the store keeps for a capability whose
// recovery failed.
const statusFailed = "failed"

// readme is README.md's fixed text (design 01 section 6).
const readme = "This is the product's map. Read product.md first. Each file in capabilities/ is one promise the product makes, with its rules and decisions. Read the ones related to your task. Anchor commits with `Capability: <file name>`.\n"

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
// the paths written, sorted.
func writeAll(root string, m Map) ([]string, error) {
	files := mapFiles(m)
	agents, err := os.ReadFile(filepath.Join(root, agentsPath))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	files[agentsPath] = withPointer(string(agents))
	return writeFiles(root, files)
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

// mapFiles are the map's files by path: README.md, product.md, glossary.md
// and one file per capability, where one with no recovered file holds its
// promise only.
func mapFiles(m Map) map[string]string {
	files := map[string]string{readmePath: readme}
	taken := map[string]bool{}
	for _, c := range m.Capabilities {
		text := c.File
		if text == "" {
			text = recovering.File(c.Name, c.Promise, "")
		}
		files[capDir+"/"+c.Name+".md"] = text
		for _, s := range slugLine.FindAllStringSubmatch(text, -1) {
			taken[s[1]] = true
		}
	}
	files[productPath] = product(m, taken)
	files[glossaryPath] = "# Glossary\n"
	if strings.TrimSpace(m.Terms) != "" {
		files[glossaryPath] = recovering.Glossary(m.Terms)
	}
	return files
}

// product is product.md: the promise in front matter, the product's name as
// its title, and its questions, each with a slug no other entry takes.
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
		text := draft.OneLine(q.Question)
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
