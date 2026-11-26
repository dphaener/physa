package main

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/skillcli"
)

func runSkill(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestSkillShowPrintsTheGuide(t *testing.T) {
	code, stdout, stderr := runSkill("skill", "show")
	if code != 0 || stdout != skillcli.Guide() || stderr != "" {
		t.Errorf("skill show: exit %d, guide equal %v, stderr %q", code, stdout == skillcli.Guide(), stderr)
	}
}

func TestSkillShowWithAnArgumentIsBadUsage(t *testing.T) {
	code, stdout, stderr := runSkill("skill", "show", "extra")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "physa skill show") {
		t.Errorf("skill show extra: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestBareSkillPrintsUsage(t *testing.T) {
	code, stdout, stderr := runSkill("skill")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "physa skill show") {
		t.Errorf("skill: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

var wordPattern = regexp.MustCompile(`^[a-z]+$`)

// TestGuideCommandsExist runs --help on every command the guide names.
func TestGuideCommandsExist(t *testing.T) {
	spans := regexp.MustCompile("`(physa [^`]*)`").FindAllStringSubmatch(skillcli.Guide(), -1)
	seen := map[string]bool{}
	for _, m := range spans {
		var words []string
		for _, tok := range strings.Fields(m[1])[1:] {
			if !wordPattern.MatchString(tok) {
				break
			}
			words = append(words, tok)
		}
		key := strings.Join(words, " ")
		if len(words) == 0 || seen[key] {
			continue
		}
		seen[key] = true
		cmd, rest, err := rootCommand().Find(words)
		if err != nil || len(rest) != 0 || cmd.CommandPath() != "physa "+key {
			// A trailing positional word (a command's own argument) is not a command.
			t.Errorf("the guide names `physa %s`, which is not a command (found %q, left over %q, err %v)", key, cmd.CommandPath(), rest, err)
			continue
		}
		code, stdout, stderr := runSkill(append(slices.Clone(words), "--help")...)
		if code != 0 || stdout == "" {
			t.Errorf("physa %s --help: exit %d, stdout %q, stderr %q", key, code, stdout, stderr)
		}
	}
	if len(seen) < 8 {
		t.Errorf("the guide names %d distinct commands, want at least 8", len(seen))
	}
}
