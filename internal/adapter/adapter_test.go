package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
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

func TestToolsPerFence(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want []string
		none bool // want is nil
	}{
		{"prompt-only", Spec{PromptOnly: true}, []string{}, false},
		{"read-only", Spec{ReadOnly: true}, []string{"Read", "Glob", "Grep"}, false},
		{"read-only and prompt-only", Spec{ReadOnly: true, PromptOnly: true}, []string{}, false},
		{"unfenced", Spec{}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.spec.Tools()
			if c.none {
				if got != nil {
					t.Fatalf("Tools() = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("Tools() = nil, want a non-nil list")
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Tools() = %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestToolsNeverWrite(t *testing.T) {
	banned := []string{"Write", "Edit", "Bash", "NotebookEdit"}
	for _, s := range []Spec{{ReadOnly: true}, {PromptOnly: true}, {ReadOnly: true, PromptOnly: true}} {
		for _, tool := range s.Tools() {
			if slices.Contains(banned, tool) {
				t.Errorf("fenced spec %+v lists %s in Tools()", s, tool)
			}
		}
	}
	for _, tool := range banned {
		if slices.Contains((Spec{ReadOnly: true}).Tools(), tool) {
			t.Errorf("ReadOnly Tools() holds %s", tool)
		}
	}
}
