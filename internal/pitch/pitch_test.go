package pitch

import (
	"reflect"
	"strings"
	"testing"
)

func full() Pitch {
	return Pitch{
		Problem:     "Ideas get lost.\n\nThey live in chat.",
		Appetite:    1_500_000,
		Solution:    "Keep them in the store.",
		Scope:       "Adding and listing only.",
		Signal:      "People shape the idea within a day.",
		RabbitHoles: "- Slug clashes\n- Editors that fork",
		NoGos:       "No delete command.",
	}
}

func TestRoundTrip(t *testing.T) {
	p := full()
	got, err := Parse(Template("Keep ideas", &p))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip changed the pitch:\n got %#v\nwant %#v", got, p)
	}
}

func TestTemplateEmptyHasSevenPartsInOrder(t *testing.T) {
	tpl := Template("Keep ideas", nil)
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
	if strings.Count(tpl, "\n## ") != 7 {
		t.Fatalf("want seven parts in %q", tpl)
	}
	_, err := Parse(tpl)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("an empty template must fail as empty parts, got %v", err)
	}
}

func TestParseLenient(t *testing.T) {
	md := "# Title\n\n## PROBLEM \nA <!-- hidden\nacross lines --> problem\n## appetite\n200K\n## Solution\ns\n## Scope\nc\n## Signal\ng\n## Rabbit Holes\nr\n## no-gos\nn\n"
	p, err := Parse(md)
	if err != nil {
		t.Fatal(err)
	}
	if p.Problem != "A  problem" || p.Appetite != 200_000 || p.RabbitHoles != "r" || p.NoGos != "n" {
		t.Fatalf("got %#v", p)
	}
}

func TestMissingPartsListedTogether(t *testing.T) {
	_, err := Parse("## Problem\np\n## Appetite\n1k\n## Solution\ns\n## Scope\nc\n## Signal\ng\n")
	if err == nil {
		t.Fatal("want error")
	}
	for _, w := range []string{"Rabbit holes", "No-gos", "missing"} {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("error %q lacks %q", err, w)
		}
	}
}

func TestEmptyPart(t *testing.T) {
	md := Template("x", &Pitch{Problem: "p", Appetite: 5, Solution: "s", Scope: "c", Signal: "g", RabbitHoles: "r", NoGos: "n"})
	md = strings.Replace(md, "\nc\n", "\n<!-- only a comment -->\n", 1)
	_, err := Parse(md)
	if err == nil || !strings.Contains(err.Error(), "Scope") || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("got %v", err)
	}
}

func TestUnknownHeading(t *testing.T) {
	p := full()
	_, err := Parse(Template("x", &p) + "\n## Spec\nstuff\n")
	if err == nil || !strings.Contains(err.Error(), "unknown") || !strings.Contains(err.Error(), "Spec") {
		t.Fatalf("got %v", err)
	}
}

func TestRepeatedHeading(t *testing.T) {
	p := full()
	_, err := Parse(Template("x", &p) + "\n## solution\nagain\n")
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("got %v", err)
	}
}

func TestAppetiteBad(t *testing.T) {
	p := full()
	md := strings.Replace(Template("x", &p), "\n1.5m\n", "\nlots\n", 1)
	_, err := Parse(md)
	if err == nil || !strings.Contains(err.Error(), "appetite") || !strings.Contains(err.Error(), "lots") {
		t.Fatalf("got %v", err)
	}
}

func TestAllErrorsTogether(t *testing.T) {
	md := "## Problem\n\n## Appetite\nlots\n## Solution\ns\n## Solution\nt\n## Bogus\nb\n"
	_, err := Parse(md)
	if err == nil {
		t.Fatal("want error")
	}
	for _, w := range []string{"Problem", "empty", "lots", "repeated", "Bogus", "Scope", "Signal", "Rabbit holes", "No-gos", "missing"} {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("error %q lacks %q", err, w)
		}
	}
}

func TestTokens(t *testing.T) {
	good := map[string]int64{
		"200k": 200_000, "200K": 200_000, "1.5m": 1_500_000, "2M": 2_000_000,
		"1,500,000": 1_500_000, "1_000": 1000, "42": 42, "0.5k": 500, "2.25k": 2250,
		" 3m ": 3_000_000,
	}
	for in, want := range good {
		got, err := ParseTokens(in)
		if err != nil || got != want {
			t.Errorf("ParseTokens(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "0k", "-5", "k", "1.5", "1.2345k", "1.2.3m", "abc", "1x", "99999999999999999999", "9999999999999m"} {
		if got, err := ParseTokens(in); err == nil {
			t.Errorf("ParseTokens(%q) = %d, want error", in, got)
		}
	}
}

func TestTokensFormat(t *testing.T) {
	cases := map[int64]string{
		200_000: "200k", 1_500_000: "1.5m", 2_000_000: "2m", 1_050_000: "1050k",
		1234: "1234", 500: "500", 1000: "1k", 999_999: "999999",
	}
	for n, want := range cases {
		if got := Tokens(n); got != want {
			t.Errorf("Tokens(%d) = %q, want %q", n, got, want)
		}
		if back, err := ParseTokens(Tokens(n)); err != nil || back != n {
			t.Errorf("Tokens(%d) does not parse back: %d, %v", n, back, err)
		}
	}
}
