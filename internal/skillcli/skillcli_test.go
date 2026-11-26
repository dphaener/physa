package skillcli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/command"
)

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := command.Execute(command.New("physa", "", nil, nil, Command()), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestShowPrintsTheGuide(t *testing.T) {
	code, out, errs := run("skill", "show")
	if code != 0 || out != Guide() || errs != "" {
		t.Fatalf("code %d, stdout matches %v, stderr %q", code, out == Guide(), errs)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatal("the guide has escape codes")
	}
}

func TestShowWithAnArgumentIsBadUsage(t *testing.T) {
	code, out, errs := run("skill", "show", "extra")
	if code != 2 || out != "" || !strings.Contains(errs, showUsage) {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errs)
	}
}

// termLines are the guide's "- **Term**: ..." lines under its terms section.
func termLines(g string) []string {
	_, rest, _ := strings.Cut(g, "## Physa's terms\n")
	rest, _, _ = strings.Cut(rest, "\n## ")
	var lines []string
	for _, l := range strings.Split(rest, "\n") {
		if strings.HasPrefix(l, "- **") {
			lines = append(lines, l)
		}
	}
	return lines
}

func glossary(t *testing.T) []string {
	b, err := os.ReadFile("../../.physa/glossary.md")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(b), "\n")
}

func TestEveryListedTermIsInTheGlossary(t *testing.T) {
	g := glossary(t)
	lines := termLines(guide)
	if len(lines) == 0 {
		t.Fatal("the guide lists no terms")
	}
	for _, l := range lines {
		name, rest, ok := strings.Cut(strings.TrimPrefix(l, "- **"), "**: ")
		if !ok {
			t.Errorf("not in the form - **Term**: meaning: %q", l)
			continue
		}
		at := -1
		for i, gl := range g {
			if strings.HasPrefix(gl, "**"+name+"**: ") {
				at = i
			}
		}
		if at < 0 {
			t.Errorf("%s is not a glossary term", name)
			continue
		}
		_, avoid, has := strings.Cut(rest, "_Avoid_: ")
		if !has {
			continue
		}
		glossaryAvoid := ""
		if at+1 < len(g) && strings.HasPrefix(g[at+1], "_Avoid_: ") {
			glossaryAvoid = strings.TrimPrefix(g[at+1], "_Avoid_: ")
		}
		for _, w := range strings.Split(strings.TrimSuffix(avoid, "."), ", ") {
			if !strings.Contains(glossaryAvoid, w) {
				t.Errorf("%s: %q is not in the glossary's Avoid list", name, w)
			}
		}
	}
}

func TestGuideNeverSaysADR(t *testing.T) {
	if strings.Contains(strings.ToUpper(guide), "ADR") {
		t.Fatal("the guide says ADR")
	}
}

func TestGuideDoesNotCopyTheSliceSkill(t *testing.T) {
	b, err := os.ReadFile("../mapslice/skill.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- "))
		if l != "" && strings.Contains(guide, l) {
			t.Errorf("the guide copies the slice skill: %q", l)
		}
	}
}
