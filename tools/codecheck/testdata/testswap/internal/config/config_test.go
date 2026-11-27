package config

import "testing"

func TestOwnVar(t *testing.T) {
	Mode = "fake"
}

func TestMapEntry(t *testing.T) {
	Table["a"] = "b"
}

func TestCleanup(t *testing.T) {
	old := hook
	t.Cleanup(func() { hook = old })
}
