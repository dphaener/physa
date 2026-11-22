// Package apischema derives the JSON schema of an API method's input and output from its Go type, and checks a JSON value against it, so every method is typed once.
package apischema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Schema is the subset of JSON Schema the API uses.
type Schema struct {
	Type                 string             `json:"type"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *bool              `json:"additionalProperties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
}

var (
	timeType   = reflect.TypeOf(time.Time{})
	schemaType = reflect.TypeOf(Schema{})
	rawType    = reflect.TypeOf(json.RawMessage(nil))
)

// For derives the schema of v's type. It panics on a type it cannot
// describe, which is a programming error that tests catch.
func For(v any) *Schema {
	return forType(reflect.TypeOf(v))
}

func forType(t reflect.Type) *Schema {
	if t == nil {
		return &Schema{Type: "object"}
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == timeType:
		return &Schema{Type: "string"}
	case t == schemaType, t == rawType:
		return &Schema{Type: "object"}
	}
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return &Schema{Type: "string"}
		}
		return &Schema{Type: "array", Items: forType(t.Elem())}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("apischema: map key of %s is not a string", t))
		}
		return &Schema{Type: "object"}
	case reflect.Interface:
		return &Schema{Type: "object"}
	case reflect.Struct:
		return forStruct(t)
	}
	panic(fmt.Sprintf("apischema: cannot describe %s", t))
}

func forStruct(t reflect.Type) *Schema {
	closed := false
	s := &Schema{Type: "object", Properties: map[string]*Schema{}, AdditionalProperties: &closed}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" && opts == "" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		p := forType(f.Type)
		if d := f.Tag.Get("desc"); d != "" {
			c := *p
			c.Description = d
			p = &c
		}
		s.Properties[name] = p
		if !strings.Contains(","+opts+",", ",omitempty,") {
			s.Required = append(s.Required, name)
		}
	}
	return s
}

// Check validates a JSON document against s. Errors name the path from
// "params", for example `params.name: must be a string`.
func Check(s *Schema, raw json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("params: not valid JSON: %w", err)
	}
	return check(s, v, "params")
}

func check(s *Schema, v any, path string) error {
	bad := func(what string) error { return fmt.Errorf("%s: must be %s", path, what) }
	switch s.Type {
	case "string":
		if _, ok := v.(string); !ok {
			return bad("a string")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return bad("a boolean")
		}
	case "number":
		if _, ok := v.(json.Number); !ok {
			return bad("a number")
		}
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return bad("an integer")
		}
		if _, err := n.Int64(); err != nil {
			f, ferr := n.Float64()
			if ferr != nil || f != math.Trunc(f) {
				return bad("an integer")
			}
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			return bad("an array")
		}
		if s.Items == nil {
			return nil
		}
		for i, e := range list {
			if err := check(s.Items, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return bad("an object")
		}
		return checkObject(s, obj, path)
	}
	return nil
}

func checkObject(s *Schema, obj map[string]any, path string) error {
	for _, name := range s.Required {
		if _, ok := obj[name]; !ok {
			return fmt.Errorf("%s: missing required %q", path, name)
		}
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p, known := s.Properties[k]
		if !known {
			if s.AdditionalProperties != nil && !*s.AdditionalProperties {
				return fmt.Errorf("%s: unknown property %q", path, k)
			}
			continue
		}
		if err := check(p, obj[k], path+"."+k); err != nil {
			return err
		}
	}
	return nil
}
