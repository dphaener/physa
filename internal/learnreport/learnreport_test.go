package learnreport

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dphaener/physa/internal/learn"
)

func sample() learn.Result {
	ref := learn.ChangeRef{Change: "aaaaaaa1111", Short: "aaaaaaa", Subject: "Revert \"feat: Add f\""}
	inc := learn.ChangeRef{Change: "bbbbbbb2222", Short: "bbbbbbb", Subject: "feat: Add f"}
	return learn.Result{
		Version: 2, Range: "main", Changes: 2,
		Unavailable: []learn.Unavailable{
			{Kind: learn.KindCorrection, Reason: "gh is not installed"},
			{Kind: learn.KindRecurringFinding, Reason: "needs the engine: x"},
		},
		SessionsUnavailable: []learn.SessionGap{},
		RecordsMissing:      2,
		Learn: []learn.ChangeSignals{{
			ChangeRef: ref,
			Signals:   []learn.Signal{{Kind: learn.KindRevert, Rule: "revert-message", Evidence: []string{"reverts bbbbbbb"}}},
		}},
		Nothing:    []learn.ChangeRef{},
		Incomplete: []learn.Incomplete{{ChangeRef: inc, Reason: "correction and argued-options not checked: gh is not installed"}},
		LearnedNow: []string{},
	}
}

func TestWriteText(t *testing.T) {
	res := sample()
	var b bytes.Buffer
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	text := b.String()
	for _, want := range []string{
		"Learning from 2 changes (main), 0 already learned.\n",
		"Forge: unavailable (gh is not installed): correction and argued-options signals not checked.\n",
		"Slice records: 2 of 2 changes had none and were not counted.\n",
		"Not checked: correction (gh is not installed).\n",
		"Session logs: 0 read, 0 unavailable.\n",
		"Not checked: recurring-finding (needs the engine: ",
		"\nSignals (1 change):\n  aaaaaaa Revert \"feat: Add f\"\n    revert [revert-message]\n      reverts bbbbbbb\n",
		"\nNothing to learn (0):\n",
		"\nNot fully triaged (1):\n  bbbbbbb feat: Add f (correction and argued-options not checked: gh is not installed)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	res.Forge = "github"
	b.Reset()
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Forge: github\n") {
		t.Errorf("text:\n%s", b.String())
	}
}

func TestWriteTextRecordsUnavailable(t *testing.T) {
	res := sample()
	res.Unavailable = append(res.Unavailable, learn.Unavailable{Kind: learn.KindServedNotLoaded, Reason: "the store cannot be opened"})
	var b bytes.Buffer
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Slice records: unavailable (the store cannot be opened).\n") {
		t.Errorf("text:\n%s", b.String())
	}
}

func TestWriteTextWatermark(t *testing.T) {
	res := sample()
	res.Watermark, res.Through = "1234567890abc", "fedcba0987654"
	var b bytes.Buffer
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	if want := "Learned through fedcba0 (was 1234567)\n"; !strings.Contains(b.String(), want) {
		t.Errorf("text lacks %q:\n%s", want, b.String())
	}
	res.Watermark = ""
	b.Reset()
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "(was none)\n") {
		t.Errorf("text:\n%s", b.String())
	}
}

