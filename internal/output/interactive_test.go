package output

import (
	"os"
	"testing"
)

func TestInteractiveRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		inTTY    bool
		outTTY   bool
		ci       string
		agent    bool
		wantTrue bool
	}{
		{"person at a terminal", true, true, "", false, true},
		{"input is a pipe", false, true, "", false, false},
		{"output is a pipe", true, false, "", false, false},
		{"CI set", true, true, "true", false, false},
		{"agent running", true, true, "", true, false},
		{"nothing is a terminal", false, false, "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := interactive(c.inTTY, c.outTTY, c.ci, c.agent); got != c.wantTrue {
				t.Errorf("interactive(%v, %v, %q, %v) = %v, want %v",
					c.inTTY, c.outTTY, c.ci, c.agent, got, c.wantTrue)
			}
		})
	}
}

func TestInteractiveDevNullIsNotATerminal(t *testing.T) {
	t.Parallel()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if Interactive(null, null, func(string) string { return "" }, false) {
		t.Error("Interactive on /dev/null in and out = true, want false")
	}
}

func TestInteractiveCIAndAgentFromCallers(t *testing.T) {
	t.Parallel()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	getenv := func(k string) string {
		if k == "CI" {
			return "1"
		}
		return ""
	}
	if Interactive(null, null, getenv, true) {
		t.Error("Interactive with CI and agent set = true, want false")
	}
}
