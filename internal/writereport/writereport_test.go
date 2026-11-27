package writereport

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/initbranch"
)

// report stands in for init's report: any value that marshals to JSON.
type report struct {
	Files int `json:"files"`
}

func outcome() initbranch.Outcome {
	return initbranch.Outcome{
		Files:  []string{".physa/README.md"},
		Commit: &initbranch.Commit{ID: "abc1234def", Short: "abc1234", Subject: "Start Physa's map"},
		Body:   "Physa init recovered this map.\n\n## Measures\n\n- Source files: 3 of 5\n",
	}
}

func TestDescribeWithReport(t *testing.T) {
	text, v := Describe(outcome(), initbranch.Hook{}, report{Files: 5})
	for _, want := range []string{
		"Wrote physa/init at abc1234: Start Physa's map\n",
		"PR: not asked\nHook: not asked\n\n",
		"## Measures\n\n- Source files: 3 of 5\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "## Measures"); n != 1 {
		t.Errorf("text has the measures %d times, want once (in the body):\n%s", n, text)
	}
	js, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(js, &doc); err != nil {
		t.Fatal(err)
	}
	if got := string(doc["report"]); got != `{"files":5}` {
		t.Errorf("--json report = %s, want {\"files\":5}:\n%s", got, js)
	}
	for _, key := range []string{"branch", "files", "pushed", "body", "pr", "hook", "commit"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("--json misses %q:\n%s", key, js)
		}
	}
}

func TestDescribeWithoutReport(t *testing.T) {
	withText, _ := Describe(outcome(), initbranch.Hook{}, report{Files: 5})
	text, v := Describe(outcome(), initbranch.Hook{}, nil)
	if text != withText {
		t.Errorf("the report changed the text, which has it in the body only:\n%s\n---\n%s", text, withText)
	}
	if _, ok := v["report"]; ok {
		t.Errorf("--json has a report with none given: %v", v["report"])
	}
	js, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), `"report"`) {
		t.Errorf("--json names a report with none given:\n%s", js)
	}
}
