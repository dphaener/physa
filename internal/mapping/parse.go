package mapping

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/jsonblock"
)

// Parse reads the session's reply: the last fenced json block in text,
// holding one object with a product, a capabilities list and a questions
// list. The error, worded to follow "the session's output: ", is for a reply
// with no such block, one that does not decode, or one with no product or no
// capabilities. Each capability and each product-wide question is decoded on
// its own, refusing unknown keys, so one that is not what it should be
// becomes a Malformed and the others are kept, in order. Parse checks shape
// only; Check holds the reply against the gather.
func Parse(text string) (Reply, []Malformed, error) {
	block, ok := jsonblock.Last(text)
	if !ok {
		return Reply{}, nil, errors.New("it has no fenced json block")
	}
	var w struct {
		Product      *Product           `json:"product"`
		Capabilities *[]json.RawMessage `json:"capabilities"`
		Questions    []json.RawMessage  `json:"questions"`
	}
	dec := json.NewDecoder(strings.NewReader(block))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return Reply{}, nil, errors.New("its json block does not decode: " + err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Reply{}, nil, errors.New("its json block holds more than one value")
	}
	if w.Product == nil {
		return Reply{}, nil, errors.New("its json block has no product")
	}
	if w.Capabilities == nil {
		return Reply{}, nil, errors.New("its json block has no capabilities list: write [] for an empty one")
	}
	r := Reply{Product: *w.Product}
	var malformed []Malformed
	for _, raw := range *w.Capabilities {
		// The session never writes unshown: that is Check's to say.
		var c struct {
			Name      string     `json:"name"`
			Promise   string     `json:"promise"`
			ShownBy   []string   `json:"shown_by"`
			Docs      []string   `json:"docs"`
			Questions []Question `json:"questions"`
		}
		if err := item(raw, &c, "a capability"); err != nil {
			malformed = append(malformed, Malformed{Raw: string(raw), Reason: err.Error()})
			continue
		}
		r.Capabilities = append(r.Capabilities, Capability{Name: c.Name, Promise: c.Promise, ShownBy: c.ShownBy, Docs: c.Docs, Questions: c.Questions})
	}
	for _, raw := range w.Questions {
		var q Question
		if err := item(raw, &q, "a question"); err != nil {
			malformed = append(malformed, Malformed{Raw: string(raw), Reason: err.Error()})
			continue
		}
		r.Questions = append(r.Questions, q)
	}
	return r, malformed, nil
}

// item decodes one item of a list into v, refusing unknown keys and anything
// that is not a JSON object. what names the item for the error.
func item(raw json.RawMessage, v any, what string) error {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return errors.New(what + " must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
