package github

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

// oneAnswer answers repo view, and every graphql call with out and err, as gh
// does: stdout is given also when gh exits non-zero.
type oneAnswer struct {
	out   string
	err   error
	calls [][]string
}

func (o *oneAnswer) run(_ string, args ...string) ([]byte, error) {
	o.calls = append(o.calls, args)
	if args[0] == "repo" {
		return []byte(repoView), nil
	}
	return []byte(o.out), o.err
}

// included is gh's --include output: a status line, headers and a body.
func included(status string, body string, headers ...string) string {
	var b strings.Builder
	b.WriteString("HTTP/2.0 " + status + "\r\n")
	for _, h := range headers {
		b.WriteString(h + "\r\n")
	}
	b.WriteString("\r\n" + body)
	return b.String()
}

// pageLimit asks for one page with o and returns its error as a RateLimit.
func pageLimit(t *testing.T, o *oneAnswer) *forge.RateLimit {
	t.Helper()
	_, err := NewWithRunner(".", o.run).MergedPage(at("2026-11-01T00:00:00Z"), "")
	var l *forge.RateLimit
	if !errors.As(err, &l) {
		t.Fatalf("err = %v (%T), want a *forge.RateLimit", err, err)
	}
	return l
}

func TestSplitInclude(t *testing.T) {
	r := splitInclude([]byte("HTTP/2.0 403 Forbidden\r\nX-Ratelimit-Remaining: 0\r\nRetry-After: 60\r\n\r\n{\"a\":1}\n"))
	if r.status != 403 || r.headers["x-ratelimit-remaining"] != "0" || r.headers["retry-after"] != "60" || string(r.body) != "{\"a\":1}\n" {
		t.Errorf("got %d %v %q", r.status, r.headers, r.body)
	}
	r = splitInclude([]byte("HTTP/1.1 200 OK\nContent-Type: application/json\n\n{}"))
	if r.status != 200 || r.headers["content-type"] != "application/json" || string(r.body) != "{}" {
		t.Errorf("LF only: got %d %v %q", r.status, r.headers, r.body)
	}
	r = splitInclude([]byte(`{"data":null}`))
	if r.status != 0 || r.headers != nil || string(r.body) != `{"data":null}` {
		t.Errorf("body alone: got %d %v %q", r.status, r.headers, r.body)
	}
}

