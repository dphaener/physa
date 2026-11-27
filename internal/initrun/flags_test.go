package initrun

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dphaener/physa/internal/command"
)

var now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// execute runs "physa init" with args through init's declared command, as
// cmd/physa builds it, with job as the command's job. It returns job's own
// code, or command.BadUsage when the line never reached job (a bad flag or
// an empty value); its help then went to stderr.
func execute(args []string, stdout, stderr io.Writer, job func(f Flags, pos, words []string, stdout, stderr io.Writer) int) int {
	var f Flags
	var c *cobra.Command
	code := command.BadUsage
	c = command.Declare(command.New("init", "", Usage, func(pos []string, stdout, stderr io.Writer) int {
		code = job(f, pos, command.Words(c), stdout, stderr)
		return code
	}), f.Declare)
	command.Execute(command.New("physa", "", nil, nil, c), append([]string{"init"}, args...), stdout, stderr)
	return code
}

// parseLine reads args as init's declared command does, and reports whether
// the line is a good one.
func parseLine(args []string, now time.Time) (Flags, bool) {
	var got Flags
	ok := false
	execute(args, io.Discard, io.Discard, func(f Flags, pos, _ []string, _, _ io.Writer) int {
		ok = f.read(pos, now)
		got = f
		return 0
	})
	return got, ok
}

func TestDeclaredFlagsMatchUsage(t *testing.T) {
	root := command.New("physa", "", nil, nil, command.Declare(command.New("init", "", Usage, nil), new(Flags).Declare))
	if problems := command.Drift(root); len(problems) > 0 {
		t.Errorf("drift:\n%s", strings.Join(problems, "\n"))
	}
}

func TestLineKeepsTheWordsTyped(t *testing.T) {
	var got []string
	args := []string{"--write", "--agent=fake", "--only", "a,b", "--recover"}
	execute(args, io.Discard, io.Discard, func(_ Flags, _, words []string, _, _ io.Writer) int {
		got = words
		return 0
	})
	if strings.Join(got, " ") != strings.Join(args, " ") {
		t.Errorf("words %q, want %q", got, args)
	}
}

func TestLineFreshForEachLine(t *testing.T) {
	if _, ok := parseLine([]string{"--recover", "--only", "a"}, now); !ok {
		t.Fatal("recover --only a refused")
	}
	f, ok := parseLine([]string{"--gather"}, now)
	if !ok || len(f.Only) > 0 || f.Recover || f.SinceSet {
		t.Errorf("second line: %+v %v", f, ok)
	}
}

func TestLineDefaults(t *testing.T) {
	f, ok := parseLine([]string{"--gather"}, now)
	if !ok || !f.Gather || f.SinceSet || f.JSON || f.MaxPRs != 0 { // 0: no PR limit
		t.Fatalf("got %+v %v", f, ok)
	}
	if want := now.AddDate(-1, 0, 0); !f.Since.Equal(want) {
		t.Errorf("since %v, want %v", f.Since, want)
	}
}

// TestMaxPRsHelpStatesTheDefault checks that the flag's own help says a bare
// gather has no PR limit.
func TestMaxPRsHelpStatesTheDefault(t *testing.T) {
	fs := pflag.NewFlagSet("init", pflag.ContinueOnError)
	new(Flags).Declare(fs)
	if got, want := fs.Lookup("max-prs").Usage, "gather at most this many merged PRs (default: every PR of the bound)"; got != want {
		t.Errorf("--max-prs help %q, want %q", got, want)
	}
}

func TestLineValues(t *testing.T) {
	for _, args := range [][]string{
		{"--gather", "--since", "2026-01-02", "--max-prs", "5", "--json"},
		{"--since=2026-01-02", "--max-prs=5", "--gather", "--json"},
	} {
		f, ok := parseLine(args, now)
		want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
		if !ok || !f.Gather || !f.SinceSet || !f.Since.Equal(want) || f.MaxPRs != 5 || !f.JSON {
			t.Errorf("%v: got %+v %v", args, f, ok)
		}
	}
}

