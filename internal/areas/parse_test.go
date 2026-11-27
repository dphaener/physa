package areas

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/productpromise"
)

func TestParseSplit(t *testing.T) {
	tests, docs, other, err := ParseSplit("thinking\n```json\n{\"tests\":[\"spec/\"],\"docs\":[\"*.md\"]}\n```\n")
	if err != nil || !reflect.DeepEqual(tests, []string{"spec/"}) || !reflect.DeepEqual(docs, []string{"*.md"}) || len(other) != 0 {
		t.Fatalf("got %v %v %v %v", tests, docs, other, err)
	}
}

func TestParseAreas(t *testing.T) {
	text := "```json\n{\"areas\":[{\"name\":\"old\",\"promise\":\"x\",\"sources\":[\"a/\"]}]}\n```\nmore\n```json\n" +
		"{\"areas\":[{\"name\":\" Sign In!\",\"promise\":\"People\\nsign in.\",\"sources\":[\"web/\",\"app/auth.rb\"]},{\"name\":\"\",\"promise\":\"p\",\"sources\":[]}]}\n```\n"
	got, product, err := ParseAreas(text)
	if err != nil || product != (productpromise.Product{}) {
		t.Fatal(product, err)
	}
	want := []Area{{Name: "sign-in", Promise: "People sign in.", Sources: []string{"web/", "app/auth.rb"}}, {Name: "unnamed", Promise: "p", Sources: []string{}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestParseFix(t *testing.T) {
	f, err := ParseFix("```json\n{\"add\":{\"Checkout\":[\"a.go\"]},\"new_areas\":[{\"name\":\"n\",\"promise\":\"p\",\"sources\":[\"b.go\"]}]}\n```")
	if err != nil || !reflect.DeepEqual(f.Add, map[string][]string{"checkout": {"a.go"}}) || len(f.NewAreas) != 1 || f.NewAreas[0].Name != "n" {
		t.Fatalf("got %+v %v", f, err)
	}
}

func TestParseMerge(t *testing.T) {
	m, err := ParseMerge("```json\n{\"merge\":[{\"into\":\"checkout\",\"from\":[\"Cart Screens\"],\"promise\":\"p\",\"why\":\"w\"}]}\n```")
	if err != nil || len(m) != 1 || m[0].Into != "checkout" || !reflect.DeepEqual(m[0].From, []string{"cart-screens"}) {
		t.Fatalf("got %+v %v", m, err)
	}
	if m, err := ParseMerge("```json\n{\"merge\":[]}\n```"); err != nil || len(m) != 0 {
		t.Errorf("empty merge: %v %v", m, err)
	}
}

func TestParseErrorsNameWhatWasExpected(t *testing.T) {
	type parse func(string) error
	all := map[string]parse{
		"split": func(s string) error { _, _, _, err := ParseSplit(s); return err },
		"areas": func(s string) error { _, _, err := ParseAreas(s); return err },
		"fix":   func(s string) error { _, err := ParseFix(s); return err },
		"merge": func(s string) error { _, err := ParseMerge(s); return err },
	}
	for name, p := range all {
		for _, c := range []struct{ label, text, has string }{
			{"missing block", "just prose, and ```go\nx\n``` code", "no fenced json block"},
			{"bad json", "```json\n{\"areas\": [oops\n```", "does not decode"},
		} {
			err := p(c.text)
			if err == nil || !strings.Contains(err.Error(), c.has) || !strings.Contains(err.Error(), "expected") {
				t.Errorf("%s/%s: got %v", name, c.label, err)
			}
		}
	}
	if _, _, err := ParseAreas("```json\n{}\n```"); err == nil || !strings.Contains(err.Error(), `"areas"`) {
		t.Errorf("no areas key: %v", err)
	}
	if _, err := ParseMerge("```json\n{}\n```"); err == nil {
		t.Error("no merge key should fail")
	}
}

func TestParseAreasProduct(t *testing.T) {
	areas := `"areas":[{"name":"a","promise":"p","sources":["a/"]}]`
	for _, c := range []struct {
		label, product string
		want           productpromise.Product
	}{
		{"stated", `"product":{"name":" Shop ","promise":"People buy\nwhat they need."},`, productpromise.Product{Name: "Shop", Promise: "People buy what they need."}},
		{"marked-up name", `"product":{"name":"# Shop","promise":"People buy."},`, productpromise.Product{Promise: "People buy."}},
		{"no product", ``, productpromise.Product{}},
		{"not an object", `"product":"Shop",`, productpromise.Product{}},
		{"wrong field types", `"product":{"name":3,"promise":["x"]},`, productpromise.Product{}},
	} {
		got, product, err := ParseAreas("```json\n{" + c.product + areas + "}\n```")
		if err != nil || len(got) != 1 || product != c.want {
			t.Errorf("%s: areas %+v product %+v err %v, want product %+v", c.label, got, product, err, c.want)
		}
	}
}
