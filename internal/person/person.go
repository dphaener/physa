// Package person names the person running Physa. It prefers the person's
// setting, then their forge login (kept in the setting for next time), then
// the part of their git email before the "@". It never uses git's author
// name. Resolve runs no program: callers hand in the lookups, and ForRepo
// hands in the usual ones for a repository.
package person

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dphaener/physa/internal/configdir"
)

// Source says where a name came from.
type Source string

const (
	// Setting is the file in the config directory.
	Setting Source = "setting"
	// Forge is the person's login on the forge.
	Forge Source = "forge"
	// Email is the part of git's email before the "@".
	Email Source = "email"
)

// ErrUnknown means no source named the person.
var ErrUnknown = errors.New("person unknown: no setting, no forge login and no git email")

// Options holds where to look. A nil lookup is skipped.
type Options struct {
	ConfigDir string
	Login     func() (string, error)
	Email     func() (string, error)
}

// Name is the resolved person.
type Name struct {
	Name   string
	Source Source
	// Kept says a forge login was written to the setting file.
	Kept bool
}

const fileName = "person"

// ConfigDir returns Physa's config directory.
func ConfigDir() (string, error) {
	d, err := configdir.Dir()
	if err != nil {
		return "", fmt.Errorf("person: %w", err)
	}
	return d, nil
}

// Resolve names the person: setting, else forge login (kept), else email.
func Resolve(o Options) (Name, error) {
	path := filepath.Join(o.ConfigDir, fileName)
	if b, err := os.ReadFile(path); err == nil {
		if line := firstLine(string(b)); line != "" {
			if strings.ContainsAny(line, " \t\r\v\f") {
				return Name{}, fmt.Errorf("person: %s: name %q must not contain whitespace", path, line)
			}
			return Name{Name: line, Source: Setting}, nil
		}
	}
	if o.Login != nil {
		if login, err := o.Login(); err == nil {
			if login = strings.TrimSpace(login); valid(login) {
				return Name{Name: login, Source: Forge, Kept: keep(o.ConfigDir, path, login)}, nil
			}
		}
	}
	if o.Email != nil {
		if addr, err := o.Email(); err == nil {
			local, _, _ := strings.Cut(strings.TrimSpace(addr), "@")
			if valid(local) {
				return Name{Name: local, Source: Email}, nil
			}
		}
	}
	return Name{}, ErrUnknown
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

func valid(s string) bool {
	return s != "" && !strings.ContainsAny(s, " \t\r\n\v\f")
}

func keep(dir, path, login string) bool {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	return os.WriteFile(path, []byte(login+"\n"), 0o600) == nil
}
