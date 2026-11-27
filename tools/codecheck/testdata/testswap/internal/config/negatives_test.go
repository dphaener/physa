package config

import "testing"

var scratch = 0

func TestShadowedParameter(t *testing.T) {
	set := func(Mode string) { Mode = "x" }
	set("y")
}

func TestDefine(t *testing.T) {
	Mode := "local"
	_ = Mode
}

func TestDeclaredInTestFile(t *testing.T) {
	scratch = 1
	scratch++
}

func TestBlank(t *testing.T) {
	_ = Mode
}

func TestFieldOfLocal(t *testing.T) {
	s := struct{ Mode string }{}
	s.Mode = "x"
	m := map[string]string{}
	m["a"] = "b"
}
