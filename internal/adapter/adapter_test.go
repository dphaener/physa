package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestTokensString(t *testing.T) {
	got := Tokens{Input: 12345, CachedInput: 10000, Output: 678}.String()
	if want := "12345 in (10000 cached), 678 out"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if got := (Tokens{}).String(); got != "0 in (0 cached), 0 out" {
		t.Fatalf("zero String() = %q", got)
	}
}

func TestTokensJSON(t *testing.T) {
	b, err := json.Marshal(Tokens{Input: 3, CachedInput: 2, Output: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"input":3,"cached_input":2,"output":1}`; string(b) != want {
		t.Fatalf("Marshal = %s, want %s", b, want)
	}
	var back Tokens
	if err := json.Unmarshal(b, &back); err != nil || back != (Tokens{3, 2, 1}) {
		t.Fatalf("Unmarshal = %+v, %v", back, err)
	}
}

func TestSentinelsDistinct(t *testing.T) {
	wrapped := fmt.Errorf("%w: the agent said no", ErrFailed)
	if !errors.Is(wrapped, ErrFailed) || errors.Is(wrapped, ErrNotInstalled) {
		t.Fatalf("wrapped ErrFailed matches wrongly: %v", wrapped)
	}
	if errors.Is(ErrNotInstalled, ErrFailed) {
		t.Fatal("ErrNotInstalled is ErrFailed")
	}
}

type plain struct{}

func (plain) Name() string { return "plain" }
func (plain) Start(context.Context, Spec) (Session, error) {
	return nil, nil
}

type windowed struct{ plain }

func (windowed) Window() int { return 123 }

func TestWindow(t *testing.T) {
	if got := Window(windowed{}); got != 123 {
		t.Errorf("Window(windowed) = %d, want 123", got)
	}
	if got := Window(plain{}); got != 0 {
		t.Errorf("Window(plain) = %d, want 0", got)
	}
	if got := Window(nil); got != 0 {
		t.Errorf("Window(nil) = %d, want 0", got)
	}
}

func TestTooLongIsFoundBesideFailed(t *testing.T) {
	err := fmt.Errorf("%w: %w: x", ErrFailed, ErrTooLong)
	if !errors.Is(err, ErrFailed) || !errors.Is(err, ErrTooLong) {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(ErrFailed, ErrTooLong) {
		t.Fatal("ErrFailed is ErrTooLong")
	}
}
