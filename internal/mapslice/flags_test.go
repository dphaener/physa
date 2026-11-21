package mapslice

import (
	"reflect"
	"testing"
)

func TestParseFlagsForms(t *testing.T) {
	o, ok := ParseFlags([]string{"a", "--with", "all", "b", "--budget=500", "--format", "json"})
	if !ok {
		t.Fatal("want ok")
	}
	want := Request{Capabilities: []string{"a", "b"}, WithDecisions: true, WithQuestions: true, Budget: 500}
	if !reflect.DeepEqual(o.Req, want) || !o.JSON {
		t.Errorf("got %+v", o)
	}
}

func TestParseFlagsWithKinds(t *testing.T) {
	o, _ := ParseFlags([]string{"a", "--with=decisions"})
	if !o.Req.WithDecisions || o.Req.WithQuestions {
		t.Errorf("decisions: %+v", o.Req)
	}
	o, _ = ParseFlags([]string{"a", "--with=questions"})
	if o.Req.WithDecisions || !o.Req.WithQuestions {
		t.Errorf("questions: %+v", o.Req)
	}
}

func TestParseFlagsRejects(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--budget", "5"},
		{"a", "--budget"},
		{"a", "--budget", "notanumber"},
		{"a", "--budget=0"},
		{"a", "--with", "nothing"},
		{"a", "--format", "xml"},
		{"a", "--bogus", "x"},
	} {
		if _, ok := ParseFlags(args); ok {
			t.Errorf("%v: want not ok", args)
		}
	}
}
