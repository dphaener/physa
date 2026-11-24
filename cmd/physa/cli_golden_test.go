package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/cli.golden")

// goldenLines are the command lines pinned by the golden file: help at every
// depth, and bad lines whose outcome is settled before any I/O.
var goldenLines = []string{
	"",
	"bogus",
	"bogus list",
	"--help",
	"help",
	"help extra",
	"version extra",
	"sync",

	"check --help",
	"check map --help",
	"check changes --help",
	"map --help",
	"map index --help",
	"map slice --help",
	"map changes --help",
	"map records --help",
	"map fmt --help",
	"map learn --help",
	"map hooks --help",
	"init --help",
	"idea --help",
	"idea add --help",
	"idea list --help",
	"idea show --help",
	"pursue --help",
	"pursuit --help",
	"pursuit list --help",
	"pursuit show --help",
	"pursuit stop --help",
	"pursuit plan --help",
	"pursuit plan set --help",
	"pursuit plan show --help",
	"pursuit work --help",
	"evidence --help",
	"engine --help",
	"engine run --help",
	"engine start --help",
	"engine status --help",
	"engine stop --help",
	"api --help",
	"api list --help",
	"api describe --help",
	"api call --help",
	"sync --help",
	"version --help",
	"help --help",

	"map slice x --help",
	"map --help index",
	"engine status -h",
	"map -h",
	"pursuit plan",
	"pursuit plan bogus",

	"check a b",
	"check map a b",
	"map index a b",
	"map records --bogus",
	"map fmt a b",
	"map hooks extra",
	"init --bogus",
	"idea list extra",
	"evidence --bogus",
	"engine status extra",
	"engine --bogus",
	"engine status -j",
	"engine start --dir /x",
	"api describe",
	"api call m {} more",
	"pursuit work a/b/c",
	"pursuit list --bogus",
}

// renderGolden runs every golden line and renders the results as blocks.
func renderGolden() string {
	var out strings.Builder
	for _, line := range goldenLines {
		args := strings.Fields(line)
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr)
		fmt.Fprintf(&out, "$ physa %s\nexit %d\n--- stdout\n%s--- stderr\n%s\n", line, code, stdout.String(), stderr.String())
	}
	return out.String()
}

func TestCommandLineGolden(t *testing.T) {
	const path = "testdata/cli.golden"
	got := renderGolden()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("command line differs from %s (run with -update to rewrite):\n%s", path, firstDiff(string(want), got))
	}
}

// firstDiff shows the first differing line of want and got.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d:\n  want %q\n  got  %q", i+1, a, b)
		}
	}
	return "no difference"
}
