package learnbranch

import "testing"

func TestParseFlagsBadUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--since"}, {"--nosuch"}, {"x"}, {"--appetite", "0"}, {"--appetite=x"}, {"--appetite"},
		{"--appetite=-3"}, {"--agent"}, {"--agent="}, {"--signals", "--appetite", "5"}, {"--signals", "--agent=claude"},
		{"--push"}, {"--pr"}, {"--write", "--pr"}, {"--signals", "--write"}, {"--write=x"},
	} {
		if _, ok := ParseFlags(args); ok {
			t.Errorf("ParseFlags(%v) ok, want not", args)
		}
	}
	o, ok := ParseFlags([]string{"--signals", "--since", "abc", "--json"})
	if !ok || o.Since != "abc" || !o.JSON || !o.Signals {
		t.Errorf("got %+v, %v", o, ok)
	}
	o, ok = ParseFlags([]string{"--write", "--push", "--pr", "--appetite=9"})
	if !ok || !o.Write || !o.Push || !o.PR || o.Appetite != 9 {
		t.Errorf("got %+v, %v", o, ok)
	}
}
