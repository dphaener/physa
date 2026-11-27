package other

import (
	"testing"

	cfg "example.com/testswap/internal/config"
)

func TestSwapThroughImport(t *testing.T) {
	cfg.Mode = "fake"
}

func TestShadowedImport(t *testing.T) {
	cfg := struct{ Mode string }{}
	cfg.Mode = "x"
}
