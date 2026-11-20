package main

import (
	"bytes"
	"testing"
)

func TestRunVersionForms(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-version"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{arg}, &stdout, &stderr)
		if code != 0 {
			t.Errorf("run(%q) = %d, want 0", arg, code)
		}
		if got, want := stdout.String(), "physa dev\n"; got != want {
			t.Errorf("run(%q) stdout = %q, want %q", arg, got, want)
		}
		if stderr.Len() != 0 {
			t.Errorf("run(%q) stderr = %q, want empty", arg, stderr.String())
		}
	}
}

func TestRunNoArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("run(nil) = %d, want 2", code)
	}
	if got, want := stderr.String(), "usage: physa version\n"; got != want {
		t.Errorf("run(nil) stderr = %q, want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("run(nil) stdout = %q, want empty", stdout.String())
	}
}

func TestRunUnknownArgPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("run(bogus) = %d, want 2", code)
	}
	if got, want := stderr.String(), "usage: physa version\n"; got != want {
		t.Errorf("run(bogus) stderr = %q, want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("run(bogus) stdout = %q, want empty", stdout.String())
	}
}
