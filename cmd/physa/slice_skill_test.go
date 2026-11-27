package main

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
)

// skillMap builds a temp repo whose capability a has rules, a decision and a
// question, so a bare slice lists headings the skill can send an agent to.
func skillMap(t *testing.T) string {
	t.Helper()
	root := tempMap(t, "`r` · from docs/d.md")
	front := "---\npromise: x\n---\n# T\n\n"
	folder(t, root, ".physa/capabilities/a", front+
		"## Rules\n\n### R\n`r` · from docs/d.md\nBody.\nBecause: y.\n\n"+
		"## Decisions\n\n### Chose X\n`chose-x` · decided by dana, accepting a proposal · 2026-03-02 · from docs/d.md\nWe chose X.\nBecause: y.\nRejected:\n- Z: no.\n\n"+
		"## Questions\n\n### Which Y\n`which-y` · raised by dana · 2026-03-02 · from docs/d.md\nOpen.\nBecause: y.\n")
	return root
}

func TestSliceSkillIsPrintedAndItsCommandsRun(t *testing.T) {
	t.Parallel()
	skill, err := os.ReadFile("../../internal/mapslice/skill.md")
	if err != nil {
		t.Fatal(err)
	}
	d := depsIn(t, skillMap(t))
	var out, errb bytes.Buffer
	if code := run(t.Context(), d, []string{"map", "slice", "a"}, &out, &errb); code != 0 {
		t.Fatalf("bare slice = %d; stderr %q", code, errb.String())
	}
	_, after, ok := strings.Cut(out.String(), "## Reading further\n\n")
	if !ok {
		t.Fatalf("slice has no Reading further section:\n%s", out.String())
	}
	if !strings.HasPrefix(after, strings.TrimRight(string(skill), "\n")+"\n") {
		t.Errorf("Reading further = %q, want the embedded skill %q", after, skill)
	}
	slugs := regexp.MustCompile(`(?m)^- ([a-z0-9-]+): `).FindAllStringSubmatch(out.String(), -1)
	if len(slugs) < 2 {
		t.Fatalf("slice lists %d heading lines, want at least 2:\n%s", len(slugs), out.String())
	}
	cmds := regexp.MustCompile("`physa map slice ([^`]+)`").FindAllStringSubmatch(string(skill), -1)
	if len(cmds) == 0 {
		t.Fatal("the skill names no physa map slice command")
	}
	for _, c := range cmds {
		var args []string
		n := 0
		for _, tok := range strings.Fields(c[1]) {
			if rest, more := strings.CutSuffix(tok, "..."); more && n > 0 {
				tok = rest // "<slug>..." repeats the slug already given
				if tok != "" {
					continue
				}
			}
			switch tok {
			case "<slug>":
				args = append(args, slugs[n%len(slugs)][1])
				n++
			case "<capability>":
				args = append(args, "a")
			case "...":
			default:
				args = append(args, tok)
			}
		}
		var o, e bytes.Buffer
		if code := run(t.Context(), d, append([]string{"map", "slice"}, args...), &o, &e); code != 0 {
			t.Errorf("the skill's command %q ran as %v = %d; stderr %q", c[0], args, code, e.String())
		}
		if o.Len() == 0 {
			t.Errorf("the skill's command %q printed nothing", c[0])
		}
	}
}
