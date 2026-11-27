// Package productpromise holds the product's name and promise as the areas
// session states them and as init's record keeps them, so the walk, recovery
// and consolidation sessions and the written map read one form.
package productpromise

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/initrecord"
)

// NoPromise is what stands for a promise the areas session did not state.
const NoPromise = "Not yet stated."

// maxName is the longest name, in runes, that Clean keeps.
const maxName = 80

// Product is the product's name and promise.
type Product struct {
	Name    string `json:"name"`
	Promise string `json:"promise"`
}

// Clean returns p with a name that is not a plain name emptied (longer than
// 80 runes, a line break or a backtick in it, or a leading #), and each field
// made one trimmed line.
func Clean(p Product) Product {
	name := strings.TrimSpace(p.Name)
	if utf8.RuneCountInString(name) > maxName || strings.ContainsAny(name, "\r\n`") || strings.HasPrefix(name, "#") {
		name = ""
	}
	return Product{Name: draft.OneLine(name), Promise: draft.OneLine(p.Promise)}
}

// PromiseOr returns the promise, or NoPromise when none is stated.
func (p Product) PromiseOr() string {
	if p.Promise == "" {
		return NoPromise
	}
	return p.Promise
}

// AreasStore is what Stored reads the areas from.
type AreasStore interface {
	Areas(ctx context.Context) (initrecord.Areas, bool, error)
}

// Stored returns the cleaned product of the stored areas and whether a promise
// is stated. No areas, no product or an empty promise give an empty product and
// false. A body that is not JSON is an error that does not quote it.
func Stored(ctx context.Context, st AreasStore) (Product, bool, error) {
	a, ok, err := st.Areas(ctx)
	if err != nil {
		return Product{}, false, err
	}
	if !ok {
		return Product{}, false, nil
	}
	var body struct {
		Product *Product `json:"product"`
	}
	if err := json.Unmarshal(a.Body, &body); err != nil {
		return Product{}, false, errors.New("the stored areas are not valid JSON")
	}
	if body.Product == nil {
		return Product{}, false, nil
	}
	p := Clean(*body.Product)
	if p.Promise == "" {
		return Product{}, false, nil
	}
	return p, true, nil
}

// Block returns the text a session's message carries for the promise: a line
// naming it, then the promise in a fence.
func Block(promise string) string {
	var b strings.Builder
	b.WriteString("The product's promise:\n\n")
	draft.Fence(&b, promise)
	return b.String()
}

// Lead returns Block of the stored promise and a blank line, or the empty
// string when none is stated or the record cannot be read.
func Lead(ctx context.Context, st AreasStore) string {
	p, ok, err := Stored(ctx, st)
	if err != nil || !ok {
		return ""
	}
	return Block(p.Promise) + "\n"
}
