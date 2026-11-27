// Package reader reads the process's state when it should not.
package reader

import (
	"os"
	osalias "os"
)

type lookup struct{}

// Getenv is a method on a local value, which is allowed.
func (lookup) Getenv(string) string { return "" }

// Where reads the working directory and the environment.
func Where() (string, string, []string) {
	wd, _ := os.Getwd()
	return wd, os.Getenv("HOME"), os.Environ()
}

// Aliased reads through an aliased import of os.
func Aliased() (string, bool) {
	return osalias.LookupEnv("CI")
}

// Dirs reads the home, config and cache directories and expands a string.
func Dirs() []string {
	h, _ := os.UserHomeDir()
	c, _ := os.UserConfigDir()
	k, _ := os.UserCacheDir()
	return []string{h, c, k, os.ExpandEnv("$HOME")}
}

// Local calls a method named Getenv on a local value, which is allowed.
func Local() string {
	var l lookup
	return l.Getenv("HOME")
}

// Allowed uses the functions the rule leaves alone.
func Allowed() string {
	return os.TempDir()
}

// Passed hands a function of os on as a value.
func Passed(use func(func(string) string)) {
	use(os.Getenv)
}
