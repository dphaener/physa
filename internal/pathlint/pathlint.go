// Package pathlint finds the file paths a recovered capability text names
// outside its anchors. The map holds no paths (vision §4.1): a path belongs
// only in a `from <path>` anchor on an entry's meta line, so a path in a
// heading, body or Because line is a problem to send back to the session.
package pathlint

import (
	"fmt"
	"regexp"
	"strings"
)

// Hit is one file path named where none belongs: its 1-based line in the
// text and the path as written.
type Hit struct {
	Line int
	Path string
}

// Message is what is sent back to the session for a path: the reason an
// entry naming it is a problem, and the reason it is dropped if it stays.
func Message(path string) string {
	return fmt.Sprintf("names the file path `%s`; a path belongs only in a `from <path>` anchor on the meta line, never in a heading, body or Because line", path)
}

// A path is a token with a folder and an extension, or a bare file name with
// a common source or doc extension.
var pathy = regexp.MustCompile(`(?:[\w.-]+/)+[\w.-]+\.[A-Za-z0-9]+\b|[\w-]+\.(?:go|rb|py|js|ts|tsx|jsx|java|kt|rs|c|h|cpp|cs|php|swift|md|yml|yaml|json|toml|sql|erb|rake|sh|css|scss|html)\b`)

// Find returns the paths one map file's text names outside its anchors, in
// order. It skips front matter, fenced blocks and each entry's meta line (the
// first non-blank line after a `### ` heading, which starts with a backtick);
// the title, overview, headings, bodies, Because and Rejected lines are read.
func Find(text string) []Hit {
	var hits []Hit
	lines := strings.Split(text, "\n")
	i := 0
	if len(lines) > 0 && strings.TrimRight(lines[0], " \t\r") == "---" {
		for i = 1; i < len(lines); i++ {
			if strings.TrimRight(lines[i], " \t\r") == "---" {
				i++
				break
			}
		}
	}
	fence := ""
	awaitMeta := false
	for ; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		if strings.HasPrefix(line, "### ") {
			awaitMeta = true
		} else if awaitMeta && trimmed != "" {
			awaitMeta = false
			if strings.HasPrefix(trimmed, "`") {
				continue
			}
		}
		for _, p := range paths(line) {
			hits = append(hits, Hit{Line: i + 1, Path: p})
		}
	}
	return hits
}

// paths lists the paths on one line, leaving out those that are part of a URL,
// follow a word character or a backtick, or end in digits only (1.2/3.4).
func paths(line string) []string {
	var out []string
	for _, m := range pathy.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		if start > 0 {
			c := line[start-1]
			if c == '`' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				continue
			}
		}
		tokenStart := strings.LastIndexAny(line[:start], " \t") + 1
		if strings.Contains(line[tokenStart:end], "://") {
			continue
		}
		p := line[start:end]
		ext := p[strings.LastIndex(p, ".")+1:]
		if strings.Trim(ext, "0123456789") == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
