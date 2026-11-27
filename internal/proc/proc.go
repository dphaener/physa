// Package proc holds the state of the process a command runs in, its working directory and environment, read once by the command and passed down, so nothing below reads the process itself.
package proc

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

// State is the working directory and environment a command runs in.
// Dir is the working directory by the name commands hand down; in the
// real run it is ".". Wd and WdErr are what the process's working
// directory lookup returned, once. Env is the environment as
// "key=value" entries.
type State struct {
	Dir   string
	Wd    string
	WdErr error
	Env   []string
}

// Getwd returns the working directory read at the start, or its error.
func (s State) Getwd() (string, error) { return s.Wd, s.WdErr }

// Getenv returns the value of key, or "" when it is unset.
func (s State) Getenv(key string) string {
	v, _ := s.LookupEnv(key)
	return v
}

// LookupEnv returns the value of key and whether it is set. When a key
// appears twice the last one wins, as it does for a child started with Env.
func (s State) LookupEnv(key string) (string, bool) {
	for i := len(s.Env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(s.Env[i], "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// Environ returns a copy of the environment the caller may append to.
func (s State) Environ() []string {
	return append([]string(nil), s.Env...)
}

// Home returns the home directory, as os.UserHomeDir does for the process.
func (s State) Home() (string, error) {
	key, name := "HOME", "$HOME"
	switch runtime.GOOS {
	case "windows":
		key, name = "USERPROFILE", "%userprofile%"
	case "plan9":
		key, name = "home", "$home"
	}
	if h := s.Getenv(key); h != "" {
		return h, nil
	}
	return "", errors.New(name + " is not defined")
}

// Abs returns path as an absolute, cleaned path, as filepath.Abs does,
// joining a relative path to Wd. With no working directory it returns WdErr.
func (s State) Abs(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if s.WdErr != nil {
		return "", s.WdErr
	}
	return filepath.Join(s.Wd, path), nil
}
