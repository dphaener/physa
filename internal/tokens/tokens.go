// Package tokens reads and writes an appetite the way people say it: a
// number of tokens such as 200k or 1.5m. It does no I/O.
package tokens

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parse reads an appetite: an integer, optionally ending in k (thousand)
// or m (million). Decimals need a suffix (1.5m); commas and underscores
// separate digits. The result is above zero.
func Parse(s string) (int64, error) {
	orig := s
	s = strings.NewReplacer(",", "", "_", "").Replace(strings.TrimSpace(s))
	mult, places := int64(1), 0
	if s != "" {
		switch s[len(s)-1] {
		case 'k', 'K':
			mult, places, s = 1_000, 3, s[:len(s)-1]
		case 'm', 'M':
			mult, places, s = 1_000_000, 6, s[:len(s)-1]
		}
	}
	bad := fmt.Errorf("%q is not a token count (try 200k, 1.5m or 50000)", orig)
	whole, frac, hasDot := strings.Cut(s, ".")
	if hasDot && (mult == 1 || len(frac) == 0 || len(frac) > places) {
		return 0, bad
	}
	if !digits(whole) || (hasDot && !digits(frac)) {
		return 0, bad
	}
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || n > math.MaxInt64/mult {
		return 0, fmt.Errorf("%q is too large", orig)
	}
	n *= mult
	if hasDot {
		f, _ := strconv.ParseInt(frac, 10, 64)
		for i := len(frac); i < places; i++ {
			f *= 10
		}
		n += f
	}
	if n <= 0 {
		return 0, fmt.Errorf("%q must be more than zero", orig)
	}
	return n, nil
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Format writes an appetite the way people say it: 200k, 1.5m, or the plain
// number when it is not a round multiple.
func Format(n int64) string {
	switch {
	case n >= 1_000_000 && n%100_000 == 0:
		if n%1_000_000 == 0 {
			return strconv.FormatInt(n/1_000_000, 10) + "m"
		}
		return fmt.Sprintf("%d.%dm", n/1_000_000, n%1_000_000/100_000)
	case n >= 1000 && n%1000 == 0:
		return strconv.FormatInt(n/1000, 10) + "k"
	}
	return strconv.FormatInt(n, 10)
}