func TestMergedPageIncludeParses(t *testing.T) {
	o := &oneAnswer{out: included("200 OK", pageOf("c1",
		node(3, "2026-11-10T00:00:00Z", "2026-11-12T00:00:00Z", ""),
		node(2, "2026-10-08T00:00:00Z", "2026-11-09T00:00:00Z", "")),
		"Content-Type: application/json; charset=utf-8", "X-Ratelimit-Remaining: 4990", "X-Ratelimit-Reset: 1795000000")}
	page, err := NewWithRunner(".", o.run).MergedPage(at("2026-11-01T00:00:00Z"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := numbers(page.PRs); !reflect.DeepEqual(got, []int{3}) || page.Next != "c1" {
		t.Errorf("page = %v next %q", got, page.Next)
	}
	if !slices.Contains(o.calls[1], "--include") {
		t.Errorf("args lack --include: %q", o.calls[1])
	}
}

func TestMergedPageNext(t *testing.T) {
	since := at("2026-11-01T00:00:00Z")
	cases := []struct {
		name, out, want string
	}{
		{"more", pageOf("c1", node(1, "2026-11-10T00:00:00Z", "2026-11-10T00:00:00Z", "")), "c1"},
		{"crossed since", pageOf("c1", node(1, "2026-10-10T00:00:00Z", "2026-10-10T00:00:00Z", "")), ""},
		{"empty", pageOf("c1"), ""},
		{"last page", pageOf("", node(1, "2026-11-10T00:00:00Z", "2026-11-10T00:00:00Z", "")), ""},
	}
	for _, c := range cases {
		o := &oneAnswer{out: c.out}
		page, err := NewWithRunner(".", o.run).MergedPage(since, "c0")
		if err != nil || page.Next != c.want || page.PRs == nil {
			t.Errorf("%s: next %q, prs %v, err %v; want next %q", c.name, page.Next, page.PRs, err, c.want)
		}
		if !slices.Contains(o.calls[1], "cursor=c0") {
			t.Errorf("%s: cursor not sent: %q", c.name, o.calls[1])
		}
	}
}

func TestLimitPrimary(t *testing.T) {
	o := &oneAnswer{
		out: included("403 Forbidden", `{"message":"API rate limit exceeded for user ID 1.","documentation_url":"https://docs.github.com"}`,
			"X-Ratelimit-Limit: 5000", "X-Ratelimit-Remaining: 0", "X-Ratelimit-Reset: 1795000000"),
		err: &Error{ExitCode: 1, Stderr: "gh: API rate limit exceeded for user ID 1. (HTTP 403)"},
	}
	l := pageLimit(t, o)
	if l.Secondary || !l.Reset.Equal(time.Unix(1795000000, 0)) || l.RetryAfter != 0 || !strings.Contains(l.Detail, "API rate limit exceeded") {
		t.Errorf("primary = %+v", l)
	}
}

func TestLimitSecondary(t *testing.T) {
	o := &oneAnswer{
		out: included("403 Forbidden", `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`,
			"Retry-After: 60", "X-Ratelimit-Remaining: 4000", "X-Ratelimit-Reset: 1795000000"),
		err: &Error{ExitCode: 1, Stderr: "gh: You have exceeded a secondary rate limit. (HTTP 403)"},
	}
	l := pageLimit(t, o)
	if !l.Secondary || l.RetryAfter != 60*time.Second || !l.Reset.IsZero() {
		t.Errorf("secondary = %+v", l)
	}
	if !strings.Contains(l.Error(), "secondary rate limit") {
		t.Errorf("message = %q", l.Error())
	}
}

func TestLimitGraphQLRateLimited(t *testing.T) {
	o := &oneAnswer{
		out: included("200 OK", `{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`,
			"X-Ratelimit-Remaining: 3200", "X-Ratelimit-Reset: 1795000000"),
		err: &Error{ExitCode: 1, Stderr: "gh: API rate limit exceeded"},
	}
	l := pageLimit(t, o)
	if !l.Secondary || !l.Reset.IsZero() || l.RetryAfter != 0 || l.Detail != "API rate limit exceeded" {
		t.Errorf("RATE_LIMITED = %+v", l)
	}
	// The same error with no requests remaining is the primary limit.
	o.out = included("200 OK", `{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`,
		"X-Ratelimit-Remaining: 0", "X-Ratelimit-Reset: 1795000000")
	if l := pageLimit(t, o); l.Secondary || !l.Reset.Equal(time.Unix(1795000000, 0)) {
		t.Errorf("RATE_LIMITED, none remaining = %+v", l)
	}
}

func TestLimit429NoHints(t *testing.T) {
	o := &oneAnswer{
		out: included("429 Too Many Requests", `{"message":"Too many requests"}`),
		err: &Error{ExitCode: 1, Stderr: "gh: Too many requests (HTTP 429)"},
	}
	l := pageLimit(t, o)
	if l.RetryAfter != 0 || !l.Reset.IsZero() {
		t.Errorf("429 = %+v, want no hints", l)
	}
}

func TestLimitStderrOnly(t *testing.T) {
	o := &oneAnswer{err: &Error{ExitCode: 1, Stderr: "API rate limit exceeded (HTTP 403)\n"}}
	l := pageLimit(t, o)
	if l.RetryAfter != 0 || !l.Reset.IsZero() || l.Detail != "API rate limit exceeded (HTTP 403)" {
		t.Errorf("stderr only = %+v", l)
	}
	if strings.Contains(l.Error(), "unavailable") {
		t.Errorf("message = %q", l.Error())
	}
}

func TestLimitNotALimit(t *testing.T) {
	since := at("2026-11-01T00:00:00Z")
	// The request that spends the last point still answers with its page.
	o := &oneAnswer{out: included("200 OK", pageOf("", node(1, "2026-11-10T00:00:00Z", "2026-11-10T00:00:00Z", "")),
		"X-Ratelimit-Remaining: 0", "X-Ratelimit-Reset: 1795000000")}
	if page, err := NewWithRunner(".", o.run).MergedPage(since, ""); err != nil || len(page.PRs) != 1 {
		t.Errorf("last point: %v, %v", page, err)
	}
	// A 403 that is not a limit, and a plain GraphQL error, stay plain errors.
	o = &oneAnswer{out: included("403 Forbidden", `{"message":"Resource not accessible by integration"}`, "X-Ratelimit-Remaining: 10"),
		err: &Error{ExitCode: 1, Stderr: "gh: Resource not accessible by integration (HTTP 403)"}}
	var l *forge.RateLimit
	if _, err := NewWithRunner(".", o.run).MergedPage(since, ""); err == nil || errors.As(err, &l) {
		t.Errorf("403 not a limit: %v", err)
	}
	o = &oneAnswer{out: included("200 OK", `{"data":null,"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}]}`),
		err: &Error{ExitCode: 1, Stderr: "gh: Could not resolve to a Repository"}}
	if _, err := NewWithRunner(".", o.run).MergedPage(since, ""); err == nil || errors.As(err, &l) || !strings.Contains(err.Error(), "Could not resolve") {
		t.Errorf("graphql error: %v", err)
	}
	// An unauthenticated gh is still classed as one.
	o = &oneAnswer{err: &Error{ExitCode: 4, Stderr: "HTTP 401: Bad credentials"}}
	if _, err := NewWithRunner(".", o.run).MergedPage(since, ""); !errors.Is(err, forge.ErrUnauthenticated) {
		t.Errorf("401: %v", err)
	}
}

func TestMergedPRsReturnsRateLimit(t *testing.T) {
	o := &oneAnswer{err: &Error{ExitCode: 1, Stderr: "API rate limit exceeded (HTTP 403)"}}
	prs, more, err := NewWithRunner(".", o.run).MergedPRs(at("2026-11-01T00:00:00Z"), 10)
	var l *forge.RateLimit
	if !errors.As(err, &l) || prs != nil || more {
		t.Errorf("got %v, %v, %v", prs, more, err)
	}
	if len(o.calls) != 2 {
		t.Errorf("retried: %d calls", len(o.calls))
	}
}
