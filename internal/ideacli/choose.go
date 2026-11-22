package ideacli

import (
	"fmt"
	"strings"

	"github.com/dphaener/physa/internal/picker"
)

// UsageError is a command line that cannot be run as given; Failed prints
// its message and the command's help.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// Choice is one slug a person may choose, with the text it stands for and
// the facts shown beside it in the picker.
type Choice struct{ Slug, Text, Detail string }

// ChooseSlug returns the slug of the kind ("idea", "bet") a command is
// about: the one given wins; with none given, a person at a terminal
// chooses one from choices; anywhere else it is a UsageError listing the
// choices, in the order given (newest first), or saying there are none.
func (e *Env) ChooseSlug(kind, given string, choices []Choice) (string, error) {
	if given != "" {
		return given, nil
	}
	if len(choices) == 0 {
		return "", &UsageError{fmt.Sprintf("name %s %s; there are none yet", article(kind), kind)}
	}
	if e.Interactive() {
		items := make([]picker.Item, len(choices))
		for i, c := range choices {
			items[i] = picker.Item{Slug: c.Slug, Text: c.Text, Detail: c.Detail}
		}
		return e.Pick("Choose "+article(kind)+" "+kind, items)
	}
	width := 0
	for _, c := range choices {
		width = max(width, len(c.Slug))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "name %s %s; the %ss are:", article(kind), kind, kind)
	for _, c := range choices {
		fmt.Fprintf(&b, "\n  %s  %s", c.Slug+strings.Repeat(" ", width-len(c.Slug)), c.Text)
	}
	return "", &UsageError{b.String()}
}

func article(kind string) string {
	if kind != "" && strings.ContainsRune("aeiou", rune(kind[0])) {
		return "an"
	}
	return "a"
}
