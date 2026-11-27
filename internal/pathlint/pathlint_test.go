package pathlint

import (
	"reflect"
	"strings"
	"testing"
)

func TestFind(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []Hit
	}{
		{"body", "# Cap\n\nIt lives in internal/x/y.go and more.\n", []Hit{{3, "internal/x/y.go"}}},
		{"heading", "# Cap\n\n### Reads internal/x/y.go first\n", []Hit{{3, "internal/x/y.go"}}},
		{"because", "### Rule\n`rule` · from #4\nBody.\nBecause: see docs/guide.md for why.\n", []Hit{{4, "docs/guide.md"}}},
		{"bare name", "### Rule\n`rule` · from #4\nThe file user.rb holds it.\n", []Hit{{3, "user.rb"}}},
		{"anchor on meta line", "### Rule\n`rule` · from #4, from internal/x.go\nBody.\n", nil},
		{"anchor after blank line", "### Rule\n\n`rule` · from internal/x.go\n", nil},
		{"second meta-like line is linted", "### Rule\n`rule` · from #4\n`a/b.go` is odd\nx a/b.go\n", []Hit{{4, "a/b.go"}}},
		{"url", "See https://example.com/docs/x.md or http://a.io/b.go now.\n", nil},
		{"version", "Since v1.2 and 3.5 and 1.2/3.4 it works; and/or not.\n", nil},
		{"words", "Reads and/or writes, either/or.\n", nil},
		{"front matter", "---\nsource: a/b.md\n---\n# Cap\n", nil},
		{"fenced", "# Cap\n```\na/b.go\n```\nc/d.go\n", []Hit{{5, "c/d.go"}}},
		{"tilde fence", "~~~\na/b.go\n~~~\n", nil},
		{"two on one line", "a/b.go and c/d.md\n", []Hit{{1, "a/b.go"}, {1, "c/d.md"}}},
		{"trailing period", "Edit docs/x.md.\n", []Hit{{1, "docs/x.md"}}},
		{"backticked path is the lab's blind spot", "Uses `a/b.go` here.\n", nil},
		{"token start is a word", "xinternal/a.go\n", []Hit{{1, "xinternal/a.go"}}},
		{"not a front matter later", "# Cap\n---\na/b.go\n", []Hit{{3, "a/b.go"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Find(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Find = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage(t *testing.T) {
	got := Message("a/b.go")
	for _, want := range []string{"names the file path `a/b.go`", "`from <path>` anchor", "never in a heading, body or Because line"} {
		if !strings.Contains(got, want) {
			t.Errorf("Message = %q, missing %q", got, want)
		}
	}
}
