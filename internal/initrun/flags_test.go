package initrun

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func TestParseFlagsDefaults(t *testing.T) {
	f, ok := ParseFlags([]string{"--gather"}, now)
	if !ok || !f.Gather || f.SinceSet || f.JSON || f.MaxPRs != 1000 {
		t.Fatalf("got %+v %v", f, ok)
	}
	if want := now.AddDate(-1, 0, 0); !f.Since.Equal(want) {
		t.Errorf("since %v, want %v", f.Since, want)
	}
}

func TestParseFlagsValues(t *testing.T) {
	for _, args := range [][]string{
		{"--gather", "--since", "2026-01-02", "--max-prs", "5", "--json"},
		{"--since=2026-01-02", "--max-prs=5", "--gather", "--json"},
	} {
		f, ok := ParseFlags(args, now)
		want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
		if !ok || !f.Gather || !f.SinceSet || !f.Since.Equal(want) || f.MaxPRs != 5 || !f.JSON {
			t.Errorf("%v: got %+v %v", args, f, ok)
		}
	}
}

func TestParseFlagsRejections(t *testing.T) {
	for _, args := range [][]string{
		{"--gather", "--since", "yesterday"},
		{"--gather", "--since", "2026-13-01"},
		{"--gather", "--since"},
		{"--gather", "--since="},
		{"--gather", "--max-prs", "0"},
		{"--gather", "--max-prs=-3"},
		{"--gather", "--max-prs", "x"},
		{"--gather", "--max-prs"},
		{"--gather", "--bogus"},
		{"--gather", "extra"},
		{"--gather=1"},
	} {
		if _, ok := ParseFlags(args, now); ok {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestParseFlagsPropose(t *testing.T) {
	f, ok := ParseFlags([]string{"--propose", "--appetite", "5000", "--agent=claude", "--json"}, now)
	if !ok || !f.Propose || f.Gather || f.Appetite != 5000 || f.Agent != "claude" || !f.JSON {
		t.Fatalf("got %+v %v", f, ok)
	}
	f, ok = ParseFlags([]string{"--propose"}, now)
	if !ok || f.Appetite != 0 || f.Agent != "" {
		t.Errorf("bare propose: %+v %v", f, ok)
	}
}

func TestParseFlagsProposeRejections(t *testing.T) {
	for _, args := range [][]string{
		{"--propose", "--gather"},
		{"--gather", "--propose"},
		{"--propose", "--since", "2026-01-02"},
		{"--propose", "--max-prs", "5"},
		{"--propose", "--appetite", "0"},
		{"--propose", "--appetite=-1"},
		{"--propose", "--appetite", "x"},
		{"--propose", "--appetite"},
		{"--propose", "--agent"},
		{"--propose", "--agent="},
		{"--appetite", "5000"},
		{"--gather", "--appetite", "5000"},
		{"--agent", "claude"},
		{"--propose=1"},
	} {
		if _, ok := ParseFlags(args, now); ok {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestParseFlagsRecover(t *testing.T) {
	for _, args := range [][]string{
		{"--recover"},
		{"--recover", "--only", "a", "--only=b,c", "--appetite", "5000", "--agent", "x", "--json"},
		{"--only", "a", "--recover"},
	} {
		f, ok := ParseFlags(args, now)
		if !ok || !f.Recover || f.Gather || f.Propose {
			t.Errorf("%v: got %+v %v", args, f, ok)
		}
	}
	f, _ := ParseFlags([]string{"--recover", "--only", "a", "--only=b,c"}, now)
	if got := strings.Join(f.Only, "|"); got != "a|b|c" {
		t.Errorf("only %q", got)
	}
}

func TestParseFlagsRecoverRejections(t *testing.T) {
	for _, args := range [][]string{
		{"--recover", "--gather"},
		{"--recover", "--propose"},
		{"--recover", "--since", "2026-01-02"},
		{"--recover", "--max-prs", "5"},
		{"--only", "a"},
		{"--propose", "--only", "a"},
		{"--recover", "--only"},
		{"--recover", "--only="},
		{"--recover", "--only", "a,,b"},
		{"--gather", "--appetite", "5"},
	} {
		if _, ok := ParseFlags(args, now); ok {
			t.Errorf("%v accepted", args)
		}
	}
}
