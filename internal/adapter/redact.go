package adapter

import (
	"regexp"
	"strings"
	"unicode"
)

// redacted replaces each token- or key-shaped piece of text.
const redacted = "[redacted]"

// keyShapes are whole matches replaced by redacted: keys with a known prefix,
// JWTs and PEM private key blocks. Each prefix needs a word boundary before
// it, so "task-notification-…" or "risk-assessment-…" never match "sk-".
// Go's regexp runs in time linear in its input, so no pattern can run away.
var keyShapes = []*regexp.Regexp{
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35}`),
	regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}`),
	// A PEM private key block, whole; one with no end line is redacted to
	// the end of the text, since the rest of it is the key.
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?:.*?-----END [A-Z0-9 ]*PRIVATE KEY-----|.*)`),
}

// bearer is an HTTP bearer credential; the scheme is kept, the token
// redacted.
var bearer = regexp.MustCompile(`\b([Bb]earer\s+)([A-Za-z0-9._~+/=-]+)`)

// assignment is a secret-named word, then = or :, then a value. The name is
// kept and the value redacted, when the value looks like a secret (see
// secretValue).
var assignment = regexp.MustCompile(`(?i)\b((?:api_key|apikey|api-key|token|secret|password|passwd)\b["']?\s*[=:]\s*["']?)(\S+)`)

// Redact replaces token- and key-shaped text in s with "[redacted]": keys
// with a known prefix (Anthropic, OpenAI, GitHub, Slack, AWS, Google,
// GitLab), JWTs, PEM private key blocks, the token after "Bearer", and the
// value of an assignment to a secret-named word (api_key, apikey, api-key,
// token, secret, password, passwd), whose name is kept. Commit ids and UUIDs
// are left alone, and so are keys with no known prefix: a bare 40-hex token
// cannot be told from a commit id.
func Redact(s string) string {
	for _, re := range keyShapes {
		s = re.ReplaceAllString(s, redacted)
	}
	s = replaceValue(bearer, s)
	return replaceValue(assignment, s)
}

// replaceValue redacts the second group of each match of re that looks like
// a secret, keeping the first group and any punctuation ending the value.
func replaceValue(re *regexp.Regexp, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		g := re.FindStringSubmatch(m)
		if strings.HasPrefix(g[2], redacted) {
			return m
		}
		value := strings.TrimRight(g[2], `.,;:!?)]}'"`+"`")
		if !secretValue(value) {
			return m
		}
		return g[1] + redacted + g[2][len(value):]
	})
}

// secretValue reports whether v looks like a secret rather than prose: eight
// or more characters, at least one of them a digit or a symbol.
func secretValue(v string) bool {
	if len([]rune(v)) < 8 {
		return false
	}
	for _, r := range v {
		if !unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
