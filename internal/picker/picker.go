// Package picker asks a person at a terminal: choose one item from a
// filterable list, confirm, or type a value; it is used only when
// output.Interactive says a person is there.
package picker

import (
	"errors"
	"os"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// ErrAborted means the person left the question without answering.
var ErrAborted = errors.New("aborted")

// ErrNoItems means there was nothing to choose from.
var ErrNoItems = errors.New("nothing to choose from")

// Item is one choice: a slug, the text it stands for and optional detail.
type Item struct{ Slug, Text, Detail string }

// listItem shows the slug as title and the text as description; the
// detail is searchable too.
type listItem struct{ Item }

func (i listItem) Title() string       { return i.Slug }
func (i listItem) Description() string { return i.Text }
func (i listItem) FilterValue() string {
	return strings.TrimSpace(i.Slug + " " + i.Text + " " + i.Detail)
}

// model is the Bubble Tea model behind Pick.
type model struct {
	list    list.Model
	chosen  string
	aborted bool
}

func newModel(title string, items []Item) model {
	entries := make([]list.Item, len(items))
	for i, it := range items {
		entries[i] = listItem{it}
	}
	l := list.New(entries, list.NewDefaultDelegate(), 80, 24)
	l.Title = title
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(false)
	l.KeyMap.Quit.SetEnabled(false)
	l.KeyMap.ForceQuit.SetEnabled(false)
	return model{list: l}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyPressMsg:
		filtering := m.list.FilterState() == list.Filtering
		switch msg.String() {
		case "ctrl+c":
			m.aborted = true
			return m, tea.Quit
		case "esc":
			if m.list.FilterState() == list.Unfiltered {
				m.aborted = true
				return m, tea.Quit
			}
		case "enter":
			if !filtering {
				if it, ok := m.list.SelectedItem().(listItem); ok {
					m.chosen = it.Slug
					return m, tea.Quit
				}
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() tea.View { return tea.NewView(m.list.View()) }

// Pick shows the items in a filterable list and returns the chosen slug,
// or ErrAborted when the person leaves with Esc or Ctrl+C.
func Pick(in, out *os.File, title string, items []Item) (string, error) {
	if len(items) == 0 {
		return "", ErrNoItems
	}
	final, err := tea.NewProgram(newModel(title, items), tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return "", err
	}
	m, ok := final.(model)
	if !ok || m.aborted || m.chosen == "" {
		return "", ErrAborted
	}
	return m.chosen, nil
}
