package state

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// logAppend is the log.append kind: it appends "<id> <text>" to a file in
// <storeDir>/logs/, once per intent. A payload that could write anywhere
// else, or more than one line, is refused: file must be a plain base name.
func logAppend(storeDir string) IntentKind {
	read := func(payload json.RawMessage) (l struct{ File, Text string }, err error) {
		if err = json.Unmarshal(payload, &l); err != nil {
			return l, fmt.Errorf("the payload must be {file, text}: %w", err)
		}
		if f := l.File; f == "" || f == "." || f == ".." || len(f) > 100 || strings.ContainsAny(f, "/\\\x00") || !filepath.IsLocal(f) {
			err = fmt.Errorf("file %q must be a plain file name of at most 100 bytes", f)
		} else if strings.ContainsAny(l.Text, "\r\n") || storeDir == "" {
			err = fmt.Errorf("log %s needs a text of one line and a state opened with its store directory", f)
		}
		return l, err
	}
	return IntentKind{
		Name:   "log.append",
		Repeat: "the line carries the intent's id, and a line already written is not written again",
		Check:  func(payload json.RawMessage) error { _, err := read(payload); return err },
		Do: func(_ context.Context, in Intent) (string, error) {
			l, err := read(in.Payload)
			if err == nil {
				err = os.MkdirAll(filepath.Join(storeDir, "logs"), 0o700)
			}
			if err != nil {
				return "", err
			}
			f, err := os.OpenFile(filepath.Join(storeDir, "logs", l.File), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				return "", err
			}
			defer f.Close()
			have, err := io.ReadAll(f)
			if err != nil {
				return "", err
			}
			mark := fmt.Sprintf("\n%d ", in.ID)
			if strings.Contains("\n"+string(have), mark) {
				return "already written", nil
			}
			if _, err := f.WriteString(mark[1:] + l.Text + "\n"); err != nil {
				return "", err
			}
			return "written", f.Sync()
		},
	}
}
