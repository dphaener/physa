package mapfolder

import (
	"reflect"
	"strings"
	"testing"
)

const unsafeText = "---\npromise: Webhooks reach customers.\n---\n\n# Delivery\n\n" +
	"## Rules\n\n### Sends are *queued*\n`queued` · from a.md\nBody.\nBecause: b.\n\n" +
	"### Contents\n`contents` · from a.md\nBody.\nBecause: b.\n\n" +
	"## Decisions\n\n### Six attempts\n`six` · 2026-01-02 · abc1234\nBody.\n\n" +
	"### Six attempts\n`six-again` · 2026-01-02 · abc1234\nBody.\n\n" +
	"### Six: attempts\n`six-colon` · 2026-01-02 · abc1234\nBody.\n\n" +
	"### Retries\n`retries` · 2026-01-02 · abc1234\n```\n### not a heading\n```\n"

func TestSafeFilesDropsUnsafeEntries(t *testing.T) {
	f, err := Split(unsafeText)
	if err != nil {
		t.Fatal(err)
	}
	got, drops := SafeFiles(f.Files)
	var heads []string
	for _, d := range drops {
		heads = append(heads, d.Kind.String()+" "+d.Heading)
		if d.Reason == "" {
			t.Errorf("drop %q has no reason", d.Heading)
		}
	}
	want := []string{"rule Sends are *queued*", "rule Contents", "decision Six attempts", "decision Six: attempts"}
	if !reflect.DeepEqual(heads, want) {
		t.Errorf("drops = %q, want %q", heads, want)
	}
	if _, ok := got[RulesFile]; ok {
		t.Errorf("rules.md kept with no entries:\n%s", got[RulesFile])
	}
	wantDecisions := "# Delivery · Decisions\n\n## Contents\n\n- [Six attempts](#six-attempts)\n- [Retries](#retries)\n\n## Entries\n\n" +
		"### Six attempts\n`six` · 2026-01-02 · abc1234\nBody.\n\n" +
		"### Retries\n`retries` · 2026-01-02 · abc1234\n```\n### not a heading\n```\n"
	if got[DecisionsFile] != wantDecisions {
		t.Errorf("decisions.md =\n%s\nwant\n%s", got[DecisionsFile], wantDecisions)
	}
	if ix := got[IndexFile]; strings.Contains(ix, RulesFile) || !strings.Contains(ix, IndexLink(Decision)) {
		t.Errorf("index.md links do not follow the kind files:\n%s", ix)
	}
}

func TestSafeFilesKeepsASafeFolder(t *testing.T) {
	f, err := Split(read(t, "testdata/init.md"))
	if err != nil {
		t.Fatal(err)
	}
	got, drops := SafeFiles(f.Files)
	if len(drops) != 0 || !reflect.DeepEqual(got, f.Files) {
		t.Errorf("a safe folder changed: drops %+v", drops)
	}
}

func TestSafeHeading(t *testing.T) {
	for in, want := range map[string]string{
		"Should <b> be *bold*?": "Should b be bold?",
		"[Draft] plans":         "Draft plans",
		"Plain one":             "Plain one",
	} {
		if got := SafeHeading(in); got != want || HeadingProblem(got) != "" {
			t.Errorf("SafeHeading(%q) = %q, want %q", in, got, want)
		}
	}
}
