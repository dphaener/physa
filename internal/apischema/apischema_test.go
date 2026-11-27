package apischema

import (
	"encoding/json"
	"testing"
	"time"
)

type item struct {
	Name string `json:"name" desc:"the item name"`
}

type sample struct {
	Name    string            `json:"name" desc:"who"`
	Count   int               `json:"count"`
	Ratio   float64           `json:"ratio,omitempty"`
	On      bool              `json:"on,omitempty"`
	When    time.Time         `json:"when,omitempty"`
	Tags    []string          `json:"tags"`
	Items   []item            `json:"items,omitempty"`
	Extra   map[string]string `json:"extra,omitempty"`
	Data    []byte            `json:"data,omitempty"`
	Ptr     *item             `json:"ptr,omitempty"`
	Skipped string            `json:"-"`
}

func TestForNestedStruct(t *testing.T) {
	b, err := json.Marshal(For(sample{}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"object","properties":{` +
		`"count":{"type":"integer"},` +
		`"data":{"type":"string"},` +
		`"extra":{"type":"object"},` +
		`"items":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string","description":"the item name"}},"required":["name"],"additionalProperties":false}},` +
		`"name":{"type":"string","description":"who"},` +
		`"on":{"type":"boolean"},` +
		`"ptr":{"type":"object","properties":{"name":{"type":"string","description":"the item name"}},"required":["name"],"additionalProperties":false},` +
		`"ratio":{"type":"number"},` +
		`"tags":{"type":"array","items":{"type":"string"}},` +
		`"when":{"type":"string"}},` +
		`"required":["name","count","tags"],"additionalProperties":false}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestForSchemaField(t *testing.T) {
	type holder struct {
		Input  *Schema         `json:"input"`
		Output Schema          `json:"output"`
		Raw    json.RawMessage `json:"raw"`
	}
	b, err := json.Marshal(For(holder{}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"object","properties":{"input":{"type":"object"},"output":{"type":"object"},"raw":{"type":"object"}},"required":["input","output","raw"],"additionalProperties":false}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestForPanicsOnChan(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	For(make(chan int))
}

func TestCheckAccepts(t *testing.T) {
	doc := `{"name":"a","count":3,"tags":["x"],"items":[{"name":"n"}],"ratio":1.5,"extra":{"k":"v"}}`
	if err := Check(For(sample{}), json.RawMessage(doc)); err != nil {
		t.Fatal(err)
	}
	if err := Check(For(sample{}), json.RawMessage(`{"name":"a","count":3.0,"tags":[]}`)); err != nil {
		t.Fatalf("3.0 is an integer: %v", err)
	}
}

func TestCheckRejects(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{"wrong type", `{"name":5,"count":1,"tags":[]}`, `params.name: must be a string`},
		{"missing required", `{"count":1,"tags":[]}`, `params: missing required "name"`},
		{"unknown property", `{"name":"a","count":1,"tags":[],"x":1}`, `params: unknown property "x"`},
		{"fractional integer", `{"name":"a","count":1.5,"tags":[]}`, `params.count: must be an integer`},
		{"array element", `{"name":"a","count":1,"tags":["ok",2]}`, `params.tags[1]: must be a string`},
		{"nested element", `{"name":"a","count":1,"tags":[],"items":[{"name":1}]}`, `params.items[0].name: must be a string`},
		{"null object", `null`, `params: must be an object`},
		{"boolean", `{"name":"a","count":1,"tags":[],"on":"yes"}`, `params.on: must be a boolean`},
		{"not an array", `{"name":"a","count":1,"tags":"x"}`, `params.tags: must be an array`},
		{"not a number", `{"name":"a","count":1,"tags":[],"ratio":"x"}`, `params.ratio: must be a number`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Check(For(sample{}), json.RawMessage(c.doc))
			if err == nil || err.Error() != c.want {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
}

func TestCheckRejectsInvalidJSON(t *testing.T) {
	if err := Check(For(sample{}), json.RawMessage(`{`)); err == nil {
		t.Fatal("expected error")
	}
}
