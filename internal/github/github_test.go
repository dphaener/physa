package github

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fake struct {
	calls [][]string
	out   string
	err   error
}

func (f *fake) run(dir string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	return []byte(f.out), f.err
}

func TestPRExists(t *testing.T) {
	f := &fake{out: "7\n"}
	ok, err := NewWithRunner(".", f.run).PRExists(7)
	if err != nil || !ok {
		t.Fatalf("got %v, %v", ok, err)
	}
	want := []string{"api", "repos/{owner}/{repo}/pulls/7", "--jq", ".number"}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Errorf("args = %v", f.calls[0])
	}
	f.err = &Error{Stderr: "gh: Not Found (HTTP 404)", ExitCode: 1}
	ok, err = NewWithRunner(".", f.run).PRExists(8)
	if err != nil || ok {
		t.Errorf("404: got %v, %v", ok, err)
	}
	f.err = &Error{Stderr: "boom", ExitCode: 1}
	if ok, err = NewWithRunner(".", f.run).PRExists(8); err == nil || ok {
		t.Errorf("other: got %v, %v", ok, err)
	}
}

func TestPullRequests(t *testing.T) {
	f := &fake{out: `{"number":5,"head":"b","state":"closed","merged_at":null}
{"number":3,"head":"a","state":"closed","merged_at":"2026-01-01T00:00:00Z"}
{"number":9,"head":"c","state":"open","merged_at":null}
{"number":4,"head":"d","state":"closed","merged_at":null}
`}
	prs, err := NewWithRunner(".", f.run).PullRequests()
	if err != nil {
		t.Fatal(err)
	}
	want := []PR{{3, "a", "merged"}, {9, "c", "open"}}
	if !reflect.DeepEqual(prs, want) {
		t.Errorf("prs = %v", prs)
	}
	if got := f.calls[0]; got[1] != "--paginate" || !strings.Contains(got[2], "state=all&per_page=100") {
		t.Errorf("args = %v", got)
	}
	f.out = "{not json"
	if _, err := NewWithRunner(".", f.run).PullRequests(); err == nil {
		t.Error("bad JSON should fail")
	}
}

func TestCheckRuns(t *testing.T) {
	f := &fake{out: `{"id":12,"head_sha":"abc","status":"completed","conclusion":"neutral","completed_at":"2026-01-01T00:00:00Z","output":{"title":"T","text":"{}"}}
{"id":13,"head_sha":"abc","status":"in_progress","conclusion":null,"completed_at":null,"output":{"title":null,"text":null}}
`}
	runs, err := NewWithRunner(".", f.run).CheckRuns("abc", "physa")
	if err != nil {
		t.Fatal(err)
	}
	want := []CheckRun{
		{12, "abc", "completed", "neutral", "2026-01-01T00:00:00Z", "T", "{}"},
		{ID: 13, HeadSHA: "abc", Status: "in_progress"},
	}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("runs = %+v", runs)
	}
	if got := f.calls[0][1]; got != "repos/{owner}/{repo}/commits/abc/check-runs?check_name=physa&per_page=100" {
		t.Errorf("path = %s", got)
	}
}

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"no gh", fmt.Errorf("fork: %w", exec.ErrNotFound), ErrNoGH},
		{"exit 4", &Error{ExitCode: 4, Stderr: "x"}, ErrUnauthenticated},
		{"auth login", &Error{ExitCode: 1, Stderr: "run: gh auth login"}, ErrUnauthenticated},
		{"401", &Error{ExitCode: 1, Stderr: "HTTP 401: Bad credentials"}, ErrUnauthenticated},
		{"404", &Error{ExitCode: 1, Stderr: "HTTP 404"}, ErrNotFound},
	}
	for _, c := range cases {
		f := &fake{err: c.err}
		_, err := NewWithRunner(".", f.run).PullRequests()
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	f := &fake{err: &Error{ExitCode: 1, Stderr: "kaput"}}
	_, err := NewWithRunner(".", f.run).PullRequests()
	var ge *Error
	if !errors.As(err, &ge) || !strings.Contains(err.Error(), "kaput") {
		t.Errorf("other: %v", err)
	}
	for _, s := range []error{ErrNoGH, ErrUnauthenticated, ErrNotFound} {
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
	ok, err := New(t.TempDir()).PRExists(7)
	if err != nil || !ok {
		t.Errorf("success: %v, %v", ok, err)
	}
	fakeGH(t, "echo 'HTTP 404' >&2\nexit 1\n")
	ok, err = New(t.TempDir()).PRExists(7)
	if err != nil || ok {
		t.Errorf("404: %v, %v", ok, err)
	}
	fakeGH(t, "echo nope >&2\nexit 4\n")
	if _, err = New(t.TempDir()).PRExists(7); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("exit 4: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err = New(t.TempDir()).PRExists(7); !errors.Is(err, ErrNoGH) {
		t.Errorf("no gh: %v", err)
	}
}
