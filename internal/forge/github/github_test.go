package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dphaener/physa/internal/forge"
)

type fake struct {
	calls [][]string
	out   string
	err   error
}

func (f *fake) run(_ context.Context, dir string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	return []byte(f.out), f.err
}

func TestName(t *testing.T) {
	if got := New(".").Name(); got != "github" {
		t.Errorf("Name = %q", got)
	}
}

func TestPRExists(t *testing.T) {
	f := &fake{out: "7\n"}
	ok, err := NewWithRunner(".", f.run).PRExists(t.Context(), 7)
	if err != nil || !ok {
		t.Fatalf("got %v, %v", ok, err)
	}
	want := []string{"api", "repos/{owner}/{repo}/pulls/7", "--jq", ".number"}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Errorf("args = %v", f.calls[0])
	}
	f.err = &Error{Stderr: "gh: Not Found (HTTP 404)", ExitCode: 1}
	ok, err = NewWithRunner(".", f.run).PRExists(t.Context(), 8)
	if err != nil || ok {
		t.Errorf("404: got %v, %v", ok, err)
	}
	f.err = &Error{Stderr: "boom", ExitCode: 1}
	if ok, err = NewWithRunner(".", f.run).PRExists(t.Context(), 8); err == nil || ok {
		t.Errorf("other: got %v, %v", ok, err)
	}
}

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		want   error
		reason string
	}{
		{"no gh", fmt.Errorf("fork: %w", exec.ErrNotFound), forge.ErrUnavailable, "gh is not installed"},
		{"exit 4", &Error{ExitCode: 4, Stderr: "x"}, forge.ErrUnauthenticated, "gh is not authenticated"},
		{"auth login", &Error{ExitCode: 1, Stderr: "run: gh auth login"}, forge.ErrUnauthenticated, "gh is not authenticated"},
		{"401", &Error{ExitCode: 1, Stderr: "HTTP 401: Bad credentials"}, forge.ErrUnauthenticated, "gh is not authenticated"},
		{"no remote", &Error{ExitCode: 1, Stderr: "none of the git remotes configured for this repository point to a known GitHub host"}, forge.ErrUnavailable, "gh finds no GitHub remote"},
		{"no remotes", &Error{ExitCode: 1, Stderr: "no git remotes found"}, forge.ErrUnavailable, "gh finds no GitHub remote"},
		{"not git", &Error{ExitCode: 1, Stderr: "fatal: not a git repository (or any of the parent directories): .git"}, forge.ErrUnavailable, "gh finds no GitHub remote"},
		{"404", &Error{ExitCode: 1, Stderr: "HTTP 404"}, forge.ErrNotFound, "not found on GitHub"},
	}
	for _, c := range cases {
		f := &fake{err: c.err}
		_, err := NewWithRunner(".", f.run).gh(t.Context(), "api", "x")
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
		if got := forge.Reason(err); got != c.reason {
			t.Errorf("%s: reason %q, want %q", c.name, got, c.reason)
		}
	}
	f := &fake{err: &Error{ExitCode: 1, Stderr: "kaput"}}
	_, err := NewWithRunner(".", f.run).gh(t.Context(), "api", "x")
	var ge *Error
	if !errors.As(err, &ge) || !strings.Contains(err.Error(), "kaput") {
		t.Errorf("other: %v", err)
	}
	for _, s := range []error{forge.ErrUnavailable, forge.ErrUnauthenticated, forge.ErrNotFound} {
		if errors.Is(err, s) {
			t.Errorf("other classed as %v", s)
		}
	}
}

