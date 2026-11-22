// Package output prints what a command has to say: styled with Lip Gloss on a terminal, plain text in a pipe, in CI or for an agent, and JSON when asked, so every command speaks the same way.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// IsTerminal reports whether a person is at f: it is a character device, and
// not the null device, which is one too.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(fi, null)
}

// Printer writes a command's output, styled or plain.
type Printer struct {
	w      io.Writer
	styled bool
}

// New makes a Printer on w, styled when styled is true.
func New(w io.Writer, styled bool) *Printer { return &Printer{w: w, styled: styled} }

// For makes a Printer on w, styled when w is a terminal and NO_COLOR is unset.
func For(w io.Writer) *Printer {
	f, ok := w.(*os.File)
	_, noColor := os.LookupEnv("NO_COLOR")
	return New(w, ok && IsTerminal(f) && !noColor)
}

var (
	bold = lipgloss.NewStyle().Bold(true)
	dim  = lipgloss.NewStyle().Faint(true)
)

func (p *Printer) render(st lipgloss.Style, s string) string {
	if !p.styled {
		return s
	}
	return st.Render(s)
}

// Heading prints s, bold when styled.
func (p *Printer) Heading(s string) { p.Line(p.render(bold, s)) }

// Line prints s and a newline.
func (p *Printer) Line(s string) { fmt.Fprintln(p.w, s) }

// Fields prints one "key  value" line per pair, keys padded to one width and
// dimmed when styled.
func (p *Printer) Fields(pairs ...[2]string) {
	width := 0
	for _, kv := range pairs {
		width = max(width, len([]rune(kv[0])))
	}
	for _, kv := range pairs {
		key := kv[0] + strings.Repeat(" ", width-len([]rune(kv[0])))
		p.Line(p.render(dim, key) + "  " + kv[1])
	}
}

// Table prints header and rows in columns padded to the widest cell, the
// header bold when styled.
func (p *Printer) Table(header []string, rows [][]string) {
	widths := make([]int, len(header))
	for _, r := range append([][]string{header}, rows...) {
		for i, c := range r {
			if i < len(widths) {
				widths[i] = max(widths[i], len([]rune(c)))
			}
		}
	}
	line := func(r []string, st *lipgloss.Style) {
		cells := make([]string, len(r))
		for i, c := range r {
			if i < len(widths)-1 {
				c += strings.Repeat(" ", widths[i]-len([]rune(c)))
			}
			cells[i] = c
		}
		s := strings.Join(cells, "  ")
		if st != nil {
			s = p.render(*st, s)
		}
		p.Line(s)
	}
	line(header, &bold)
	for _, r := range rows {
		line(r, nil)
	}
}

// JSON writes v as one indented JSON document and a newline.
func JSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
