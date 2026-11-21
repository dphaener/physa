package history

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteChangesText(t *testing.T) {
	commits := []Commit{
		{Short: "abc1234", Subject: "feat: x", Capabilities: []string{"a", "b"}},
		{Short: "def5678", Subject: "fix: y"},
	}
	var b bytes.Buffer
	if err := WriteChanges(&b, "r", commits, nil, false); err != nil {
		t.Fatal(err)
	}
	want := "abc1234 feat: x\n  capability: a, b\ndef5678 fix: y\n  capability: —\n\nphysa map changes: ok (2 commits)\n"
	if b.String() != want {
		t.Errorf("got %q", b.String())
	}
}

func TestWriteChangesFindings(t *testing.T) {
	var b bytes.Buffer
	f := []Finding{{Commit: "abc1234", Message: "no capability"}}
	if err := WriteChanges(&b, "r", nil, f, false); err != nil {
		t.Fatal(err)
	}
	if want := "\n1 finding:\nabc1234: no capability\n"; b.String() != want {
		t.Errorf("got %q", b.String())
	}
}

func TestWriteChangesJSON(t *testing.T) {
	var b bytes.Buffer
	if err := WriteChanges(&b, "a..b", []Commit{{Short: "x"}}, nil, true); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Range   string
		Commits []Commit
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil || got.Range != "a..b" || len(got.Commits) != 1 {
		t.Errorf("got %q, %v", b.String(), err)
	}
}

func TestSlugListDash(t *testing.T) {
	if SlugList(nil) != "—" || SlugList([]string{"a", "b"}) != "a, b" {
		t.Error("SlugList")
	}
}

func TestWriteChangesFindingsPlural(t *testing.T) {
	var b bytes.Buffer
	f := []Finding{{Commit: "abc1234", Message: "a"}, {Commit: "def5678", Message: "b"}}
	if err := WriteChanges(&b, "r", nil, f, false); err != nil || !strings.Contains(b.String(), "\n2 findings:\n") {
		t.Errorf("err %v, got %q", err, b.String())
	}
}