func fakeGH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestDefaultRunner(t *testing.T) {
	fakeGH(t, "echo 7\n")
	ok, err := New(t.TempDir()).PRExists(t.Context(), 7)
	if err != nil || !ok {
		t.Errorf("success: %v, %v", ok, err)
	}
	fakeGH(t, "echo 'HTTP 404' >&2\nexit 1\n")
	ok, err = New(t.TempDir()).PRExists(t.Context(), 7)
	if err != nil || ok {
		t.Errorf("404: %v, %v", ok, err)
	}
	fakeGH(t, "echo nope >&2\nexit 4\n")
	if _, err = New(t.TempDir()).PRExists(t.Context(), 7); !errors.Is(err, forge.ErrUnauthenticated) {
		t.Errorf("exit 4: %v", err)
	}
	fakeGH(t, "echo '{\"errors\":[{\"message\":\"bad oid\"}]}'\necho 'gh: bad oid' >&2\nexit 1\n")
	c := New(t.TempDir())
	c.owner, c.name = "o", "r"
	if _, _, err = c.ChangePR(t.Context(), "abc"); err == nil || !strings.Contains(err.Error(), "bad oid") {
		t.Errorf("graphql error with exit 1: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err = New(t.TempDir()).PRExists(t.Context(), 7); !errors.Is(err, forge.ErrUnavailable) || forge.Reason(err) != "gh is not installed" {
		t.Errorf("no gh: %v", err)
	}
}

func TestDefaultRunnerCancelledIsNoTimeout(t *testing.T) {
	fakeGH(t, "echo 7\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := ghStdinRunner(ctx, t.TempDir(), nil, "api", "x")
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "timed out") {
		t.Errorf("cancelled: %v", err)
	}
}

// script answers gh calls by their first two arguments, and records them.
type script struct {
	calls   [][]string
	answers map[string]string
	errs    map[string]error
}

func (s *script) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	s.calls = append(s.calls, args)
	key := strings.Join(args[:2], " ")
	if err, ok := s.errs[key]; ok {
		return nil, err
	}
	if out, ok := s.answers[key]; ok {
		return []byte(out), nil
	}
	return nil, &Error{Args: args, Stderr: "HTTP 404", ExitCode: 1}
}

const repoView = `{"name":"physa","owner":{"id":"X","login":"dphaener"}}`

const fullPR = `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[{
  "number": 9,
  "author": {"login": "agent"},
  "comments": {"nodes": [
    {"author": {"login": "dphaener", "__typename": "User"}, "createdAt": "2026-11-20T10:05:00Z", "body": "why not B?"},
    {"author": {"login": "ci-bot", "__typename": "Bot"}, "createdAt": "2026-11-20T10:01:00Z", "body": "deployed"},
    {"author": {"login": "renovate[bot]", "__typename": "User"}, "createdAt": "2026-11-20T10:02:00Z", "body": "bump"}
  ]},
  "reviews": {"nodes": [
    {"author": {"login": "dphaener", "__typename": "User"}, "state": "CHANGES_REQUESTED", "submittedAt": "2026-11-20T10:03:00Z", "body": "Rename this.",
     "comments": {"nodes": [{"author": {"login": "dphaener", "__typename": "User"}, "createdAt": "2026-11-20T10:02:30Z", "body": "here"}]}},
    {"author": {"login": "dphaener", "__typename": "User"}, "submittedAt": "2026-11-20T10:04:00Z", "body": "  ", "comments": {"nodes": []}},
    {"author": {"login": "dphaener", "__typename": "User"}, "submittedAt": null, "body": "pending", "comments": {"nodes": []}}
  ]},
  "commits": {"nodes": [
    {"commit": {"oid": "bbb", "committedDate": "2026-11-20T11:00:00Z"}},
    {"commit": {"oid": "aaa", "committedDate": "2026-11-20T09:00:00Z"}}
  ]}
}]}}}}}`

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestChangePR(t *testing.T) {
	s := &script{answers: map[string]string{"repo view": repoView, "api graphql": fullPR}}
	c := NewWithRunner(".", s.run)
	pr, ok, err := c.ChangePR(t.Context(), "c0ffee")
	if err != nil || !ok {
		t.Fatalf("got %v, %v", ok, err)
	}
	want := forge.PR{
		Number: 9,
		Author: "agent",
		Comments: []forge.Comment{
			{Author: "dphaener", At: at("2026-11-20T10:02:30Z"), Kind: forge.KindReview, Body: "here", RequestsChanges: true},
			{Author: "dphaener", At: at("2026-11-20T10:03:00Z"), Kind: forge.KindReview, Body: "Rename this.", RequestsChanges: true},
			{Author: "dphaener", At: at("2026-11-20T10:05:00Z"), Kind: forge.KindDiscussion, Body: "why not B?"},
		},
		Commits: []forge.Commit{{ID: "aaa", At: at("2026-11-20T09:00:00Z")}, {ID: "bbb", At: at("2026-11-20T11:00:00Z")}},
	}
	if !reflect.DeepEqual(pr, want) {
		t.Errorf("pr =\n%+v\nwant\n%+v", pr, want)
	}
	wantCalls := [][]string{
		{"repo", "view", "--json", "owner,name"},
		{"api", "graphql", "-f", "query=" + changePRQuery, "-f", "owner=dphaener", "-f", "name=physa", "-f", "oid=c0ffee"},
	}
	if !reflect.DeepEqual(s.calls, wantCalls) {
		t.Errorf("calls = %q", s.calls)
	}
	if _, _, err := c.ChangePR(t.Context(), "c0ffee"); err != nil || len(s.calls) != 3 || s.calls[2][0] != "api" {
		t.Errorf("second call did not reuse the repo: %v, %q", err, s.calls)
	}
}

