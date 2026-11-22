package picker

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

var items = []Item{
	{Slug: "alpha", Text: "first idea"},
	{Slug: "beta", Text: "second idea"},
	{Slug: "gamma", Text: "third idea", Detail: "needle"},
}

func run(t *testing.T, drive func(tm *teatest.TestModel)) model {
	t.Helper()
	tm := teatest.NewTestModel(t, newModel("Pick an idea", items))
	drive(tm)
	return tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(model)
}

func key(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code})
}

func TestPickByArrowKeysAndEnter(t *testing.T) {
	m := run(t, func(tm *teatest.TestModel) {
		key(tm, tea.KeyDown)
		key(tm, tea.KeyEnter)
	})
	if m.chosen != "beta" || m.aborted {
		t.Fatalf("chosen %q aborted %v, want beta", m.chosen, m.aborted)
	}
}

func TestPickFirstOnEnter(t *testing.T) {
	m := run(t, func(tm *teatest.TestModel) { key(tm, tea.KeyEnter) })
	if m.chosen != "alpha" {
		t.Fatalf("chosen %q, want alpha", m.chosen)
	}
}

func TestFilterThenPick(t *testing.T) {
	m := run(t, func(tm *teatest.TestModel) {
		tm.Type("/")
		tm.Type("needle")
		key(tm, tea.KeyEnter)
		key(tm, tea.KeyEnter)
	})
	if m.chosen != "gamma" {
		t.Fatalf("chosen %q, want gamma", m.chosen)
	}
}

func TestAbortWithEsc(t *testing.T) {
	m := run(t, func(tm *teatest.TestModel) { key(tm, tea.KeyEscape) })
	if !m.aborted || m.chosen != "" {
		t.Fatalf("chosen %q aborted %v, want abort", m.chosen, m.aborted)
	}
}

func TestAbortWithCtrlC(t *testing.T) {
	m := run(t, func(tm *teatest.TestModel) {
		tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	})
	if !m.aborted {
		t.Fatal("ctrl+c did not abort")
	}
}

func TestPickWithoutItems(t *testing.T) {
	if _, err := Pick(nil, nil, "x", nil); !errors.Is(err, ErrNoItems) {
		t.Fatalf("err %v, want ErrNoItems", err)
	}
}

func accessible(input string) (config, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return config{accessible: true, in: strings.NewReader(input), out: out}, out
}

func TestConfirmReadsYesAndNo(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "yes\n": true, "n\n": false, "\n": false} {
		c, _ := accessible(input)
		got, err := confirm(c, "Stop the bet?")
		if err != nil || got != want {
			t.Errorf("confirm(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
}

func TestAskReturnsTypedValue(t *testing.T) {
	c, out := accessible("200k\n")
	got, err := ask(c, "Appetite", "200k", nil)
	if err != nil || got != "200k" {
		t.Fatalf("ask = %q, %v", got, err)
	}
	if !strings.Contains(out.String(), "Appetite") {
		t.Errorf("title missing from output %q", out.String())
	}
}

func TestAskInvalidInputRefusedByValidate(t *testing.T) {
	c, out := accessible("nope\n200k\n")
	validate := func(s string) error {
		if s != "200k" {
			return errors.New("not an appetite")
		}
		return nil
	}
	got, err := ask(c, "Appetite", "", validate)
	if err != nil || got != "200k" {
		t.Fatalf("ask = %q, %v; want 200k", got, err)
	}
	if !strings.Contains(out.String(), "not an appetite") {
		t.Errorf("refusal not shown in %q", out.String())
	}
}
