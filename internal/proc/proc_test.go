package proc

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func homeKey() string {
	switch runtime.GOOS {
	case "windows":
		return "USERPROFILE"
	case "plan9":
		return "home"
	}
	return "HOME"
}

func TestGetwd(t *testing.T) {
	t.Parallel()
	boom := errors.New("gone")
	got, err := State{Wd: "/w"}.Getwd()
	if got != "/w" || err != nil {
		t.Errorf("Getwd = %q, %v", got, err)
	}
	got, err = State{WdErr: boom}.Getwd()
	if got != "" || !errors.Is(err, boom) {
		t.Errorf("Getwd with WdErr = %q, %v", got, err)
	}
}

func TestGetenvAndLookupEnv(t *testing.T) {
	t.Parallel()
	s := State{Env: []string{"A=1", "EMPTY=", "B=x=y"}}
	if got := s.Getenv("A"); got != "1" {
		t.Errorf("Getenv(A) = %q", got)
	}
	if got := s.Getenv("B"); got != "x=y" {
		t.Errorf("Getenv(B) = %q", got)
	}
	if v, ok := s.LookupEnv("EMPTY"); !ok || v != "" {
		t.Errorf("LookupEnv(EMPTY) = %q, %v", v, ok)
	}
	if v, ok := s.LookupEnv("MISSING"); ok || v != "" {
		t.Errorf("LookupEnv(MISSING) = %q, %v", v, ok)
	}
	if got := s.Getenv("MISS"); got != "" {
		t.Errorf("a prefix of a key matched: %q", got)
	}
}

func TestDuplicateKeyLastWins(t *testing.T) {
	t.Parallel()
	s := State{Env: []string{"A=1", "B=2", "A=3"}}
	if got := s.Getenv("A"); got != "3" {
		t.Errorf("Getenv(A) = %q, want 3", got)
	}
	if v, ok := s.LookupEnv("A"); !ok || v != "3" {
		t.Errorf("LookupEnv(A) = %q, %v", v, ok)
	}
}

func TestEnvironIsACopy(t *testing.T) {
	t.Parallel()
	s := State{Env: []string{"A=1"}}
	got := s.Environ()
	got[0] = "A=2"
	_ = append(got, "B=1")
	if s.Env[0] != "A=1" || len(s.Env) != 1 {
		t.Errorf("Environ shares storage with Env: %v", s.Env)
	}
	if !slices.Equal(s.Environ(), []string{"A=1"}) {
		t.Errorf("Environ = %v", s.Environ())
	}
}

func TestHome(t *testing.T) {
	t.Parallel()
	got, err := State{Env: []string{homeKey() + "=/h"}}.Home()
	if got != "/h" || err != nil {
		t.Errorf("Home = %q, %v", got, err)
	}
}

func TestHomeUnset(t *testing.T) {
	t.Parallel()
	for _, env := range [][]string{nil, {homeKey() + "="}} {
		got, err := State{Env: env}.Home()
		if got != "" || err == nil {
			t.Errorf("Home with %v = %q, %v; want an error", env, got, err)
		}
	}
}

func TestAbs(t *testing.T) {
	t.Parallel()
	wd := t.TempDir()
	s := State{Wd: wd}
	abs := filepath.Join(wd, "a", "..", "b")
	if got, err := s.Abs(abs); err != nil || got != filepath.Join(wd, "b") {
		t.Errorf("Abs(abs) = %q, %v", got, err)
	}
	if got, err := s.Abs("x/../y"); err != nil || got != filepath.Join(wd, "y") {
		t.Errorf("Abs(rel) = %q, %v", got, err)
	}
}

func TestAbsWithWdErr(t *testing.T) {
	t.Parallel()
	boom := errors.New("gone")
	s := State{WdErr: boom}
	got, err := s.Abs("x")
	if got != "" || !errors.Is(err, boom) {
		t.Errorf("Abs(rel) with WdErr = %q, %v; want the error", got, err)
	}
	abs := filepath.Join(t.TempDir(), "k")
	if got, err := s.Abs(abs); err != nil || got != abs {
		t.Errorf("Abs(abs) with WdErr = %q, %v", got, err)
	}
}

func TestMatchesTheProcess(t *testing.T) {
	t.Parallel()
	wd, wdErr := os.Getwd()
	s := State{Dir: ".", Wd: wd, WdErr: wdErr, Env: os.Environ()}
	if got, want := s.Getenv("PATH"), os.Getenv("PATH"); got != want {
		t.Errorf("Getenv(PATH) = %q, want %q", got, want)
	}
	gotHome, gotErr := s.Home()
	wantHome, wantErr := os.UserHomeDir()
	if gotHome != wantHome || (gotErr == nil) != (wantErr == nil) {
		t.Errorf("Home = %q, %v; os.UserHomeDir = %q, %v", gotHome, gotErr, wantHome, wantErr)
	}
	gotAbs, gotErr := s.Abs("x")
	wantAbs, wantErr := filepath.Abs("x")
	if gotAbs != wantAbs || (gotErr == nil) != (wantErr == nil) {
		t.Errorf("Abs = %q, %v; filepath.Abs = %q, %v", gotAbs, gotErr, wantAbs, wantErr)
	}
}
