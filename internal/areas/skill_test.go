package areas

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// capabilityDefinition returns the first two sentences of the glossary's
// Capability entry, which architecture 15 §4 quotes as the definition.
func capabilityDefinition(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../.physa/glossary.md")
	if err != nil {
		t.Fatal(err)
	}
	const head = "**Capability**: "
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, head) {
			continue
		}
		body := strings.TrimPrefix(line, head)
		first := strings.Index(body, ". ")
		if first < 0 {
			t.Fatalf("Capability entry has no first sentence: %q", body)
		}
		second := strings.Index(body[first+2:], ". ")
		if second < 0 {
			t.Fatalf("Capability entry has no second sentence: %q", body)
		}
		return body[:first+2+second+1]
	}
	t.Fatal("glossary has no Capability entry")
	return ""
}

func TestSkillCapabilityDefinitionMatchesGlossary(t *testing.T) {
	def := capabilityDefinition(t)
	if !strings.Contains(Areas, def) {
		t.Errorf("areas.md does not hold the glossary's Capability definition byte for byte:\n%s", def)
	}
}

func TestSkillPartsHoldAcrossParts(t *testing.T) {
	if !strings.Contains(Parts, AcrossParts) {
		t.Errorf("parts.md lacks %q", AcrossParts)
	}
}

func TestSkillsHoldNoPerRepoWords(t *testing.T) {
	placeholder := regexp.MustCompile(`\{[A-Za-z_]+\}|\$\{|\$cap`)
	adr := regexp.MustCompile(`(?i)\bADRs?\b`)
	words := []string{"Discourse", "Ember", "plugin", "Frameworks", "frameworks:"}
	skills := map[string]string{"sources.md": Sources, "areas.md": Areas, "parts.md": Parts, "merge.md": Merge}
	for name, s := range skills {
		if strings.TrimSpace(s) == "" {
			t.Errorf("%s is empty", name)
		}
		if adr.MatchString(s) {
			t.Errorf("%s holds the word ADR", name)
		}
		if placeholder.MatchString(s) {
			t.Errorf("%s holds a placeholder", name)
		}
		for _, w := range words {
			if strings.Contains(s, w) {
				t.Errorf("%s holds %q", name, w)
			}
		}
	}
}

func TestSkillsAskForTheProduct(t *testing.T) {
	for _, want := range []string{"`product`", "`name`", "one sentence on one line: what the product does for its users and why that matters to them", `{"product":{"name":`} {
		if !strings.Contains(Areas, want) {
			t.Errorf("areas.md lacks %q", want)
		}
	}
	if !strings.Contains(Parts, "first part states") || !strings.Contains(Parts, "do not restate") {
		t.Error("parts.md does not say the first part states the product and later parts do not restate it")
	}
	if !strings.Contains(Merge, "product's promise") || !strings.Contains(Merge, "stays as the first part stated it") {
		t.Error("merge.md does not say the product's promise is given and stays as the first part stated it")
	}
}
