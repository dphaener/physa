package picker

import (
	"errors"
	"io"
	"os"

	"charm.land/huh/v2"
)

// config lets tests drive the Huh forms without a terminal.
type config struct {
	accessible bool
	in         io.Reader
	out        io.Writer
}

func (c config) form(f huh.Field) *huh.Form {
	return huh.NewForm(huh.NewGroup(f)).
		WithAccessible(c.accessible).
		WithInput(c.in).
		WithOutput(c.out)
}

func (c config) run(f huh.Field) error {
	err := c.form(f).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

// Confirm asks a yes or no question; the answer defaults to no.
func Confirm(in, out *os.File, question string) (bool, error) {
	return confirm(config{in: in, out: out}, question)
}

func confirm(c config, question string) (bool, error) {
	var yes bool
	if err := c.run(huh.NewConfirm().Title(question).Value(&yes)); err != nil {
		return false, err
	}
	return yes, nil
}

// Ask asks for a typed value; validate (when not nil) refuses bad input.
func Ask(in, out *os.File, title, placeholder string, validate func(string) error) (string, error) {
	return ask(config{in: in, out: out}, title, placeholder, validate)
}

func ask(c config, title, placeholder string, validate func(string) error) (string, error) {
	var value string
	field := huh.NewInput().Title(title).Placeholder(placeholder).Value(&value)
	if validate != nil {
		field = field.Validate(validate)
	}
	if err := c.run(field); err != nil {
		return "", err
	}
	return value, nil
}
