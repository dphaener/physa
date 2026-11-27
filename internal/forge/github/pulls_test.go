package github

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dphaener/physa/internal/forge"
)

type call struct {
	args  []string
	stdin string
}

type stdinFake struct {
	calls []call
	out   string
	err   error
}

func (f *stdinFake) run(_ context.Context, _ string, stdin []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{args, string(stdin)})
	return []byte(f.out), f.err
}

func TestOpenPR(t *testing.T) {
	f := &stdinFake{out: `[{"number":12,"url":"https://github.com/o/r/pull/12"}]`}
	n, url, ok, err := NewWithStdinRunner(".", f.run).OpenPR(t.Context(), "physa/learn")
	if err != nil || !ok || n != 12 || url != "https://github.com/o/r/pull/12" {
		t.Fatalf("got %d %q %v %v", n, url, ok, err)
	}
	want := []string{"pr", "list", "--head", "physa/learn", "--state", "open", "--json", "number,url", "--limit", "1"}
	if !reflect.DeepEqual(f.calls[0].args, want) {
		t.Errorf("args = %v", f.calls[0].args)
	}
}

func TestOpenPRNoneOpen(t *testing.T) {
	f := &stdinFake{out: `[]`}
	_, _, ok, err := NewWithStdinRunner(".", f.run).OpenPR(t.Context(), "physa/learn")
	if err != nil || ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
}

func TestCreatePR(t *testing.T) {
	f := &stdinFake{out: "https://github.com/o/r/pull/34\n"}
	n, url, err := NewWithStdinRunner(".", f.run).CreatePR(t.Context(), "physa/learn", "main", "A title", "the body\n")
	if err != nil || n != 34 || url != "https://github.com/o/r/pull/34" {
		t.Fatalf("got %d %q %v", n, url, err)
	}
	want := []string{"pr", "create", "--head", "physa/learn", "--base", "main", "--title", "A title", "--body-file", "-"}
	if !reflect.DeepEqual(f.calls[0].args, want) || f.calls[0].stdin != "the body\n" {
		t.Errorf("call = %+v", f.calls[0])
	}
}

func TestCreatePRNoNumber(t *testing.T) {
	f := &stdinFake{out: "created\n"}
	if _, _, err := NewWithStdinRunner(".", f.run).CreatePR(t.Context(), "h", "main", "t", "b"); err == nil {
		t.Fatal("want an error")
	}
}

func TestEditPR(t *testing.T) {
	f := &stdinFake{}
	if err := NewWithStdinRunner(".", f.run).EditPR(t.Context(), 34, "new body"); err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "edit", "34", "--body-file", "-"}
	if !reflect.DeepEqual(f.calls[0].args, want) || f.calls[0].stdin != "new body" {
		t.Errorf("call = %+v", f.calls[0])
	}
}

func TestPRsUnauth(t *testing.T) {
	f := &stdinFake{err: &Error{Args: []string{"pr", "create"}, Stderr: "To get started with GitHub CLI, please run:  gh auth login", ExitCode: 4}}
	c := NewWithStdinRunner(".", f.run)
	_, _, err := c.CreatePR(t.Context(), "h", "main", "t", "b")
	if !errors.Is(err, forge.ErrUnauthenticated) {
		t.Fatalf("create: %v", err)
	}
	if err := c.EditPR(t.Context(), 1, "b"); !errors.Is(err, forge.ErrUnauthenticated) {
		t.Fatalf("edit: %v", err)
	}
	if _, _, _, err := c.OpenPR(t.Context(), "h"); !errors.Is(err, forge.ErrUnauthenticated) {
		t.Fatalf("list: %v", err)
	}
	if got := forge.Reason(err); got != "gh is not authenticated" {
		t.Errorf("reason = %q", got)
	}
}

func TestPRsUnavailable(t *testing.T) {
	f := &stdinFake{err: &Error{Args: []string{"pr", "list"}, Stderr: "none of the git remotes point to GitHub", ExitCode: 1}}
	_, _, _, err := NewWithStdinRunner(".", f.run).OpenPR(t.Context(), "h")
	if !errors.Is(err, forge.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}
