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
		_, err := NewWithRunner(".", f.run).gh("api", "x")
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	f := &fake{err: &Error{ExitCode: 1, Stderr: "kaput"}}
	_, err := NewWithRunner(".", f.run).gh("api", "x")
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
