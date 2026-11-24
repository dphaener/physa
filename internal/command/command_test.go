package command

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

func TestCompletionCommandIsHidden(t *testing.T) {
	root := tree()
	code, out, errs := execute(root, "completion", "bash")
	if code != 0 || errs != "" || !strings.HasPrefix(out, "# bash completion V2 for physa") {
		t.Fatalf("got %d %q %q", code, firstLine(out), errs)
	}
	found := false
	for _, s := range root.Commands() {
		if s.Name() == "completion" {
			found = true
			if !s.Hidden {
				t.Fatal("the completion command is visible")
			}
		}
	}
	if !found {
		t.Fatal("root has no completion command")
	}
	var b bytes.Buffer
	Help(root, &b)
	if strings.Contains(b.String(), "completion") {
		t.Fatalf("root help lists completion:\n%s", b.String())
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
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

// entries is a custom value kept outside its declaration, so it resets
// itself.
type entries struct{ list []string }

func (e *entries) String() string     { return strings.Join(e.list, ",") }
func (e *entries) Set(s string) error { e.list = append(e.list, s); return nil }
func (e *entries) Type() string       { return "entry" }
func (e *entries) Reset()             { e.list = nil }

// declaredTree builds: physa > dec (with flags), quiet (declared with no
// flags) and plain (never declared). dec prints its flags, its positionals
// and the words typed after its name; it returns BadUsage for more than
// three positionals.
func declaredTree() *cobra.Command {
	var (
		asJSON bool
		slug   string
		limit  int
		tags   []string
		entry  entries
		dec    *cobra.Command
	)
	dec = Declare(New("dec", "a declared command", []string{"physa dec [<arg>...] [--json] [--slug <slug>] [--limit N] [--tag T]... [--entry E]"},
		func(args []string, stdout, _ io.Writer) int {
			if len(args) > 3 {
				return BadUsage
			}
			fmt.Fprintf(stdout, "json=%v slug=%q limit=%d tags=%q entry=%q args=%q words=%q\n",
				asJSON, slug, limit, tags, entry.list, args, Words(dec))
			return 0
		}),
		func(fs *pflag.FlagSet) {
			fs.BoolVar(&asJSON, "json", false, "")
			fs.StringVar(&slug, "slug", "", "")
			fs.IntVar(&limit, "limit", 7, "")
			fs.StringArrayVar(&tags, "tag", nil, "")
			fs.Var(&entry, "entry", "")
		})
	return New("physa", "test root", []string{"physa <command>"}, nil,
		dec,
		Declare(New("quiet", "takes no flags", []string{"physa quiet"}, echo("quiet")), nil),
		New("plain", "never declared", []string{"physa plain [<arg>...]"}, echo("plain")),
	)
}

func decHelp(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	Help(declaredTree().Commands()[0], &b)
	return b.String()
}

func TestDeclaredParsesFlagsAndPassesPositionals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"dec"}, `json=false slug="" limit=7 tags=[] entry=[] args=[] words=[]`},
		{[]string{"dec", "a", "--json", "b", "--slug", "s", "--limit", "3", "c"}, `json=true slug="s" limit=3 tags=[] entry=[] args=["a" "b" "c"] words=["a" "--json" "b" "--slug" "s" "--limit" "3" "c"]`},
		{[]string{"dec", "--slug=s", "--limit=4"}, `json=false slug="s" limit=4 tags=[] entry=[] args=[] words=["--slug=s" "--limit=4"]`},
		{[]string{"dec", "--slug", "x", "--slug", "y"}, `json=false slug="y" limit=7 tags=[] entry=[] args=[] words=["--slug" "x" "--slug" "y"]`},
		{[]string{"dec", "--json=false"}, `json=false slug="" limit=7 tags=[] entry=[] args=[] words=["--json=false"]`},
		{[]string{"dec", "--json=true"}, `json=true slug="" limit=7 tags=[] entry=[] args=[] words=["--json=true"]`},
		{[]string{"dec", "--", "-x", "--json"}, `json=false slug="" limit=7 tags=[] entry=[] args=["-x" "--json"] words=["--" "-x" "--json"]`},
		{[]string{"dec", "--tag", "a", "--tag=b", "--entry", "e"}, `json=false slug="" limit=7 tags=["a" "b"] entry=["e"] args=[] words=["--tag" "a" "--tag=b" "--entry" "e"]`},
	} {
		code, out, errs := execute(declaredTree(), tc.args...)
		if code != 0 || errs != "" || out != tc.want+"\n" {
			t.Errorf("%v: got %d %q %q\nwant %q", tc.args, code, out, errs, tc.want)
		}
	}
}

