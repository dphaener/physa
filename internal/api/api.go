// Package api is the engine's local API: methods called by name with JSON in and out over a WebSocket on a Unix socket, each with a JSON schema for its input and output, under the physa.v1 subprotocol and one API revision; the engine may send messages of its own on the same connection.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/dphaener/physa/client"
	"github.com/dphaener/physa/internal/apischema"
)

// The wire's names are the public client's, so the two never part.
const (
	Subprotocol = client.Subprotocol // the WebSocket subprotocol both sides require
	Revision    = 1                  // rises when a method changes in a way a client could notice
	Path        = client.Path        // the only HTTP path the server answers, with GET

	CodeBadRequest    = client.CodeBadRequest
	CodeUnknownMethod = client.CodeUnknownMethod
	CodeInternal      = client.CodeInternal
)

// Error is the error a call replies with: a code from the list above and
// a message for a person.
type Error = client.Error

// Method is one method of the API: its name, what it does, the schemas of
// its input and output, and an example of each. Build one with Define.
type Method struct {
	Name, Summary                   string
	Notes                           []string
	Input, Output                   *apischema.Schema
	RequestExample, ResponseExample string
	call                            func(ctx context.Context, c *Conn, params json.RawMessage) (any, error)
}

// Define builds a method typed once, by In and Out. A call checks its
// params (missing or null mean {}) against In's schema, replying
// bad_request with the check's message, then decodes them into In. A
// returned *Error is replied as it is; any other error replies internal.
func Define[In, Out any](name, summary string, notes []string, reqExample, respExample string,
	fn func(ctx context.Context, c *Conn, in In) (Out, error)) Method {
	input := apischema.For(*new(In))
	return Method{name, summary, notes, input, apischema.For(*new(Out)), reqExample, respExample,
		func(ctx context.Context, c *Conn, params json.RawMessage) (any, error) {
			if t := bytes.TrimSpace(params); len(t) == 0 || string(t) == "null" {
				params = json.RawMessage("{}")
			}
			if err := apischema.Check(input, params); err != nil {
				return nil, &Error{Code: CodeBadRequest, Message: err.Error()}
			}
			var in In
			dec := json.NewDecoder(bytes.NewReader(params))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				return nil, &Error{Code: CodeBadRequest, Message: "params: " + err.Error()}
			}
			return fn(ctx, c, in)
		},
	}
}

// Table is the set of methods a server answers, sorted by name.
type Table struct {
	methods []Method
	byName  map[string]Method
}

// MethodSummary is one line of api.list's output.
type MethodSummary struct {
	Name    string `json:"name" desc:"The method's name, as a call names it."`
	Summary string `json:"summary" desc:"What the method does, in one line."`
}

// DescribeInput is api.describe's input.
type DescribeInput struct {
	Name string `json:"name" desc:"The method to describe."`
}

// Description is api.describe's output.
type Description struct {
	Name            string            `json:"name" desc:"The method's name."`
	Summary         string            `json:"summary" desc:"What the method does, in one line."`
	Notes           []string          `json:"notes" desc:"Anything a caller should know; empty when there is nothing."`
	Input           *apischema.Schema `json:"input" desc:"The JSON schema of the method's params."`
	Output          *apischema.Schema `json:"output" desc:"The JSON schema of the method's result."`
	RequestExample  string            `json:"request_example" desc:"Example params, as JSON text."`
	ResponseExample string            `json:"response_example" desc:"Example result, as JSON text."`
}

// NewTable holds methods sorted by name, adding api.list and api.describe.
// A name given twice panics: it is a programming error that tests catch.
func NewTable(methods ...Method) *Table {
	t := &Table{byName: map[string]Method{}}
	list := Define("api.list", "Lists every method with its summary.", nil,
		`{}`, `[{"name":"api.describe","summary":"Describes one method: its schemas, examples and notes."}]`,
		func(_ context.Context, _ *Conn, _ struct{}) ([]MethodSummary, error) {
			out := make([]MethodSummary, 0, len(t.methods))
			for _, m := range t.methods {
				out = append(out, MethodSummary{Name: m.Name, Summary: m.Summary})
			}
			return out, nil
		})
	describe := Define("api.describe", "Describes one method: its schemas, examples and notes.",
		[]string{"An unknown name replies bad_request."},
		`{"name":"api.list"}`, `{"name":"api.list","summary":"Lists every method with its summary.","notes":[],"input":{"type":"object","additionalProperties":false},"output":{"type":"array","items":{"type":"object"}},"request_example":"{}","response_example":"[]"}`,
		func(_ context.Context, _ *Conn, in DescribeInput) (Description, error) {
			m, ok := t.byName[in.Name]
			if !ok {
				return Description{}, &Error{Code: CodeBadRequest, Message: fmt.Sprintf("no method %q", in.Name)}
			}
			return Description{Name: m.Name, Summary: m.Summary, Notes: append([]string{}, m.Notes...), Input: m.Input,
				Output: m.Output, RequestExample: m.RequestExample, ResponseExample: m.ResponseExample}, nil
		})
	for _, m := range append([]Method{list, describe}, methods...) {
		if _, dup := t.byName[m.Name]; dup {
			panic(fmt.Sprintf("api: method %q defined twice", m.Name))
		}
		t.byName[m.Name] = m
		t.methods = append(t.methods, m)
	}
	sort.Slice(t.methods, func(i, j int) bool { return t.methods[i].Name < t.methods[j].Name })
	return t
}
