package github

import (
	"errors"
	"testing"

	"github.com/dphaener/physa/internal/forge"
)

func TestLogin(t *testing.T) {
	fakeGH(t, "echo dphaener\n")
	got, err := New(t.TempDir()).Login(t.Context())
	if err != nil || got != "dphaener" {
		t.Errorf("Login = %q, %v", got, err)
	}
}

func TestLoginAsksForTheUser(t *testing.T) {
	f := &fake{out: " dphaener \n"}
	got, err := NewWithRunner(".", f.run).Login(t.Context())
	if err != nil || got != "dphaener" {
		t.Fatalf("Login = %q, %v", got, err)
	}
	if len(f.calls) != 1 || len(f.calls[0]) != 4 || f.calls[0][0] != "api" || f.calls[0][1] != "user" || f.calls[0][3] != ".login" {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestLoginNoGH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := New(t.TempDir()).Login(t.Context()); !errors.Is(err, forge.ErrUnavailable) {
		t.Errorf("no gh: %v", err)
	}
}

func TestLoginUnauthenticated(t *testing.T) {
	fakeGH(t, "echo nope >&2\nexit 4\n")
	if _, err := New(t.TempDir()).Login(t.Context()); !errors.Is(err, forge.ErrUnauthenticated) {
		t.Errorf("exit 4: %v", err)
	}
}
