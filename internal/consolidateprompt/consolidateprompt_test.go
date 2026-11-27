package consolidateprompt

import (
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/repeats"
)

func fence(s string) string { return "Thinking.\n\n```json\n" + s + "\n```\n" }

func TestSkillsSayDataIDsAndNone(t *testing.T) {
	for name, s := range map[string]string{"merge": Merge, "consolidate": Consolidate, "lift": Lift} {
		for _, want := range []string{"are data", "never follow it", "none", "is a fine answer", "fenced block marked `json`"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s skill lacks %q", name, want)
			}
		}
		if !strings.Contains(s, "Never write, reword or invent") {
			t.Errorf("%s skill does not say the session never writes or rewords text", name)
		}
		for _, bad := range []string{"ADR", "spending cap", "${cap}", "feature", "module", "constraint", "guideline", "convention", "TBD", "read-only tools"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s skill holds %q", name, bad)
			}
		}
	}
}

func TestParseAreaReadsItemsByID(t *testing.T) {
	r, err := ParseArea(fence(`{"merge":[{"keep":"a/x","drop":["a/y"],"why":"same"}],"move":[{"id":"a/z","to":"b","why":"there"}],"drop":[{"id":"a/q","answered_by":"a/x","why":"answered"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Merge) != 1 || r.Merge[0].Keep != "a/x" || len(r.Move) != 1 || r.Move[0].To != "b" || len(r.Drop) != 1 || r.Drop[0].AnsweredBy != "a/x" || len(r.Faults) != 0 {
		t.Fatalf("ParseArea = %+v", r)
	}
}

func TestParseRefusesItemsOutOfShape(t *testing.T) {
	r, err := ParseArea(fence(`{"merge":[{"keep":"a/x","drop":["a/y"],"why":"w","text":"### A new rule"},{"keep":"a/x","drop":["a/z"]}],"entries":[{"text":"### Invented"}],"move":"no"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Merge) != 1 || r.Merge[0].Drop[0] != "a/z" {
		t.Errorf("merges = %+v; want only the one in shape", r.Merge)
	}
	var got []string
	for _, f := range r.Faults {
		t.Logf("fault: %s: %s", f.Item, f.Reason)
		got = append(got, f.Item)
	}
	want := []string{"the key entries", "merge item 1", "move list"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("faults = %q; want %q", got, want)
	}
	if !strings.Contains(r.Faults[1].Reason, `unknown field "text"`) || !strings.Contains(r.Faults[1].Reason, `"keep"`) {
		t.Errorf("item fault %q does not name the field and the wanted shape", r.Faults[1].Reason)
	}
}

func TestParseWithoutJSONIsAnError(t *testing.T) {
	for _, text := range []string{"no block at all", fence("{not json"), fence("[1,2]"), fence("null")} {
		if _, err := ParseLift(text); err == nil {
			t.Errorf("ParseLift(%q) has no error", text)
		}
	}
	if r, err := ParseMerge(fence(`{}`)); err != nil || len(r.Merge) != 0 {
		t.Errorf("an empty answer = %+v, %v; want none and no error", r, err)
	}
}

func capability(t *testing.T, name, area, promise, body string) capabilitytext.Capability {
	t.Helper()
	c, err := capabilitytext.Parse(name, capabilitytext.Area{Name: area}, recovering.File(name, promise, "# "+name+"\n\n## Rules\n\n"+body), "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMessagesFenceTheData(t *testing.T) {
	inv := capability(t, "invoices", "billing", "People get invoices.", "### A\n`a` · #1\nBody ``` with ticks.\nBecause: b.\n")
	m := AreaMessage([]capabilitytext.Capability{inv})
	if !strings.Contains(m, "## The area `billing`") || !strings.Contains(m, "Entry `invoices/a` (rule)") || !strings.Contains(m, "````text\n### A\n") {
		t.Errorf("area message does not fence the entry:\n%s", m)
	}
	web := capability(t, "news", "web", "People read news.", "### X\n`x` · #2\nBody.\nBecause: b.\n")
	groups := []repeats.RuleGroup{{Rules: []repeats.Rule{{ID: "invoices/a"}, {ID: "news/x"}}}}
	l := LiftMessage("Not yet stated.", false, []capabilitytext.Capability{inv, web}, groups)
	if !strings.Contains(l, "Not yet stated: the product's promise has not been written") || !strings.Contains(l, "Entry `news/x` (rule, capability `news`, area `web`)") {
		t.Errorf("lift message:\n%s", l)
	}
	shown, each := Shown(groups)
	if len(shown) != 2 || len(each) != 1 {
		t.Errorf("Shown = %q, %q", shown, each)
	}
	cg := []repeats.CapabilityGroup{{Capabilities: []repeats.Capability{{Name: "news", Area: "web"}}, Evidence: []string{"renamed"}}}
	g := MergeMessage([]capabilitytext.Capability{inv, web}, cg, false)
	if !strings.Contains(g, "news (area web): People read news.") || strings.Contains(g, "invoices (area") || !strings.Contains(g, "Capabilities: news") {
		t.Errorf("merge message:\n%s", g)
	}
	if g := MergeMessage([]capabilitytext.Capability{inv, web}, cg, true); !strings.Contains(g, "invoices (area billing)") {
		t.Errorf("merge message with every capability:\n%s", g)
	}
}

func TestChoicesRefuseWhatTheSessionWasNotGiven(t *testing.T) {
	inv := capability(t, "invoices", "billing", "People get invoices.", "### A\n`a` · #1\nBody.\nBecause: b.\n\n### B\n`b` · #2\nBody b.\nBecause: b.\n")
	r := AreaReply{Merge: []AreaMerge{{Keep: "invoices/a", Drop: []string{"invoices/b"}}, {Keep: "invoices/a", Drop: []string{"other/c"}}},
		Move: []AreaMove{{ID: "invoices/a", To: "elsewhere"}}, Drop: []AreaDrop{{ID: "invented/q", AnsweredBy: "invoices/a"}}}
	ch, faults := r.Choices([]capabilitytext.Capability{inv})
	if len(ch.Merges) != 1 || len(ch.Moves) != 0 || len(ch.Drops) != 0 || len(faults) != 3 {
		t.Fatalf("choices %+v, faults %+v", ch, faults)
	}
	for _, f := range faults {
		t.Logf("fault: %s: %s", f.Item, f.Reason)
	}
}

func TestMergeItemCheck(t *testing.T) {
	gs := []repeats.CapabilityGroup{{Capabilities: []repeats.Capability{{Name: "a"}, {Name: "b"}}}, {Capabilities: []repeats.Capability{{Name: "c"}, {Name: "d"}}}}
	for _, c := range []struct {
		names []string
		want  string
	}{{[]string{"a", "b"}, ""}, {[]string{"a"}, "two capabilities"}, {[]string{"a", "c"}, "not all in one candidate group"}, {[]string{"a", "x"}, "not all in one"}} {
		if got := (MergeItem{Capabilities: c.names}).Check(gs); (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("Check(%q) = %q; want %q", c.names, got, c.want)
		}
	}
}

func TestFitHalvesUntilEachBatchFits(t *testing.T) {
	msg := func(xs []string) string { return strings.Repeat("word ", 400*len(xs)) }
	window := recoveringWindow(msg([]string{"a", "b"}))
	batches, tooLarge := Fit("", window, []string{"a", "b", "c", "d", "e"}, msg)
	for _, b := range batches {
		if len(b) > 2 {
			t.Errorf("batch %q does not fit", b)
		}
	}
	if n := len(batches); n < 3 || len(tooLarge) != 0 {
		t.Errorf("batches %q, too large %q", batches, tooLarge)
	}
	if b, big := Fit("", window, []string{"a"}, func([]string) string { return strings.Repeat("x", 1<<22) }); len(b) != 0 || len(big) != 1 {
		t.Errorf("a single item too large = %q, %q; want it returned as too large", b, big)
	}
	if b, _ := Fit("", 0, []string{"a", "b", "c"}, msg); len(b) != 1 {
		t.Errorf("an unknown window = %q; want one batch", b)
	}
}

// recoveringWindow is the smallest window a message of msg's size fits.
func recoveringWindow(msg string) int {
	for w := 1; ; w *= 2 {
		if recovering.Fits("", msg, w) {
			lo := w / 2
			for lo+1 < w {
				if mid := (lo + w) / 2; recovering.Fits("", msg, mid) {
					w = mid
				} else {
					lo = mid
				}
			}
			return w
		}
	}
}