func TestLineGatherAgent(t *testing.T) {
	for _, args := range [][]string{
		{"--gather", "--agent", "x"},
		{"--agent=x", "--gather", "--json"},
	} {
		f, ok := parseLine(args, now)
		if !ok || !f.Gather || f.Agent != "x" {
			t.Errorf("%v: got %+v %v", args, f, ok)
		}
	}
	for _, args := range [][]string{
		{"--gather", "--appetite", "1000"},
		{"--gather", "--yes"},
		{"--gather", "--agent="},
		{"--gather", "--write"},
		{"--gather", "--write", "--push"},
		{"--gather", "--write", "--push", "--pr"},
	} {
		if _, ok := parseLine(args, now); ok {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestLineRejections(t *testing.T) {
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
		{"--gather", "--", "extra"},
		{"--gather=1"},
		{"--gather=false"},
		{"-h"},
		{"--gather", "--agent", ""},
	} {
		if _, ok := parseLine(args, now); ok {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestLinePropose(t *testing.T) {
	f, ok := parseLine([]string{"--propose", "--agent=claude", "--json"}, now)
	if !ok || !f.Propose || f.Gather || f.Agent != "claude" || !f.JSON {
		t.Fatalf("got %+v %v", f, ok)
	}
	f, ok = parseLine([]string{"--propose"}, now)
	if !ok || f.Agent != "" {
		t.Errorf("bare propose: %+v %v", f, ok)
	}
}

func TestLineAreas(t *testing.T) {
	f, ok := parseLine([]string{"--areas", "--agent=claude", "--json"}, now)
	if !ok || !f.Areas || f.Full() || f.Agent != "claude" || !f.JSON {
		t.Fatalf("got %+v %v", f, ok)
	}
	for _, args := range [][]string{{"--gather"}, {"--propose"}, {"--recover"}, {"--write"}, {"--write", "--push"}, {"--hooks"},
		{"--answers", "a"}, {"--yes"}, {"--only", "a"}, {"--since", "2026-01-02"}, {"--max-prs", "5"}} {
		if _, ok := parseLine(append([]string{"--areas"}, args...), now); ok {
			t.Errorf("--areas %v accepted", args)
		}
	}
}

func TestLineProposeRejections(t *testing.T) {
	for _, args := range [][]string{
		{"--propose", "--gather"},
		{"--gather", "--propose"},
		{"--propose", "--since", "2026-01-02"},
		{"--propose", "--max-prs", "5"},
		{"--propose", "--appetite", "5000"},
		{"--propose", "--appetite"},
		{"--propose", "--yes"},
		{"--propose", "--agent"},
		{"--propose", "--agent="},
		{"--propose=1"},
	} {
		if _, ok := parseLine(args, now); ok {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestLineRecover(t *testing.T) {
	for _, args := range [][]string{
		{"--recover"},
		{"--recover", "--only", "a", "--only=b,c", "--agent", "x", "--json"},
		{"--only", "a", "--recover"},
	} {
		f, ok := parseLine(args, now)
		if !ok || !f.Recover || f.Gather || f.Propose {
			t.Errorf("%v: got %+v %v", args, f, ok)
		}
	}
	f, _ := parseLine([]string{"--recover", "--only", "a", "--only=b,c"}, now)
	if got := strings.Join(f.Only, "|"); got != "a|b|c" {
		t.Errorf("only %q", got)
	}
}

func TestLineRecoverRejections(t *testing.T) {
	for _, args := range [][]string{
		{"--recover", "--gather"},
		{"--recover", "--propose"},
		{"--recover", "--since", "2026-01-02"},
		{"--recover", "--max-prs", "5"},
		{"--recover", "--appetite", "5000"},
		{"--recover", "--yes"},
		{"--only", "a"},
		{"--propose", "--only", "a"},
		{"--recover", "--only"},
		{"--recover", "--only="},
		{"--recover", "--only", "a,,b"},
		{"--recover", "--only", " , a"},
	} {
		if _, ok := parseLine(args, now); ok {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestLineFull(t *testing.T) {
	f, ok := parseLine(nil, now)
	if !ok || !f.Full() || f.Write || f.Yes {
		t.Fatalf("bare: %+v %v", f, ok)
	}
	f, ok = parseLine([]string{"--write", "--push", "--pr", "--hooks", "--yes", "--agent=x",
		"--answers", "a.txt", "--since", "2026-01-02", "--max-prs=5", "--json"}, now)
	if !ok || !f.Full() || !f.Write || !f.Push || !f.PR || !f.Hooks || !f.Yes || f.Agent != "x" ||
		f.Answers != "a.txt" || !f.SinceSet || f.MaxPRs != 5 || !f.JSON {
		t.Errorf("every flag: %+v %v", f, ok)
	}
	f, ok = parseLine([]string{"--recover", "--only", "a", "--write", "--push", "--pr", "--hooks"}, now)
	if !ok || f.Full() || !f.Recover || !f.Write || !f.Push || !f.PR || !f.Hooks {
		t.Errorf("recover --write: %+v %v", f, ok)
	}
}

func TestLineFullRejections(t *testing.T) {
	for _, args := range [][]string{
		{"--push"},
		{"--write", "--pr"},
		{"--pr"},
		{"--hooks"},
		{"--write", "--propose"},
		{"--write", "--gather"},
		{"--answers", "a.txt", "--recover"},
		{"--answers", "a.txt", "--gather"},
		{"--answers"},
		{"--answers="},
		{"--write=1"},
		{"--appetite", "300000"},
		{"--yes=1"},
		{"--recover", "--push"},
		{"--recover", "--hooks"},
	} {
		if _, ok := parseLine(args, now); ok {
			t.Errorf("%v accepted", args)
		}
	}
}