func TestWriteJSON(t *testing.T) {
	res := sample()
	res.Watermark, res.Through = "aaa", "bbb"
	var b bytes.Buffer
	if err := WriteJSON(&b, res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"version":2`, `"sessions_read":0`, `"sessions_unavailable":[]`, `"incomplete":[{`, `"already_learned":0`, `"records_missing":2`, `"watermark":"aaa"`, `"through":"bbb"`, `"learned_now":[]`, `"nothing":[]`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("json lacks %s: %s", want, b.String())
		}
	}
	var back learn.Result
	if err := json.Unmarshal(b.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, res) {
		t.Errorf("round trip = %+v\nwant         %+v", back, res)
	}
}

func TestJSONOmitsMessageAndCapabilities(t *testing.T) {
	res := sample()
	var plain, with bytes.Buffer
	if err := WriteJSON(&plain, res); err != nil {
		t.Fatal(err)
	}
	res.Learn[0].Message = "fixed after review\n\nCapability: learning"
	res.Learn[0].Capabilities = []string{"learning"}
	if err := WriteJSON(&with, res); err != nil {
		t.Fatal(err)
	}
	if plain.String() != with.String() {
		t.Errorf("json differs with message and capabilities set:\n%s\nwant\n%s", with.String(), plain.String())
	}
	for _, banned := range []string{`"message"`, `"capabilities"`, "Capability: learning"} {
		if strings.Contains(with.String(), banned) {
			t.Errorf("json carries %s: %s", banned, with.String())
		}
	}
}

// withSessions is sample with a session signal, quotes and gaps.
func withSessions() learn.Result {
	res := sample()
	res.SessionsRead = 3
	res.SessionsUnavailable = []learn.SessionGap{
		{Change: "aaaaaaa", Session: "0a1b2c3d-0000-4000-8000-000000000000", Reason: "no log on this machine"},
		{Change: "bbbbbbb", Session: "0a1b2c3d-0000-4000-8000-000000000000", Reason: "no log on this machine"},
		{Change: "bbbbbbb", Session: "s9", Reason: "unreadable"},
	}
	res.Learn[0].Signals = append(res.Learn[0].Signals,
		learn.Signal{Kind: learn.KindAnsweredAsk, Rule: learn.RulePersonAnswered, Changes: []string{"aaaaaaa"},
			Evidence: []string{"1 answered ask(s) in session 9f8e7d6c"}, Session: "9f8e7d6c-1111-4000-8000-000000000000",
			MatchedBy: learn.MatchedByTrailer, Quotes: []learn.Quote{{Author: "person", Text: "Which-store-QUOTE?",
				Options: []string{"sqlite-QUOTE", "files-QUOTE"}, Answer: "files-QUOTE, notes-QUOTE"}}},
		learn.Signal{Kind: learn.KindIntervention, Rule: learn.RuleMidRun, Changes: []string{"aaaaaaa"},
			Evidence: []string{"1 message(s) mid-run in session 12345678"}, Session: "12345678-2222-4000-8000-000000000000",
			MatchedBy: learn.MatchedByTime, Quotes: []learn.Quote{{Author: "message in the session", Text: "stop-QUOTE"}}})
	return res
}

func TestWriteTextSessions(t *testing.T) {
	var b bytes.Buffer
	if err := WriteText(&b, withSessions()); err != nil {
		t.Fatal(err)
	}
	text := b.String()
	for _, want := range []string{
		"Session logs: 3 read, 2 unavailable.\n" +
			"  aaaaaaa 0a1b2c3d: no log on this machine\n" +
			"  bbbbbbb 0a1b2c3d: no log on this machine\n" +
			"  bbbbbbb s9: unreadable\n",
		"    answered-ask [person-answered]\n      1 answered ask(s) in session 9f8e7d6c\n      session 9f8e7d6c, matched by trailer\n",
		"    intervention [message-mid-run]\n      1 message(s) mid-run in session 12345678\n      session 12345678, matched by time\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "matched by") != 2 {
		t.Errorf("only session signals name their session:\n%s", text)
	}
}

func TestWriteTextNoSessionReader(t *testing.T) {
	res := sample()
	res.Unavailable = append(res.Unavailable,
		learn.Unavailable{Kind: learn.KindAnsweredAsk, Reason: "no session reader"},
		learn.Unavailable{Kind: learn.KindIntervention, Reason: "no session reader"},
		learn.Unavailable{Kind: learn.KindFixedFinding, Reason: "no session reader"})
	var b bytes.Buffer
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Session logs: unavailable (no session reader).\n", "Not checked: fixed-finding (no session reader).\n"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, b.String())
		}
	}
}

func TestNoQuotesInOutput(t *testing.T) {
	res := withSessions()
	var text, js bytes.Buffer
	if err := WriteText(&text, res); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(&js, res); err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"text": text.String(), "json": js.String()} {
		for _, banned := range []string{"QUOTE", "message in the session", `"quotes"`} {
			if strings.Contains(out, banned) {
				t.Errorf("%s carries %q:\n%s", name, banned, out)
			}
		}
	}
	for _, want := range []string{`"session":"9f8e7d6c-1111-4000-8000-000000000000"`, `"matched_by":"trailer"`, `"sessions_read":3`,
		`"sessions_unavailable":[{"change":"aaaaaaa","session":"0a1b2c3d-0000-4000-8000-000000000000","reason":"no log on this machine"}`} {
		if !strings.Contains(js.String(), want) {
			t.Errorf("json lacks %s: %s", want, js.String())
		}
	}
}

func TestWriteTextWordsCountsOfOneAndMore(t *testing.T) {
	res := sample()
	res.Changes, res.RecordsMissing = 1, 1
	var b bytes.Buffer
	if err := WriteText(&b, res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Learning from 1 change (main)", "Slice records: 1 of 1 change had none"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, b.String())
		}
	}
	res.Changes = 2
	res.Learn = append(res.Learn, res.Learn[0])
	b.Reset()
	if err := WriteText(&b, res); err != nil || !strings.Contains(b.String(), "Signals (2 changes):") {
		t.Errorf("want plural signals heading, err %v:\n%s", err, b.String())
	}
}
