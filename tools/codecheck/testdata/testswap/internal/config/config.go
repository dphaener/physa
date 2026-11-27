// Package config holds package variables a test must not swap.
package config

// Mode is exported so another package's test can reach it.
var Mode = "real"

// Table is a map a test must not write through.
var Table = map[string]string{}

var hook = func() {}

// Run calls the hook.
func Run() { hook() }
