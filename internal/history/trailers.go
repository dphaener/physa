package history

import (
	"fmt"
	"regexp"
	"strings"
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

// readTrailers returns the capabilities of the Capability trailers and of the
// Loaded trailers in a commit message. The subject paragraph never holds
// trailers; from the last paragraph back, every paragraph made only of
// `Key: value` lines does, so a squash merge's own trailers after a blank
// line read like the branch commit's.
func readTrailers(message string) (capabilities, loaded []string) {
	capabilities, loaded = []string{}, []string{}
	paras := paragraphs(message)
	first := len(paras)
	for first > 1 && isTrailerBlock(paras[first-1]) {
		first--
	}
	for _, para := range paras[first:] {
		for _, t := range joinContinuations(para) {
			key, value, _ := strings.Cut(t, ":")
			switch strings.ToLower(key) {
			case "capability":
				capabilities = append(capabilities, splitSlugs(value)...)
			case "loaded":
				loaded = append(loaded, splitSlugs(value)...)
			}
		}
	}
	return capabilities, loaded
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

// Check reports the trailer findings of commits against the capability slugs
// known accepts. Findings follow commit order; for each commit, missing or
// multiple come before unknown slugs, Capability before Loaded, each in the
// order written.
func Check(commits []Commit, known func(slug string) bool) []Finding {
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
		for _, s := range c.Capabilities {
			if !known(s) {
				add(KindUnknown, s, "Capability %q is not in the map", s)
			}
		}
		for _, s := range c.Loaded {
			if !known(s) {
				add(KindUnknown, s, "Loaded %q is not in the map", s)
			}
		}
	}
	return findings
}
