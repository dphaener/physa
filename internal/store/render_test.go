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
			{At: at, Capabilities: []string{"a", "b"}, With: "all", Budget: 500, Session: "s1"},
			{At: at, Capabilities: []string{"c"}},
		}, false, "2026-11-01T12:00:00Z s1 a,b with all budget 500\n2026-11-01T12:00:00Z — c\n"},
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
