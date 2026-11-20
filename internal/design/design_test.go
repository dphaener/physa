package design

import (
	"io/fs"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(Files, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var (
	declRe = regexp.MustCompile(`--([a-z0-9-]+)\s*:\s*([^;]+);`)
	useRe  = regexp.MustCompile(`var\(--([a-z0-9-]+)`)
	urlRe  = regexp.MustCompile(`url\("([^"]+)"\)`)
)

// themes splits tokens.css into its three blocks: light (bare :root), the
// system dark block, and the data-theme dark block.
func themes(t *testing.T) (light, sysDark, attrDark map[string]string) {
	css := read(t, "tokens.css")
	i := strings.Index(css, "@media (prefers-color-scheme: dark)")
	j := strings.Index(css, `:root[data-theme="dark"]`)
	if i < 0 || j < i {
		t.Fatal("tokens.css needs a light :root, then a system dark block, then a data-theme dark block")
	}
	parse := func(s string) map[string]string {
		m := map[string]string{}
		for _, d := range declRe.FindAllStringSubmatch(s, -1) {
			m[d[1]] = strings.TrimSpace(d[2])
		}
		return m
	}
	return parse(css[:i]), parse(css[i:j]), parse(css[j:])
}

func TestComponentsUseOnlyDefinedTokens(t *testing.T) {
	light, _, _ := themes(t)
	for _, u := range useRe.FindAllStringSubmatch(read(t, "components.css"), -1) {
		if _, ok := light[u[1]]; !ok && u[1] != "w" && u[1] != "used" {
			t.Errorf("components.css uses --%s, which tokens.css does not define", u[1])
		}
	}
}

func TestEveryColourHasADarkValueInBothDarkBlocks(t *testing.T) {
	light, sysDark, attrDark := themes(t)
	for name, v := range light {
		if !strings.HasPrefix(v, "#") {
			continue
		}
		if sysDark[name] == "" || sysDark[name] != attrDark[name] {
			t.Errorf("--%s: dark values differ or are missing (system %q, data-theme %q)", name, sysDark[name], attrDark[name])
		}
	}
}

func TestFontsAreEmbedded(t *testing.T) {
	for _, u := range urlRe.FindAllStringSubmatch(read(t, "tokens.css"), -1) {
		if _, err := fs.Stat(Files, u[1]); err != nil {
			t.Errorf("tokens.css points at %s, which is not embedded", u[1])
		}
	}
}

// pairs are the text and mark pairs the brand book promises, with the
// contrast each needs: 4.5 for text, 3 for marks and control borders.
var pairs = []struct {
	fg, bg string
	min    float64
}{
	{"ink", "surface", 4.5}, {"ink", "surface-raised", 4.5}, {"ink", "surface-sunk", 4.5},
	{"ink-muted", "surface", 4.5}, {"ink-muted", "surface-raised", 4.5}, {"ink-muted", "surface-sunk", 4.5},
	{"accent", "surface", 4.5}, {"accent", "surface-raised", 4.5}, {"on-accent", "accent", 4.5},
	{"accent", "accent-soft", 4.5}, {"lesson-ink", "surface", 4.5}, {"lesson-ink", "lesson-soft", 4.5},
	{"danger", "surface", 4.5}, {"danger", "danger-soft", 4.5}, {"steady", "surface", 4.5}, {"steady", "steady-soft", 4.5},
	{"tube-capability", "surface", 3}, {"tube-idea", "surface", 3}, {"tube-idea", "surface-sunk", 3},
	{"lesson", "surface", 3}, {"lesson", "surface-sunk", 3}, {"line-strong", "surface", 3}, {"line-strong", "surface-raised", 3},
	{"focus", "surface", 3}, {"focus", "surface-raised", 3}, {"focus", "surface-sunk", 3},
}

func luminance(t *testing.T, hex string) float64 {
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("not a #rrggbb colour: %q", hex)
	}
	var l float64
	for i, w := range []float64{.2126, .7152, .0722} {
		n, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		c := float64(n) / 255
		if c <= .03928 {
			c /= 12.92
		} else {
			c = math.Pow((c+.055)/1.055, 2.4)
		}
		l += w * c
	}
	return l
}

func TestContrastInBothThemes(t *testing.T) {
	light, dark, _ := themes(t)
	for theme, m := range map[string]map[string]string{"light": light, "dark": dark} {
		for _, p := range pairs {
			a, b := luminance(t, m[p.fg]), luminance(t, m[p.bg])
			ratio := (math.Max(a, b) + .05) / (math.Min(a, b) + .05)
			if ratio < p.min {
				t.Errorf("%s: --%s on --%s is %.2f:1, needs %.1f:1", theme, p.fg, p.bg, ratio, p.min)
			}
		}
	}
}
