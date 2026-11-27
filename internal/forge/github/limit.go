package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

// response is what `gh api --include` printed: the HTTP status, the headers
// (keys in lower case) and the body. Output that does not start with a status
// line is a body alone, with status 0 and no headers.
type response struct {
	status  int
	headers map[string]string
	body    []byte
}

// splitInclude splits gh's --include output into its status line, headers
// and body. Lines may end in "\r\n" or "\n"; a blank line ends the headers.
func splitInclude(out []byte) response {
	if !bytes.HasPrefix(out, []byte("HTTP/")) {
		return response{body: out}
	}
	r := response{headers: map[string]string{}}
	rest := out
	for first := true; len(rest) > 0; first = false {
		line := rest
		rest = nil
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line, rest = line[:i], line[i+1:]
		}
		line = bytes.TrimRight(line, "\r")
		if first {
			if f := strings.Fields(string(line)); len(f) >= 2 {
				r.status, _ = strconv.Atoi(f[1])
			}
			continue
		}
		if len(line) == 0 {
			r.body = rest
			break
		}
		if k, v, ok := strings.Cut(string(line), ":"); ok {
			r.headers[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return r
}

// limitBody is the part of a REST or GraphQL body that can name a rate limit.
type limitBody struct {
	Message string `json:"message"`
	Errors  []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"errors"`
}

// rateLimit returns the rate limit r reports, or nil when it reports none,
// following GitHub's docs for REST and GraphQL. A 429, a 403 with no
// requests remaining, a 200 with no requests remaining and errors, a GraphQL
// error of type RATE_LIMITED, or a message naming a rate limit is one. It is
// the primary limit only when no requests remain and no message names the
// secondary limit. RetryAfter comes from retry-after, and Reset from
// x-ratelimit-reset only when no requests remain.
func rateLimit(r response) *forge.RateLimit {
	var b limitBody
	_ = json.Unmarshal(r.body, &b) // a body that is not JSON names no limit
	remaining, ok := r.headers["x-ratelimit-remaining"]
	exhausted := ok && remaining == "0"
	detail, limited := b.Message, mentionsLimit(b.Message)
	for _, e := range b.Errors {
		if e.Type == "RATE_LIMITED" || mentionsLimit(e.Message) {
			detail, limited = e.Message, true
			break
		}
	}
	if detail == "" && len(b.Errors) > 0 {
		detail = b.Errors[0].Message
	}
	switch r.status {
	case 429:
		limited = true
	case 403:
		limited = limited || exhausted
	case 200:
		// The request that spends the last point still answers with data,
		// so a 200 is a limit only when it also carries errors.
		limited = limited || (exhausted && len(b.Errors) > 0)
	}
	if !limited {
		return nil
	}
	l := &forge.RateLimit{
		Secondary: !exhausted || strings.Contains(strings.ToLower(detail), "secondary rate limit"),
		Detail:    detail,
	}
	if s, err := strconv.Atoi(r.headers["retry-after"]); err == nil && s > 0 {
		l.RetryAfter = time.Duration(s) * time.Second
	}
	if exhausted {
		if s, err := strconv.ParseInt(r.headers["x-ratelimit-reset"], 10, 64); err == nil && s > 0 {
			l.Reset = time.Unix(s, 0).UTC()
		}
	}
	if l.Detail == "" && r.status != 0 {
		l.Detail = "HTTP " + strconv.Itoa(r.status)
	}
	return l
}

// stderrLimit returns the rate limit a gh that exited non-zero names on
// stderr alone, with no hints of when to retry, or nil when it names none.
func stderrLimit(err error) *forge.RateLimit {
	var ge *Error
	if !errors.As(err, &ge) || !mentionsLimit(ge.Stderr) {
		return nil
	}
	stderr := strings.TrimSpace(ge.Stderr)
	return &forge.RateLimit{Secondary: true, Detail: stderr}
}

// mentionsLimit reports whether s names a rate limit, in any case.
func mentionsLimit(s string) bool { return strings.Contains(strings.ToLower(s), "rate limit") }
