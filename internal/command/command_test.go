package command

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func echo(name string) Run {
	return func(args []string, stdout, _ io.Writer) int {
		fmt.Fprintf(stdout, "%s %s\n", name, strings.Join(args, ","))
		return 0
	}
}

// tree builds: physa (no Run) > grp (Run echoes) > leaf, bad, fail, three;
// and grp2 (no Run) > x.
func tree() *cobra.Command {
	return New("physa", "test root", []string{"physa <command>"}, nil,
		New("grp", "a group with a bare job", []string{"physa grp [arg]", "physa grp leaf"}, echo("grp"),
			New("leaf", "a leaf", []string{"physa grp leaf [dir]"}, echo("leaf")),
			New("bad", "always bad", []string{"physa grp bad"}, func([]string, io.Writer, io.Writer) int { return BadUsage }),
			New("fail", "fails", nil, func([]string, io.Writer, io.Writer) int { return 1 }),
			New("three", "exits 3", nil, func([]string, io.Writer, io.Writer) int { return 3 }),
		),
		New("grp2", "a group without a bare job", nil, nil,
			New("x", "x leaf", nil, echo("x")),
		),
	)
}

func execute(root *cobra.Command, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Execute(root, args, &out, &errb)
	return code, out.String(), errb.String()
}

func dispatch(args ...string) (int, string, string) {
	return execute(tree(), args...)
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

func TestExitCodeThreePassesThrough(t *testing.T) {
	code, out, errs := dispatch("grp", "three")
	if code != 3 || out != "" || errs != "" {
		t.Fatalf("got %d %q %q", code, out, errs)
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
	Help(tree().Commands()[0], &b)
	want := "usage: physa grp [arg]\n" +
		"       physa grp leaf\n" +
		"\n" +
		"a group with a bare job\n" +
		"\n" +
		"Commands:\n" +
		"  leaf   a leaf\n" +
		"  bad    always bad\n" +
		"  fail   fails\n" +
		"  three  exits 3\n"
	if b.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", b.String(), want)
	}
}

func TestHelpWithoutUsageDerivesFromName(t *testing.T) {
	var b bytes.Buffer
	Help(New("solo", "alone", nil, nil), &b)
	if !strings.HasPrefix(b.String(), "usage: solo\n") {
		t.Fatalf("got %q", b.String())
	}
}

func TestNoCompletionCommand(t *testing.T) {
	root := tree()
	code, out, errs := execute(root, "completion", "bash")
	if code != 2 || out != "" || !strings.HasPrefix(errs, "usage: physa <command>") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	for _, s := range root.Commands() {
		if s.Name() == "completion" {
			t.Fatal("root has a completion command")
		}
	}
}

func TestNoHelpCommandUnlessDeclared(t *testing.T) {
	code, out, errs := dispatch("help")
	if code != 2 || out != "" || strings.Contains(errs, "Help about any command") || strings.Contains(errs, "  help") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}

// helpTree declares help as the root's last command, as physa does.
func helpTree() *cobra.Command {
	root := New("physa", "test root", []string{"physa <command>"}, nil,
		New("zeta", "z", nil, echo("zeta")),
		New("alpha", "a", nil, echo("alpha")),
	)
	root.AddCommand(New("help", "Print the commands.", []string{"physa help"}, func(args []string, stdout, _ io.Writer) int {
		if len(args) > 0 {
			return BadUsage
		}
		Help(root, stdout)
		return 0
	}))
	return root
}

func TestHelpStaysWhereDeclared(t *testing.T) {
	code, out, errs := execute(helpTree(), "help")
	want := "usage: physa <command>\n\ntest root\n\nCommands:\n" +
		"  zeta   z\n" +
		"  alpha  a\n" +
		"  help   Print the commands.\n"
	if code != 0 || out != want || errs != "" {
		t.Fatalf("got %d %q %q\nwant %q", code, out, errs, want)
	}
	code, out, errs = execute(helpTree(), "help", "extra")
	if code != 2 || out != "" || !strings.HasPrefix(errs, "usage: physa help\n") {
		t.Fatalf("help extra: got %d %q %q", code, out, errs)
	}
}

func TestSubsKeepDeclarationOrder(t *testing.T) {
	var names []string
	for _, s := range helpTree().Commands() {
		names = append(names, s.Name())
	}
	if got := strings.Join(names, ","); got != "zeta,alpha,help" {
		t.Fatalf("order %s", got)
	}
}

func TestNilArgsDoNotReadOSArgs(t *testing.T) {
	old := os.Args
	os.Args = []string{"physa", "grp", "leaf", "from-os"}
	t.Cleanup(func() { os.Args = old })
	var out, errb bytes.Buffer
	code := Execute(tree(), nil, &out, &errb)
	if code != 2 || out.String() != "" || !strings.HasPrefix(errb.String(), "usage: physa <command>") {
		t.Fatalf("got %d %q %q", code, out.String(), errb.String())
	}
}
