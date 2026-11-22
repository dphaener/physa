package output

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestPlainFields(t *testing.T) {
	var b bytes.Buffer
	New(&b, false).Fields([2]string{"pid", "42"}, [2]string{"socket", "/x"})
	want := "pid     42\nsocket  /x\n"
	if b.String() != want || strings.Contains(b.String(), "\x1b[") {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestPlainTable(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, false)
	p.Heading("Methods")
	p.Table([]string{"name", "kind"}, [][]string{{"engine.status", "call"}, {"a", "b"}})
	want := "Methods\nname           kind\nengine.status  call\na              b\n"
	if b.String() != want || strings.Contains(b.String(), "\x1b[") {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestStyledHasTextAndEscapes(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Heading("Head")
	p.Fields([2]string{"k", "v"})
	p.Table([]string{"h"}, [][]string{{"r"}})
	if !strings.Contains(b.String(), "\x1b[") {
		t.Errorf("styled output has no escape: %q", b.String())
	}
	if got, want := ansi.ReplaceAllString(b.String(), ""), "Head\nk  v\nh\nr\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestForBufferIsPlain(t *testing.T) {
	var b bytes.Buffer
	For(&b).Heading("x")
	if b.String() != "x\n" {
		t.Errorf("got %q", b.String())
	}
}

func TestIsTerminalDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("the null device reads as a terminal")
	}
}

func TestIsTerminalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in")
	if err := os.WriteFile(path, []byte("answer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("a regular file reads as a terminal")
	}
}

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, map[string]any{"a": 1, "b": []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 1,\n  \"b\": [\n    \"x\"\n  ]\n}\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
