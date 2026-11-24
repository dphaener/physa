package mapslice

import (
	"io"
	"reflect"
	"testing"

	"github.com/dphaener/physa/internal/command"
)

// parse runs args through a declared slice command, as physa map slice does,
// and returns the flags its job read, and whether the line was good.
func parse(args ...string) (Flags, bool) {
	var l Line
	var got Flags
	c := command.Declare(command.New("slice", "", []string{"slice [--with x] [--format x] [--entry]"},
		func(pos []string, _, _ io.Writer) int {
			o, ok := l.Flags(pos)
			if !ok {
				return command.BadUsage
			}
			got = o
			return 0
		}), l.Declare)
	code := command.Execute(c, args, io.Discard, io.Discard)
	return got, code == 0
}

func TestParseFlagsForms(t *testing.T) {
	o, ok := parse("a", "--with", "all", "b", "--format", "json")
	if !ok {
		t.Fatal("want ok")
	}
	want := Request{Capabilities: []string{"a", "b"}, WithDecisions: true, WithQuestions: true}
	if !reflect.DeepEqual(o.Req, want) || !o.JSON {
		t.Errorf("got %+v", o)
	}
}

func TestParseFlagsWithKinds(t *testing.T) {
	o, _ := parse("a", "--with=decisions")
	if !o.Req.WithDecisions || o.Req.WithQuestions {
		t.Errorf("decisions: %+v", o.Req)
	}
	o, _ = parse("a", "--with=questions")
	if o.Req.WithDecisions || !o.Req.WithQuestions {
		t.Errorf("questions: %+v", o.Req)
	}
	o, _ = parse("a", "--with", "decisions", "--with", "questions")
	if !o.Req.WithDecisions || !o.Req.WithQuestions {
		t.Errorf("repeated --with: %+v", o.Req)
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
		{[]string{"--entry", "b", "--entry=a"}, []string{"b", "a"}, false},
		{[]string{"--entry", "a", "--entry=b", "c"}, []string{"a", "b", "c"}, false},
		{[]string{"--format", "json", "--entry", "b", "a"}, []string{"b", "a"}, true},
		{[]string{"--entry", "a", "--format=json", "b"}, []string{"a", "b"}, true},
		{[]string{"--entry", "a", "a"}, []string{"a", "a"}, false},
	} {
		o, ok := parse(c.args...)
		if !ok {
			t.Errorf("%v: want ok", c.args)
			continue
		}
		if !reflect.DeepEqual(o.Req, Request{Entries: c.want}) || o.JSON != c.json {
			t.Errorf("%v: got %+v", c.args, o)
		}
	}
}

// TestParseFlagsStartAfresh runs two lines on one command: the second must
// not see the first's flags.
func TestParseFlagsStartAfresh(t *testing.T) {
	var l Line
	var got []Flags
	c := command.Declare(command.New("slice", "", nil,
		func(pos []string, _, _ io.Writer) int {
			o, ok := l.Flags(pos)
			if !ok {
				return command.BadUsage
			}
			got = append(got, o)
			return 0
		}), l.Declare)
	for _, args := range [][]string{{"--entry=x", "--format", "json"}, {"a"}} {
		if code := command.Execute(c, args, io.Discard, io.Discard); code != 0 {
			t.Fatalf("%v = %d", args, code)
		}
	}
	if want := (Flags{Req: Request{Capabilities: []string{"a"}}}); !reflect.DeepEqual(got[1], want) {
		t.Errorf("second line = %+v, want %+v", got[1], want)
	}
}

func TestParseFlagsRejects(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"a", "--with", "nothing"},
		{"a", "--format", "xml"},
		{"a", "--format"},
		{"a", "--format="},
		{"a", "--with="},
		{"a", "--bogus", "x"},
		{"a", "-x"},
		{"--entry"},
		{"--entry="},
		{"--entry", "--format", "json"},
		{"a", "--entry", "b"},
		{"--entry", "b", "--with", "all"},
		{"--with=decisions", "--entry=b"},
		{"--entry", "b", "--with=bad"},
	} {
		if _, ok := parse(args...); ok {
			t.Errorf("%v: want not ok", args)
		}
	}
}
