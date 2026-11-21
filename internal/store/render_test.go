package store

import (
	"bytes"
	"testing"
	"time"
)

func TestWriteRecords(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		recs   []SliceRecord
		asJSON bool
		want   string
	}{
		{"none", nil, false, "No slice records.\n"},
		{"none json", nil, true, "[]\n"},
		{"lines", []SliceRecord{
			{At: at, Capabilities: []string{"a", "b"}, With: "all", Session: "s1"},
			{At: at, Capabilities: []string{"c"}},
			{At: at, Entries: []string{"a", "b"}, Session: "s2"},
			{At: at, Capabilities: []string{"c"}, Entries: []string{"d"}},
		}, false, "2026-11-01T12:00:00Z s1 a,b with all\n2026-11-01T12:00:00Z — c\n2026-11-01T12:00:00Z s2 entries a, b\n2026-11-01T12:00:00Z — c entries d\n"},
	}
	for _, c := range cases {
		var b bytes.Buffer
		if err := WriteRecords(&b, c.recs, c.asJSON); err != nil {
			t.Fatal(err)
		}
		if b.String() != c.want {
			t.Errorf("%s: got %q, want %q", c.name, b.String(), c.want)
		}
	}
}
