package productpromise

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/initrecord"
)

type fakeStore struct {
	areas initrecord.Areas
	ok    bool
	err   error
}

func (f fakeStore) Areas(context.Context) (initrecord.Areas, bool, error) {
	return f.areas, f.ok, f.err
}

func withBody(body string) fakeStore {
	return fakeStore{areas: initrecord.Areas{Body: []byte(body)}, ok: true}
}

func TestStoredReadsProduct(t *testing.T) {
	p, ok, err := Stored(context.Background(), withBody(`{"areas":[],"product":{"name":"Physa","promise":"Maps for teams."}}`))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if p.Name != "Physa" || p.Promise != "Maps for teams." {
		t.Fatalf("got %+v", p)
	}
}

func TestStoredNoProductKey(t *testing.T) {
	p, ok, err := Stored(context.Background(), withBody(`{"areas":[]}`))
	if err != nil || ok || p != (Product{}) {
		t.Fatalf("got %+v %v %v", p, ok, err)
	}
}

func TestStoredNoAreasRow(t *testing.T) {
	p, ok, err := Stored(context.Background(), fakeStore{})
	if err != nil || ok || p != (Product{}) {
		t.Fatalf("got %+v %v %v", p, ok, err)
	}
	if p.PromiseOr() != NoPromise {
		t.Fatalf("PromiseOr = %q", p.PromiseOr())
	}
}

func TestStoredEmptyPromise(t *testing.T) {
	p, ok, err := Stored(context.Background(), withBody(`{"product":{"name":"Physa","promise":"  "}}`))
	if err != nil || ok || p != (Product{}) {
		t.Fatalf("got %+v %v %v", p, ok, err)
	}
	if p.PromiseOr() != NoPromise {
		t.Fatalf("PromiseOr = %q", p.PromiseOr())
	}
}

func TestStoredUndecodableBody(t *testing.T) {
	_, ok, err := Stored(context.Background(), withBody(`secret-body {not json`))
	if err == nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if strings.Contains(err.Error(), "secret-body") {
		t.Fatalf("error quotes the body: %v", err)
	}
}

func TestStoredStoreError(t *testing.T) {
	want := errors.New("boom")
	if _, _, err := Stored(context.Background(), fakeStore{err: want}); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestCleanMultiLinePromise(t *testing.T) {
	p := Clean(Product{Name: " Physa ", Promise: "  one\n\ttwo   three\n"})
	if p.Name != "Physa" || p.Promise != "one two three" {
		t.Fatalf("got %+v", p)
	}
}

func TestCleanEmptiesBadName(t *testing.T) {
	for name, in := range map[string]string{
		"long":      strings.Repeat("a", 81),
		"newline":   "a\nb",
		"backtick":  "a`b",
		"heading":   "# Physa",
		"paragraph": "Physa\n\nmore",
	} {
		if got := Clean(Product{Name: in, Promise: "p"}); got.Name != "" || got.Promise != "p" {
			t.Errorf("%s: got %+v", name, got)
		}
	}
	if got := Clean(Product{Name: strings.Repeat("é", 80)}); got.Name == "" {
		t.Error("an 80 rune name was emptied")
	}
}

func TestBlockAndLead(t *testing.T) {
	want := "The product's promise:\n\n```text\nMaps.\n```\n"
	if got := Block("Maps."); got != want {
		t.Fatalf("Block = %q", got)
	}
	if got := Lead(context.Background(), withBody(`{"product":{"promise":"Maps."}}`)); got != want+"\n" {
		t.Fatalf("Lead = %q", got)
	}
	if got := Lead(context.Background(), fakeStore{}); got != "" {
		t.Fatalf("Lead with no areas = %q", got)
	}
	if got := Lead(context.Background(), withBody(`nope`)); got != "" {
		t.Fatalf("Lead with a bad body = %q", got)
	}
}
