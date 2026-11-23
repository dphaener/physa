package ideacli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

// ReadFrom reads the file at path, or stdin for "-".
func (e *Env) ReadFrom(path string) (string, error) {
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(e.Stdin)
	} else {
		b, err = os.ReadFile(path)
	}
	return string(b), err
}

// WriteTemp writes md to a new temporary file named by pattern (as
// os.CreateTemp takes it) and returns its path.
func WriteTemp(pattern, md string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(md)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// Edit opens path in $VISUAL, else $EDITOR, else vi, on the terminal. The
// variable may carry arguments, split on spaces.
func (e *Env) Edit(path string) error {
	editor := strings.Fields(e.Getenv("VISUAL"))
	if len(editor) == 0 {
		editor = strings.Fields(e.Getenv("EDITOR"))
	}
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	cmd := exec.Command(editor[0], append(editor[1:], path)...)
	if e.Stdio[0] != nil {
		cmd.Stdin = e.Stdio[0]
	}
	if e.Stdio[1] != nil {
		cmd.Stdout = e.Stdio[1]
	}
	if e.Stdio[2] != nil {
		cmd.Stderr = e.Stdio[2]
	}
	if err := cmd.Run(); err != nil {
		return errors.New("the editor " + editor[0] + " failed: " + err.Error())
	}
	return nil
}
