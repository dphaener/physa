package initrun

import (
	"encoding/json"
	"strings"
	"testing"
)

// reportHead is the first line of init's report as plain text, and
// sectionHead the PR body section's heading.
const (
	reportHead  = "Measures of init's record, in counts:\n"
	sectionHead = "## Measures\n"
)

// walkMeasures are the measures that need the areas, the walk or the
// placements, which a full run stores and so can give; notStored are the
// reasons a measure gives when one of them is not stored.
var (
	walkMeasures = []string{"files", "placed", "prs_in_one_capability", "capabilities_with_no_pr_or_change", "rules_reasons"}
	notStored    = []string{"no areas are stored", "no walk is stored", "no placements are stored", "no consolidation is stored"}
)

// sectionBetween fails unless body holds the measures section once, after
// the capabilities proposed and before the entries recovered.
func sectionBetween(t *testing.T, body string) {
	t.Helper()
	caps, sec, entries := strings.Index(body, "## Capabilities proposed"), strings.Index(body, sectionHead), strings.Index(body, "## Entries recovered")
	if n := strings.Count(body, sectionHead); n != 1 || caps < 0 || caps >= sec || sec >= entries {
		t.Errorf("the measures section (%d of them) is not once between the capabilities and the entries:\n%s", n, body)
	}
}

// TestReportWriteAfterStoredPhases: --consolidate --write after a stored
// walk, placements and recoveries shows the report's section in the PR body
// and, plain, only there; with --json the write's value holds a report
// object, and no report stands beside it.
func TestReportWriteAfterStoredPhases(t *testing.T) {
	env := consEnv(t, &consolidateAgent{merge: mergeSignIn}, mergeCaps)
	code, out, errOut := runInit(env, "--consolidate", "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	t.Logf("plain output of --consolidate --write:\n%s", out)
	sectionBetween(t, out)
	if strings.Contains(out, reportHead) {
		t.Errorf("--write printed the report beside the body's section:\n%s", out)
	}
	for _, want := range []string{"- Consolidation:\n  - capabilities merged: 1 group, 1 merged away\n", "- Capabilities with no PR or change: 3 of 3 (100%)\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("the section lacks %q:\n%s", want, out)
		}
	}

	env = consEnv(t, &consolidateAgent{merge: mergeSignIn}, mergeCaps)
	code, out, errOut = runInit(env, "--consolidate", "--write", "--json")
	var doc struct {
		Write  map[string]json.RawMessage
		Report json.RawMessage
	}
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	t.Logf("--json write.report:\n%s", doc.Write["report"])
	var rep map[string]json.RawMessage
	if err := json.Unmarshal(doc.Write["report"], &rep); err != nil || len(rep) != 7 {
		t.Fatalf("write.report is not the seven measures (%v):\n%s", err, out)
	}
	var cons map[string]any
	if err := json.Unmarshal(rep["consolidation"], &cons); err != nil || cons["cant_be_given"] != nil || cons["capability_merges"] != 1.0 {
		t.Errorf("write.report.consolidation = %s, want its counts", rep["consolidation"])
	}
	if doc.Report != nil {
		t.Errorf("--write --json has the report twice, beside the write: %s", doc.Report)
	}
	var body string
	if err := json.Unmarshal(doc.Write["body"], &body); err != nil {
		t.Fatal(err)
	}
	sectionBetween(t, body)
}

