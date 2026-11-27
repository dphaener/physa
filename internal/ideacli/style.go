package ideacli

import (
	"io"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/dphaener/physa/internal/output"
)

// The styles a person at a terminal sees: the design's accent for slugs and
// titles, faint for keys and facts. Nothing else is marked.
var (
	accent = lipgloss.Color("#6d9bff")
	titleS = lipgloss.NewStyle().Bold(true).Foreground(accent)
	slugS  = lipgloss.NewStyle().Foreground(accent)
	boldS  = lipgloss.NewStyle().Bold(true)
	faintS = lipgloss.NewStyle().Faint(true)
)

// View prints a command's answer: styled with Lip Gloss for a person at a
// terminal, written through a writer that drops what the output cannot show
// (so a pipe never gets escapes); plain through output.For otherwise.
type View struct {
	w      io.Writer
	styled bool
	plain  *output.Printer
}

// View makes the View for w, styled when Interactive says a person is there.
func (e *Env) View(w io.Writer) *View {
	return &View{w: w, styled: e.Interactive(), plain: output.For(w)}
}

// line writes to the terminal, so a failed write is not something to act on,
// as with fmt.Fprint.
func (v *View) line(s string) { _, _ = lipgloss.Fprintln(v.w, s) }

// Title prints s as the answer's title.
func (v *View) Title(s string) {
	if !v.styled {
		v.plain.Heading(s)
		return
	}
	v.line(titleS.Render(s))
}

// Line prints s as it is.
func (v *View) Line(s string) {
	if !v.styled {
		v.plain.Line(s)
		return
	}
	v.line(s)
}

// Fields prints one "key  value" line per pair, keys padded to one width.
func (v *View) Fields(pairs ...[2]string) {
	if !v.styled {
		v.plain.Fields(pairs...)
		return
	}
	width := 0
	for _, kv := range pairs {
		width = max(width, len([]rune(kv[0])))
	}
	for _, kv := range pairs {
		v.line(faintS.Render(pad(kv[0], width)) + "  " + kv[1])
	}
}

// Section prints a part's name and its body indented under it.
func (v *View) Section(name, body string) {
	head := name
	if v.styled {
		head = boldS.Render(name)
	}
	v.Line("\n" + head)
	for _, l := range strings.Split(strings.TrimSpace(body), "\n") {
		v.Line("  " + l)
	}
}

// Table prints rows under header in padded columns. Styled, the first
// column is a slug and the last holds facts.
func (v *View) Table(header []string, rows [][]string) {
	if !v.styled {
		v.plain.Table(header, rows)
		return
	}
	widths := make([]int, len(header))
	for _, r := range append([][]string{header}, rows...) {
		for i, c := range r {
			if i < len(widths) {
				widths[i] = max(widths[i], len([]rune(c)))
			}
		}
	}
	row := func(r []string, style func(i int, s string) string) {
		cells := make([]string, len(r))
		for i, c := range r {
			if i < len(widths)-1 {
				c = pad(c, widths[i])
			}
			cells[i] = style(i, c)
		}
		v.line(strings.Join(cells, "  "))
	}
	row(header, func(_ int, s string) string { return boldS.Render(s) })
	for _, r := range rows {
		row(r, func(i int, s string) string {
			switch i {
			case 0:
				return slugS.Render(s)
			case len(widths) - 1:
				return faintS.Render(s)
			}
			return s
		})
	}
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", width-len([]rune(s)))
}

// When shows an RFC 3339 time from the engine as a person reads it, in UTC
// to the minute; a time that does not parse is shown as it is.
func When(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}
