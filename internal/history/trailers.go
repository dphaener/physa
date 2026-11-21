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

// Check reports the trailer findings of commits. A slug is known when its
// file is in the map as that commit leaves it, so a commit that adds a
// capability may name it, and a slug only a later commit adds is unknown.
// Findings follow commit order; for each commit, missing or multiple come
// before unknown slugs, each in the order written. The error is for a commit
// whose files version control cannot list.
func Check(repo vcs.Repo, commits []Commit) ([]Finding, error) {
	findings := []Finding{}
	for _, c := range commits {
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
		known, err := slugsAt(repo, c.Hash)
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

// slugsAt returns the capability slugs whose files change id leaves in the
// map.
func slugsAt(repo vcs.Repo, id string) (map[string]bool, error) {
	files, err := repo.Files(id, CapabilitiesDir)
	if err != nil {
		return nil, fmt.Errorf("cannot list the map's capabilities at %s: %w", id, err)
	}
	slugs := make(map[string]bool, len(files))
	for _, f := range files {
		if stem, ok := strings.CutSuffix(path.Base(f), ".md"); ok {
			slugs[stem] = true
		}
	}
	return slugs, nil
}