// TestReportFullRunWithoutWrite: a full run that does not write ends with
// the report, after the consolidation's own output, plain and under report
// in --json. It stores the areas, the walk and the placements, so every
// measure is given and none says it can't be; the exit code is the run's
// own, 0.
func TestReportFullRunWithoutWrite(t *testing.T) {
	remoteRepo(t)
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()))
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	t.Logf("plain output's report:\n%s", out[max(strings.Index(out, reportHead), 0):])
	cons, at := strings.Index(out, "Consolidated 2 capabilities"), strings.Index(out, reportHead)
	if cons < 0 || at < cons || strings.Count(out, reportHead) != 1 || strings.Contains(out, sectionHead) {
		t.Fatalf("the report is not printed once after the consolidation:\n%s", out)
	}
	report := out[at:]
	for _, want := range []string{"Source files: 2\n  in a capability: 2 (100%)\n", "PRs placed: 1 of 1 (100%)\n", "PRs in one capability: ", "Capabilities with no PR or change: 1 of 2 (50%)\n",
		"Rules, by where their reasons came from: 2\n", "Consolidation:\n", "Spend per phase:\n  discover: ", "\n  walk: "} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
	for _, why := range notStored {
		if strings.Contains(report, why) {
			t.Errorf("a measure of a full run says %q:\n%s", why, report)
		}
	}
	if !strings.HasSuffix(errOut, "physa init: this run spent 81k tokens.\n") {
		t.Errorf("stderr does not end with the run's spend:\n%s", errOut)
	}

	remoteRepo(t)
	code, out, errOut = runInit(fullEnv(t, onePR(), newRoutedAgent()), "--json")
	var doc struct {
		Recovery, Consolidation json.RawMessage
		Report                  map[string]map[string]any
	}
	if code != 0 || json.Unmarshal([]byte(out), &doc) != nil || doc.Recovery == nil || doc.Consolidation == nil {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	js, _ := json.MarshalIndent(doc.Report, "", "  ")
	t.Logf("--json report:\n%s", js)
	for _, key := range append([]string{"consolidation", "spend"}, walkMeasures...) {
		if m := doc.Report[key]; len(m) == 0 || m["cant_be_given"] != nil {
			t.Errorf("report.%s = %v, want the measure", key, m)
		}
	}
}

// TestReportFullRunWriting: a full run with --write shows the report through
// the body alone, its walk measures given.
func TestReportFullRunWriting(t *testing.T) {
	remoteRepo(t)
	code, out, errOut := runInit(fullEnv(t, onePR(), newRoutedAgent()), "--write")
	if code != 0 {
		t.Fatalf("code %d\nstdout %s\nstderr %s", code, out, errOut)
	}
	sectionBetween(t, out)
	if strings.Contains(out, reportHead) {
		t.Errorf("--write printed the report beside the body's section:\n%s", out)
	}
	if !strings.Contains(out, "- PRs in one capability: ") || strings.Contains(out, "is stored") {
		t.Errorf("the section lacks the walk's measures:\n%s", out)
	}
	inOrder(t, errOut, "physa init: this run spent ")
	if strings.Contains(errOut[strings.Index(errOut, "physa init: this run spent "):], "physa init: recovering") {
		t.Errorf("the run's spend was said before recovery ended:\n%s", errOut)
	}
}

// TestReportNotForAPhaseAlone: --place, --consolidate and --recover alone
// print their own output and no report, plain or --json.
func TestReportNotForAPhaseAlone(t *testing.T) {
	cases := map[string][][]string{
		"place": {{"--place"}, {"--place", "--json"}},
		"consolidate and recover": {{"--consolidate"}, {"--consolidate", "--json"},
			{"--recover", "--only", "billing"}, {"--recover", "--only", "billing", "--json"}},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			var env Env
			if name == "place" {
				env, _ = placeEnv(t)
			} else {
				env = consEnv(t, &consolidateAgent{merge: mergeSignIn}, mergeCaps)
			}
			for _, args := range lines {
				code, out, errOut := runInit(env, args...)
				if code != 0 || out == "" {
					t.Fatalf("%v: code %d\nstdout %s\nstderr %s", args, code, out, errOut)
				}
				noReport(t, args, out)
			}
		})
	}
}

// TestReportNotForANewCodebase: a new codebase's run, writing or not, prints
// no report and its body has no measures.
func TestReportNotForANewCodebase(t *testing.T) {
	newCodebaseRepo(t)
	answers := t.TempDir()
	write(t, answers, "answers.txt", "1: Lathe\n2: Lathe turns sketches into cut lists.\n")
	for _, args := range [][]string{{}, {"--json"}, {"--write"}, {"--write", "--json"}} {
		args = append(args, "--answers", answers+"/answers.txt")
		code, out, errOut := runInit(fullEnv(t, nil, newRoutedAgent()), args...)
		if code != 0 || out == "" {
			t.Fatalf("%v: code %d\nstdout %s\nstderr %s", args, code, out, errOut)
		}
		noReport(t, args, out)
		if args[0] == "--write" { // so the next --write writes the branch afresh
			gitIn(t, ".", "branch", "-q", "-D", "physa/init")
		}
	}
}

// noReport fails the test when out, the output of args, holds the report.
func noReport(t *testing.T, args []string, out string) {
	t.Helper()
	if strings.Contains(out, "Measures") || strings.Contains(out, `"report"`) || strings.Contains(out, "cant_be_given") {
		t.Errorf("%v printed the report:\n%s", args, out)
	}
}