func TestEveryCommandParsesFlagsWithArbitraryArgs(t *testing.T) {
	root := declaredTree()
	for _, c := range append([]*cobra.Command{root}, root.Commands()...) {
		if c.Args == nil || c.Args(c, []string{"a", "b", "c", "d", "e"}) != nil {
			t.Fatalf("%s: Args do not take any count", c.Name())
		}
		if code, _, _ := execute(root, append(strings.Fields(c.CommandPath())[1:], "--bogus")...); code != 2 {
			t.Fatalf("%s --bogus: got %d", c.CommandPath(), code)
		}
	}
	code, out, errs := execute(root, "dec", "a", "b", "c", "d")
	if code != 2 || out != "" || errs != decHelp(t) {
		t.Fatalf("too many positionals: got %d %q %q", code, out, errs)
	}
}

func TestDeclaredBadFlagsPrintHelpToStderr(t *testing.T) {
	for _, args := range [][]string{
		{"dec", "-h"},
		{"dec", "--bogus"},
		{"dec", "--bogus=1"},
		{"dec", "-x"},
		{"dec", "x", "-j"},
		{"dec", "--slug"},
		{"dec", "--limit", "x"},
		{"dec", "--limit="},
		{"dec", "--json=maybe"},
		{"dec", "--slug="},
		{"dec", "--slug", ""},
		{"dec", "--tag="},
		{"dec", "--tag", "a", "--tag", ""},
		{"dec", "--help=maybe"},
	} {
		code, out, errs := execute(declaredTree(), args...)
		if code != 2 || out != "" || errs != decHelp(t) {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
	code, out, errs := execute(declaredTree(), "quiet", "--json")
	if code != 2 || out != "" || !strings.HasPrefix(errs, "usage: physa quiet\n") {
		t.Errorf("quiet --json: got %d %q %q", code, out, errs)
	}
}

func TestDeclaredHelpWinsAnywhere(t *testing.T) {
	for _, args := range [][]string{
		{"dec", "--help"},
		{"dec", "a", "--help"},
		{"dec", "--slug", "--help"},
		{"dec", "--bogus", "--help"},
		{"dec", "--slug=", "--help"},
		{"dec", "-h", "--help"},
		{"dec", "--", "--help"},
		{"dec", "a", "b", "c", "d", "--help"},
		{"dec", "--help=true"},
	} {
		code, out, errs := execute(declaredTree(), args...)
		if code != 0 || errs != "" || out != decHelp(t) {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}

func TestDeclaredHelpFlagHasNoShorthand(t *testing.T) {
	root := declaredTree()
	execute(root, "dec")
	dec := root.Commands()[0]
	f := dec.Flags().Lookup("help")
	if f == nil || f.Shorthand != "" || !f.Hidden || dec.Flags().ShorthandLookup("h") != nil {
		t.Fatalf("help flag %+v", f)
	}
}

func TestDeclaredHelpMatchesUndeclaredHelp(t *testing.T) {
	var plain bytes.Buffer
	Help(New("dec", "a declared command", []string{"physa dec [<arg>...] [--json] [--slug <slug>] [--limit N] [--tag T]... [--entry E]"}, nil), &plain)
	if got := decHelp(t); got != plain.String() {
		t.Fatalf("got %q, want %q", got, plain.String())
	}
}

func TestDeclaredFlagsResetForEachLine(t *testing.T) {
	root := declaredTree()
	code, out, _ := execute(root, "dec", "--json", "--slug", "s", "--limit", "1", "--tag", "a", "--entry", "e", "x")
	if code != 0 || !strings.HasPrefix(out, `json=true slug="s" limit=1 tags=["a"] entry=["e"]`) {
		t.Fatalf("first line: got %d %q", code, out)
	}
	code, out, _ = execute(root, "dec")
	if want := `json=false slug="" limit=7 tags=[] entry=[] args=[] words=[]` + "\n"; code != 0 || out != want {
		t.Fatalf("second line: got %d %q, want %q", code, out, want)
	}
	code, out, _ = execute(root, "dec", "--tag", "b", "--entry", "f")
	if code != 0 || !strings.Contains(out, `tags=["b"] entry=["f"]`) {
		t.Fatalf("third line: got %d %q", code, out)
	}
	for _, f := range []string{"json", "slug", "limit", "tag", "entry"} {
		execute(root, "plain")
		if root.Commands()[0].Flags().Lookup(f).Changed {
			t.Errorf("--%s still changed", f)
		}
	}
}

// TestUndeclaredCommandTakesNoFlags: a command New built and Declare never
// touched parses like one declared with no flags.
func TestUndeclaredCommandTakesNoFlags(t *testing.T) {
	for _, args := range [][]string{
		{"plain", "--bogus"},
		{"plain", "-h"},
		{"plain", "x", "--json"},
		{"plain", "--help=false"},
		{"--bogus"},
		{"-h"},
	} {
		code, out, errs := execute(declaredTree(), args...)
		if code != 2 || out != "" || errs == "" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
	code, out, errs := execute(declaredTree(), "plain", "x", "--", "-h", "--bogus")
	if code != 0 || out != "plain x,-h,--bogus\n" || errs != "" {
		t.Fatalf("after --: got %d %q %q", code, out, errs)
	}
	code, out, _ = execute(declaredTree(), "plain", "a", "--help")
	if code != 0 || !strings.HasPrefix(out, "usage: physa plain") {
		t.Fatalf("--help: got %d %q", code, out)
	}
	if w := Words(&cobra.Command{Use: "foreign"}); w != nil {
		t.Fatalf("a command New did not build has words %q", w)
	}
}

func TestDeclareNeedsACommandFromNew(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Declare took a command New did not build")
		}
	}()
	Declare(&cobra.Command{Use: "foreign"}, nil)
}

func TestCompletionAnswersFromDeclarations(t *testing.T) {
	code, out, _ := execute(declaredTree(), "__complete", "dec", "--")
	if code != 0 {
		t.Fatalf("got %d %q", code, out)
	}
	for _, f := range []string{"--json", "--slug", "--limit", "--tag", "--entry"} {
		if !strings.Contains(out, f) {
			t.Errorf("no %s in %q", f, out)
		}
	}
	if strings.Contains(out, "--help") {
		t.Errorf("hidden --help completed: %q", out)
	}
}

func TestDrift(t *testing.T) {
	if p := Drift(declaredTree()); len(p) != 0 {
		t.Fatalf("clean tree drifts: %q", p)
	}
	root := New("physa", "", []string{"physa <command> [--help]"}, nil,
		Declare(New("grp", "", []string{"physa grp [--json]", "physa grp sub [--deep]"}, nil,
			Declare(New("sub", "", []string{"physa grp sub [--deep]"}, nil), func(fs *pflag.FlagSet) { fs.Bool("deep", false, "") }),
		), func(fs *pflag.FlagSet) {
			fs.Bool("json", false, "")
			fs.Bool("quiet", false, "")
			fs.Bool("secret", false, "")
			_ = fs.MarkHidden("secret")
		}),
		Declare(New("odd", "", []string{"physa odd [--from <file>|-] [--write [--push]]"}, nil), func(fs *pflag.FlagSet) {
			fs.String("from", "", "")
			fs.Bool("write", false, "")
		}),
		New("plain", "", []string{"physa plain [--anything]"}, nil),
	)
	want := []string{
		"physa grp: declares --quiet, which its usage lines don't name",
		"physa odd: its usage lines name --push, which it doesn't declare",
		"physa plain: its usage lines name --anything, which it doesn't declare",
	}
	if got := Drift(root); !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSwitchTakesNoValue(t *testing.T) {
	var on bool
	root := New("physa", "test root", nil, nil,
		Declare(New("leaf", "a leaf", []string{"physa leaf [--on]"}, func(_ []string, stdout, _ io.Writer) int {
			fmt.Fprintln(stdout, on)
			return 0
		}), func(fs *pflag.FlagSet) { Switch(fs, "on", &on, "") }))
	for _, c := range []struct {
		line []string
		code int
		out  string
	}{
		{[]string{"leaf"}, 0, "false\n"},
		{[]string{"leaf", "--on"}, 0, "true\n"},
		{[]string{"leaf"}, 0, "false\n"},
		{[]string{"leaf", "--on", "--on"}, 0, "true\n"},
		{[]string{"leaf", "--on=false"}, 2, ""},
		{[]string{"leaf", "--on=1"}, 2, ""},
	} {
		code, out, _ := execute(root, c.line...)
		if code != c.code || out != c.out {
			t.Errorf("%v: got %d %q, want %d %q", c.line, code, out, c.code, c.out)
		}
	}
}

func TestDeclaredHelpTakesNoValue(t *testing.T) {
	root := New("physa", "test root", nil, nil,
		Declare(New("leaf", "a leaf", []string{"physa leaf"}, echo("leaf")), nil))
	if code, out, _ := execute(root, "leaf", "--help=true"); code != 0 || !strings.HasPrefix(out, "usage: physa leaf") {
		t.Errorf("--help=true: got %d %q", code, out)
	}
	for _, v := range []string{"--help=false", "--help=1"} {
		if code, out, errs := execute(root, "leaf", v); code != 2 || out != "" || !strings.HasPrefix(errs, "usage: physa leaf") {
			t.Errorf("%s: got %d %q %q", v, code, out, errs)
		}
	}
}
