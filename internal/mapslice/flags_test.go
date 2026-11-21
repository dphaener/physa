package mapslice

import (
	"reflect"
	"testing"
)

func TestParseFlagsForms(t *testing.T) {
	o, ok := ParseFlags([]string{"a", "--with", "all", "b", "--format", "json"})
	if !ok {
		t.Fatal("want ok")
	}
	want := Request{Capabilities: []string{"a", "b"}, WithDecisions: true, WithQuestions: true}
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

func TestParseFlagsEntry(t *testing.T) {
	for _, c := range []struct {
		args []string
		want []string
		json bool
	}{
		{[]string{"--entry", "a", "b"}, []string{"a", "b"}, false},
		{[]string{"--entry=a", "--entry=b"}, []string{"a", "b"}, false},
		{[]string{"--entry", "a", "--entry", "b"}, []string{"a", "b"}, false},
		{[]string{"--entry=a", "b"}, []string{"a", "b"}, false},
		{[]string{"--format", "json", "--entry", "b", "a"}, []string{"b", "a"}, true},
		{[]string{"--entry", "a", "--format=json", "b"}, []string{"a", "b"}, true},
		{[]string{"--entry", "a", "a"}, []string{"a", "a"}, false},
	} {
		o, ok := ParseFlags(c.args)
		if !ok {
			t.Errorf("%v: want ok", c.args)
			continue
		}
		if !reflect.DeepEqual(o.Req, Request{Entries: c.want}) || o.JSON != c.json {
			t.Errorf("%v: got %+v", c.args, o)
		}
	}
}

func TestParseFlagsRejects(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"a", "--with", "nothing"},
		{"a", "--format", "xml"},
		{"a", "--format"},
		{"a", "--bogus", "x"},
		{"--entry"},
		{"--entry="},
		{"--entry", "--format", "json"},
		{"a", "--entry", "b"},
		{"--entry", "b", "--with", "all"},
		{"--with=decisions", "--entry=b"},
	} {
		if _, ok := ParseFlags(args); ok {
			t.Errorf("%v: want not ok", args)
		}
	}
}
