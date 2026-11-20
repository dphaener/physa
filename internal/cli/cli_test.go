package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// tree builds: physa (no Run) > grp (Run echoes) > leaf, bad; and grp2 (no Run) > x.
func tree() *Command {
	echo := func(name string) func([]string, io.Writer, io.Writer) int {
		return func(args []string, stdout, _ io.Writer) int {
			fmt.Fprintf(stdout, "%s %s\n", name, strings.Join(args, ","))
			return 0
		}
	}
	return &Command{
		Name:    "physa",
		Summary: "test root",
		Usage:   []string{"physa <command>"},
		Subs: []*Command{
			{
				Name:    "grp",
				Summary: "a group with a bare job",
				Usage:   []string{"physa grp [arg]", "physa grp leaf"},
				Run:     echo("grp"),
				Subs: []*Command{
					{Name: "leaf", Summary: "a leaf", Usage: []string{"physa grp leaf [dir]"}, Run: echo("leaf")},
					{Name: "bad", Summary: "always bad", Usage: []string{"physa grp bad"}, Run: func([]string, io.Writer, io.Writer) int { return BadUsage }},
					{Name: "fail", Summary: "fails", Run: func([]string, io.Writer, io.Writer) int { return 1 }},
				},
			},
			{
				Name:    "grp2",
				Summary: "a group without a bare job",
				Subs:    []*Command{{Name: "x", Summary: "x leaf", Run: echo("x")}},
			},
		},
	}
}

func dispatch(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Dispatch(tree(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestDescendIntoSub(t *testing.T) {
	code, out, errs := dispatch("grp", "leaf", "a", "b")
	if code != 0 || out != "leaf a,b\n" || errs != "" {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}

func TestBareGroupRunsItsJob(t *testing.T) {
	code, out, _ := dispatch("grp")
	if code != 0 || out != "grp \n" {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestUnknownSubOfGroupWithRunPassesArgs(t *testing.T) {
	code, out, _ := dispatch("grp", "nonsense", "more")
	if code != 0 || out != "grp nonsense,more\n" {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestUnknownSubOfGroupWithoutRunPrintsHelp(t *testing.T) {
	code, out, errs := dispatch("grp2", "nonsense")
	if code != 2 || out != "" || !strings.Contains(errs, "Commands:") || !strings.Contains(errs, "x leaf") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}

func TestUnknownCommandAtRootPrintsHelp(t *testing.T) {
	code, out, errs := dispatch("index")
	if code != 2 || out != "" || !strings.Contains(errs, "usage: physa <command>") || !strings.Contains(errs, "grp2") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}

func TestEmptyArgsAtRootPrintsHelp(t *testing.T) {
	code, out, errs := dispatch()
	if code != 2 || out != "" || !strings.Contains(errs, "Commands:") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}

func TestHelpFlagAtRootGroupAndLeaf(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "usage: physa <command>"},
		{[]string{"grp", "--help"}, "usage: physa grp [arg]"},
		{[]string{"grp", "leaf", "--help"}, "usage: physa grp leaf [dir]"},
		{[]string{"grp", "leaf", "x", "--help"}, "usage: physa grp leaf [dir]"},
	} {
		code, out, errs := dispatch(tc.args...)
		if code != 0 || errs != "" || !strings.Contains(out, tc.want) {
			t.Errorf("%v: got %d %q %q", tc.args, code, out, errs)
		}
	}
}

func TestHelpFlagBeatsRun(t *testing.T) {
	_, out, _ := dispatch("grp", "--help")
	if strings.Contains(out, "grp \n") {
		t.Fatalf("Run was called: %q", out)
	}
}

func TestBadUsagePrintsHelpToStderrAndReturns2(t *testing.T) {
	code, out, errs := dispatch("grp", "bad")
	if code != 2 || out != "" || !strings.Contains(errs, "usage: physa grp bad") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}

func TestRunCodePassesThrough(t *testing.T) {
	code, _, errs := dispatch("grp", "fail")
	if code != 1 || errs != "" {
		t.Fatalf("got %d %q", code, errs)
	}
}

func TestNoPrefixOrShortFlag(t *testing.T) {
	if code, _, _ := dispatch("gr"); code != 2 {
		t.Fatalf("prefix matched: %d", code)
	}
	if code, out, _ := dispatch("-h"); code != 2 || out != "" {
		t.Fatalf("-h accepted: %d %q", code, out)
	}
}

func TestHelpLayout(t *testing.T) {
	var b bytes.Buffer
	Help(tree().Subs[0], &b)
	want := "usage: physa grp [arg]\n" +
		"       physa grp leaf\n" +
		"\n" +
		"a group with a bare job\n" +
		"\n" +
		"Commands:\n" +
		"  leaf  a leaf\n" +
		"  bad   always bad\n" +
		"  fail  fails\n"
	if b.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", b.String(), want)
	}
}

func TestHelpWithoutUsageDerivesFromName(t *testing.T) {
	var b bytes.Buffer
	Help(&Command{Name: "solo", Summary: "alone", Run: nil}, &b)
	if !strings.HasPrefix(b.String(), "usage: solo\n") {
		t.Fatalf("got %q", b.String())
	}
}
