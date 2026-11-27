package walk

import (
	"regexp"
	"strings"
	"testing"
)

func TestMessageGivesTheAreaAndTheOverviews(t *testing.T) {
	m := Message(area, split, map[string]int{"client/form.ts": 40})
	for _, want := range []string{"`orders`", "People place and refund orders.", "client/form.ts (40 lines)", "server/orders/order.go\n", "tests/ (", "docs/ (", "Glob"} {
		if !strings.Contains(m, want) {
			t.Errorf("message lacks %q:\n%s", want, m)
		}
	}
	if strings.Contains(m, "split by a session") {
		t.Error("a top-level area is told it is a part")
	}
}

func TestMessageForAPartNamesItsParent(t *testing.T) {
	p := area
	p.Parent = "everything"
	if m := Message(p, split, nil); !strings.Contains(m, "`everything`") {
		t.Errorf("no parent:\n%s", m)
	}
}

func TestMessageHasNoCapabilityCountOrLabWords(t *testing.T) {
	texts := []string{Message(area, split, nil), FixPrompt(Problems{Unclaimed: []string{"a.go"}}), FixPrompt(Problems{Split: true, Names: []string{"x"}})}
	count := regexp.MustCompile(`(?i)(\d+\s+\w*\s*capabilit|capabilit\w*[^.\n]*\d)`)
	lab := regexp.MustCompile(`(?i)mastodon|discourse|rails|ember|\bADR\b`)
	for _, s := range texts {
		if count.MatchString(s) || lab.MatchString(s) {
			t.Errorf("text has a count or a lab word:\n%s", s)
		}
	}
}