func TestChangePRNoPR(t *testing.T) {
	for name, body := range map[string]string{
		"no node":    `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[]}}}}}`,
		"no object":  `{"data":{"repository":{"object":null}}}`,
		"not commit": `{"data":{"repository":{"object":{}}}}`,
	} {
		s := &script{answers: map[string]string{"repo view": repoView, "api graphql": body}}
		pr, ok, err := NewWithRunner(".", s.run).ChangePR(t.Context(), "c0ffee")
		if err != nil || ok || !reflect.DeepEqual(pr, forge.PR{}) {
			t.Errorf("%s: got %+v, %v, %v", name, pr, ok, err)
		}
	}
}

func TestChangePRGraphQLErrors(t *testing.T) {
	s := &script{answers: map[string]string{"repo view": repoView,
		"api graphql": `{"data":null,"errors":[{"message":"Could not resolve to a Repository"},{"message":"second"}]}`}}
	_, ok, err := NewWithRunner(".", s.run).ChangePR(t.Context(), "c0ffee")
	if ok || err == nil || !strings.Contains(err.Error(), "Could not resolve to a Repository") || strings.Contains(err.Error(), "second") {
		t.Errorf("got %v, %v", ok, err)
	}
	s.answers["api graphql"] = "not json"
	if _, ok, err = NewWithRunner(".", s.run).ChangePR(t.Context(), "c0ffee"); ok || err == nil {
		t.Errorf("bad json: %v, %v", ok, err)
	}
}

func TestChangePRUnavailable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"gh missing", &exec.Error{Name: "gh", Err: exec.ErrNotFound}, forge.ErrUnavailable},
		{"unauthenticated", &Error{ExitCode: 4, Stderr: "To get started with GitHub CLI, please run:  gh auth login"}, forge.ErrUnauthenticated},
		{"no GitHub remote", &Error{ExitCode: 1, Stderr: "none of the git remotes configured for this repository point to a known GitHub host."}, forge.ErrUnavailable},
	}
	for _, c := range cases {
		s := &script{errs: map[string]error{"repo view": c.err}}
		_, ok, err := NewWithRunner(".", s.run).ChangePR(t.Context(), "c0ffee")
		if ok || !errors.Is(err, c.want) {
			t.Errorf("%s: %v, %v", c.name, ok, err)
		}
		if len(s.calls) != 1 {
			t.Errorf("%s: calls after failure = %q", c.name, s.calls)
		}
	}
	s := &script{answers: map[string]string{"repo view": repoView},
		errs: map[string]error{"api graphql": &Error{ExitCode: 4, Stderr: "HTTP 401: Bad credentials"}}}
	if _, _, err := NewWithRunner(".", s.run).ChangePR(t.Context(), "c0ffee"); !errors.Is(err, forge.ErrUnauthenticated) {
		t.Errorf("graphql 401: %v", err)
	}
}

func TestRepoViewBad(t *testing.T) {
	for _, out := range []string{"nope", `{"name":"","owner":{"login":""}}`} {
		s := &script{answers: map[string]string{"repo view": out}}
		if _, _, err := NewWithRunner(".", s.run).ChangePR(t.Context(), "c0ffee"); err == nil {
			t.Errorf("%q: no error", out)
		}
	}
}

func TestChangeLogins(t *testing.T) {
	f := &fake{out: `{"data":{"repository":{"c0":{"author":{"user":{"login":"dana"}}},"c1":{"author":{"user":null}},"c2":null}}}`}
	c := NewWithRunner(".", f.run)
	c.owner, c.name = "o", "n"
	got, err := c.ChangeLogins(t.Context(), []string{"aaa", "bbb", "ccc"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]string{"aaa": "dana"}) {
		t.Errorf("logins = %v", got)
	}
	if len(f.calls) != 1 || !strings.Contains(strings.Join(f.calls[0], " "), `c2:object(oid:"ccc")`) {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestChangeLoginsAsksAHundredAtATime(t *testing.T) {
	f := &fake{out: `{"data":{"repository":{}}}`}
	c := NewWithRunner(".", f.run)
	c.owner, c.name = "o", "n"
	ids := make([]string, 250)
	for i := range ids {
		ids[i] = fmt.Sprintf("%040d", i)
	}
	if _, err := c.ChangeLogins(t.Context(), ids); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 {
		t.Errorf("queries = %d, want 3", len(f.calls))
	}
}
