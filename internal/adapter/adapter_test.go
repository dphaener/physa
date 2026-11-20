package adapter

import (
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
