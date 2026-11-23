package signal

import (
	"strings"
	"testing"
	"time"
)

var today = time.Date(2026, 11, 23, 15, 0, 0, 0, time.UTC)

const filled = `# Keep ideas

## Claim
<!-- guidance -->
People find an idea again within a day.

## Direction
For: most ideas are shown again
  within a day.
Against: ideas are added and never shown.

## Window
2026-12-15

## Resolver
ann
`

func TestParseReadsEveryPart(t *testing.T) {
	s, err := Parse(filled, today)
	if err != nil {
		t.Fatal(err)
	}
	want := Signal{
		Claim:    "People find an idea again within a day.",
		For:      "most ideas are shown again\n  within a day.",
		Against:  "ideas are added and never shown.",
		Window:   "2026-12-15",
		Resolver: "ann",
	}
	if s != want {
		t.Fatalf("got %#v\nwant %#v", s, want)
	}
}

func TestParseIsLenient(t *testing.T) {
	md := "# t\n## CLAIM \nc <!-- hidden\nacross lines -->\n## direction\nfor: f\nAGAINST: a\n## window\n2026-11-23\n## Resolver\nci-green check\n"
	s, err := Parse(md, today)
	if err != nil {
		t.Fatal(err)
	}
	if s.Claim != "c" || s.For != "f" || s.Against != "a" || s.Window != "2026-11-23" || s.Resolver != "ci-green check" {
		t.Fatalf("got %#v", s)
	}
}

func TestTemplateHasFourPartsInOrderAndFailsEmpty(t *testing.T) {
	tpl := Template("Keep ideas")
	if !strings.HasPrefix(tpl, "# Keep ideas\n") {
		t.Fatalf("title missing: %q", tpl)
	}
	last := -1
	for _, h := range partNames {
		i := strings.Index(tpl, "## "+h+"\n")
		if i <= last {
			t.Fatalf("heading %q missing or out of order", h)
		}
		last = i
	}
	_, err := Parse(tpl, today)
	if err == nil {
		t.Fatal("an empty template parsed")
	}
	for _, w := range []string{`empty part "Claim"`, `empty "For:" in Direction`, `empty "Against:" in Direction`, `empty part "Window"`, `empty part "Resolver"`} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q lacks %q", err, w)
		}
	}
}

func TestEveryProblemOnOneLine(t *testing.T) {
	md := "## Claim\n\n## Direction\nsomething first\nFor: f\nFor: again\n## Window\nnext week\n## Bogus\nb\n## claim\nx\n"
	_, err := Parse(md, today)
	if err == nil {
		t.Fatal("want error")
	}
	msg := err.Error()
	if strings.Contains(msg, "\n") || !strings.HasPrefix(msg, "the signal has problems: ") {
		t.Fatalf("not one line: %q", msg)
	}
	for _, w := range []string{`unknown heading "Bogus"`, `repeated heading "Claim"`, `missing part "Resolver"`, `text in Direction before`,
		`repeated "For:" in Direction`, `empty "Against:" in Direction`, `empty part "Claim"`, `window "next week" is not a date`} {
		if !strings.Contains(msg, w) {
			t.Errorf("error %q lacks %q", msg, w)
		}
	}
	if strings.Contains(msg, `empty part "Resolver"`) {
		t.Errorf("a missing part is also reported empty: %q", msg)
	}
}

func TestWindowBeforeTodayIsRefused(t *testing.T) {
	md := strings.Replace(filled, "2026-12-15", "2026-11-22", 1)
	_, err := Parse(md, today)
	if err == nil || !strings.Contains(err.Error(), "window 2026-11-22 is before today, 2026-11-23") {
		t.Fatalf("got %v", err)
	}
}

func TestCheck(t *testing.T) {
	ok := Signal{Claim: "c", For: "f", Against: "a", Window: "2026-11-23", Resolver: "ann"}
	if ps := Check(ok, today); len(ps) != 0 {
		t.Fatalf("a full signal has problems: %v", ps)
	}
	ps := Check(Signal{Claim: " ", Window: "2026-02-30"}, today)
	want := []string{`empty part "Claim"`, `empty "For:" in Direction`, `empty "Against:" in Direction`,
		`window "2026-02-30" is not a date: write it as YYYY-MM-DD`, `empty part "Resolver"`}
	if strings.Join(ps, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", ps, want)
	}
}
