package plan

import (
	"reflect"
	"strings"
	"testing"
)

var caps = []string{"ideas", "engine", "delivery"}

const valid = `{"changes":[
 {"name":"retry-queue","capability":"delivery","what":"Retries go through the queue",
  "acceptance":["go test ./delivery/... -run TestRetryEnqueue","failure case: a full queue refuses"],
  "must_not":["change the attempt limit"],"after":[]},
 {"name":"report","capability":["engine"],"what":"Report it",
  "acceptance":["go test ./engine/...","No failure case: pure formatting"],"after":["retry-queue"]}
]}`

func problems(t *testing.T, data string, c []string) string {
	t.Helper()
	_, err := Parse([]byte(data), c)
	if err == nil {
		t.Fatalf("expected an error for %s", data)
	}
	if !strings.HasPrefix(err.Error(), "the plan has problems: ") {
		t.Fatalf("error = %q", err)
	}
	return err.Error()
}

func change(body string) string { return `{"changes":[` + body + `]}` }

func one(extra string) string {
	return change(`{"name":"a","capability":"ideas","what":"w","acceptance":["failure case: x"]` + extra + `}`)
}

func TestValidPlan(t *testing.T) {
	p, err := Parse([]byte(valid), caps)
	if err != nil {
		t.Fatal(err)
	}
	if got := Names(p); !reflect.DeepEqual(got, []string{"retry-queue", "report"}) {
		t.Fatalf("names = %v", got)
	}
	if p.Changes[1].Capability != "engine" || !reflect.DeepEqual(p.Changes[1].After, []string{"retry-queue"}) {
		t.Fatalf("second change = %+v", p.Changes[1])
	}
	if p.Changes[0].MustNot[0] != "change the attempt limit" {
		t.Fatalf("must_not = %v", p.Changes[0].MustNot)
	}
}

func TestOneValidChange(t *testing.T) {
	if _, err := Parse([]byte(one("")), caps); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownFieldsRefused(t *testing.T) {
	if _, err := Parse([]byte(one(`,"files":["x.go"]`)), caps); err == nil {
		t.Fatal("a file list must be refused")
	}
	if _, err := Parse([]byte(`not json`), caps); err == nil {
		t.Fatal("bad JSON must be refused")
	}
}

func TestProblemKinds(t *testing.T) {
	cases := map[string]struct{ data, want string }{
		"no changes":      {`{"changes":[]}`, "the plan has no changes"},
		"no name":         {change(`{"capability":"ideas","what":"w","acceptance":["failure case: x"]}`), "change 1 has no name"},
		"not a slug":      {strings.Replace(one(""), `"a"`, `"Bad Name"`, 1), `"Bad Name" is not a slug`},
		"long name":       {strings.Replace(one(""), `"a"`, `"`+strings.Repeat("a", 41)+`"`, 1), "at most 40 characters"},
		"repeated":        {change(`{"name":"a","capability":"ideas","what":"w","acceptance":["failure case: x"]},{"name":"a","capability":"ideas","what":"w","acceptance":["failure case: x"]}`), `change "a" is repeated`},
		"no what":         {strings.Replace(one(""), `"what":"w"`, `"what":""`, 1), `change "a" says nothing of what it does`},
		"no capability":   {strings.Replace(one(""), `"capability":"ideas",`, ``, 1), `change "a" names no capability`},
		"empty array":     {strings.Replace(one(""), `"ideas"`, `[]`, 1), `change "a" names no capability`},
		"array of two":    {strings.Replace(one(""), `"ideas"`, `["ideas","engine"]`, 1), `change "a" names two capabilities: it should have been split`},
		"comma":           {strings.Replace(one(""), `"ideas"`, `"ideas, engine"`, 1), `change "a" names two capabilities: it should have been split`},
		"slash":           {strings.Replace(one(""), `"ideas"`, `"ideas/engine"`, 1), `names two capabilities`},
		"not in the map":  {strings.Replace(one(""), `"ideas"`, `"nope"`, 1), `change "a" names "nope", which is not a capability in the map`},
		"no acceptance":   {strings.Replace(one(""), `["failure case: x"]`, `[]`, 1), `change "a" acceptance holds no failure case and no stated reason`},
		"no failure case": {strings.Replace(one(""), `failure case: x`, `go test ./...`, 1), `acceptance holds no failure case and no stated reason`},
		"empty failure":   {strings.Replace(one(""), `failure case: x`, `failure case:  `, 1), `acceptance holds no failure case and no stated reason`},
		"unknown after":   {one(`,"after":["ghost"]`), `change "a" comes after "ghost", which is not in the plan`},
		"self after":      {one(`,"after":["a"]`), `change "a" comes after itself`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := problems(t, c.data, caps); !strings.Contains(got, c.want) {
				t.Fatalf("error %q lacks %q", got, c.want)
			}
		})
	}
}

func TestNoMapIsSaidOnce(t *testing.T) {
	data := change(`{"name":"a","capability":"ideas","what":"w","acceptance":["failure case: x"]},{"name":"b","capability":"engine","what":"w","acceptance":["failure case: x"]}`)
	got := problems(t, data, nil)
	if n := strings.Count(got, "the repo has no map, so no capability can be checked"); n != 1 {
		t.Fatalf("said %d times: %s", n, got)
	}
	if strings.Contains(got, "not a capability") {
		t.Fatalf("no per-change capability check without a map: %s", got)
	}
}

func TestSeveralProblemsTogether(t *testing.T) {
	data := change(`{"name":"a","capability":"ideas, engine","what":"","acceptance":[]},{"name":"b","capability":"nope","what":"w","acceptance":["failure case: x"],"after":["ghost"]}`)
	got := problems(t, data, caps)
	for _, want := range []string{"two capabilities", "says nothing", "no failure case", `"nope"`, "ghost"} {
		if !strings.Contains(got, want) {
			t.Fatalf("error %q lacks %q", got, want)
		}
	}
	if strings.Count(got, "the plan has problems") != 1 {
		t.Fatalf("not one error: %s", got)
	}
}

func TestCycleNamedInOrder(t *testing.T) {
	ch := func(n, after string) string {
		return `{"name":"` + n + `","capability":"ideas","what":"w","acceptance":["failure case: x"],"after":["` + after + `"]}`
	}
	got := problems(t, change(ch("a", "b")+","+ch("b", "c")+","+ch("c", "a")), caps)
	if !strings.Contains(got, "a -> b -> c -> a") {
		t.Fatalf("cycle not named in order: %s", got)
	}
	if strings.Count(got, "cycle") != 1 {
		t.Fatalf("cycle reported more than once: %s", got)
	}
}

func TestProblemsTwoCapabilitiesAndCycle(t *testing.T) {
	data := change(`{"name":"a","capability":"ideas, engine","what":"w","acceptance":["failure case: x"],"after":["b"]},{"name":"b","capability":"ideas","what":"w","acceptance":["failure case: x"],"after":["a"]}`)
	got := problems(t, data, caps)
	t.Log(got)
	if !strings.Contains(got, "names two capabilities") || !strings.Contains(got, "a -> b -> a") {
		t.Fatalf("error lacks both: %s", got)
	}
}
