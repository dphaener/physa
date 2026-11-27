package walk

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const capabilityHead = "**Capability**: "

// glossaryCapability returns the Capability entry of a glossary's text.
func glossaryCapability(glossary string) (string, bool) {
	for _, line := range strings.Split(glossary, "\n") {
		if strings.HasPrefix(line, capabilityHead) {
			return strings.TrimPrefix(line, capabilityHead), true
		}
	}
	return "", false
}

// checkDrift fails unless the glossary's Capability entry begins with the
// definition the skill carries.
func checkDrift(entry string) error {
	if !strings.HasPrefix(entry, CapabilityDefinition) {
		return fmt.Errorf("the glossary's Capability entry does not begin with the skill's definition:\n%s", entry)
	}
	return nil
}

func TestSkillDriftCapabilityDefinitionMatchesGlossary(t *testing.T) {
	b, err := os.ReadFile("../../.physa/glossary.md")
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := glossaryCapability(string(b))
	if !ok {
		t.Fatal("glossary has no Capability entry")
	}
	if err := checkDrift(entry); err != nil {
		t.Error(err)
	}
	if !strings.Contains(Walk, CapabilityDefinition) {
		t.Error("walk.md does not hold the capability definition byte for byte")
	}
}

func TestSkillDriftFailsWhenTheEntryDiffers(t *testing.T) {
	entry := CapabilityDefinition + " Established ideas become part of a capability."
	if err := checkDrift(entry); err != nil {
		t.Fatalf("an entry beginning with the definition failed: %v", err)
	}
	altered := strings.Replace(entry, "rely on", "depend on", 1)
	if altered == entry {
		t.Fatal("the alteration changed nothing")
	}
	if err := checkDrift(altered); err == nil {
		t.Error("an altered entry passed the drift check")
	}
}

func TestSkillsHoldNoPerRepoWords(t *testing.T) {
	placeholder := regexp.MustCompile(`\{[A-Za-z_]+\}|\$\{|\$cap`)
	adr := regexp.MustCompile(`(?i)\bADRs?\b`)
	count := regexp.MustCompile(`(?i)\d.*(capabilit|\bparts?\b)|(capabilit|\bparts?\b).*\d`)
	words := []string{"Discourse", "Ember", "Rails", "Mastodon", "RSpec", "Minitest", "Jest", "Sidekiq", "plugin", "framework", "{users}", "{styles}", "{test_names}"}
	for name, s := range map[string]string{"walk.md": Walk, "split.md": Split} {
		if strings.TrimSpace(s) == "" {
			t.Errorf("%s is empty", name)
		}
		if adr.MatchString(s) {
			t.Errorf("%s names ADR", name)
		}
		for _, w := range words {
			if strings.Contains(s, w) {
				t.Errorf("%s holds the per-repo word %q", name, w)
			}
		}
		for i, line := range strings.Split(s, "\n") {
			if placeholder.MatchString(line) {
				t.Errorf("%s line %d holds a placeholder: %s", name, i+1, line)
			}
			if count.MatchString(line) {
				t.Errorf("%s line %d pairs a number with capabilities or parts: %s", name, i+1, line)
			}
		}
	}
}

func TestSkillSplitNamesNoMaximum(t *testing.T) {
	if !strings.Contains(Split, "two or more parts") || !strings.Contains(Split, "never split again") {
		t.Error("split.md lacks two or more parts or the rule that parts are not split again")
	}
	if strings.Contains(Split, "between") {
		t.Error("split.md gives a range")
	}
}

func TestSkillNothingReadsTheGlossaryAtRunTime(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(b)), "glossary") {
			t.Errorf("%s mentions the glossary", f)
		}
	}
}
