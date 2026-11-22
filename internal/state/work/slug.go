package work

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Slug limits: what people type, so short, and never a subcommand's name.
const (
	maxSlugLen   = 40
	maxSlugWords = 5
	fallbackSlug = "idea"
)

// reserved are the subcommand names of `physa idea` and `physa bet`. A slug
// made from text that lands on one gets a number; a chosen one is refused.
var reserved = map[string]bool{"add": true, "list": true, "show": true, "shape": true, "stop": true, "plan": true}

var (
	slugPat = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	wordPat = regexp.MustCompile(`[a-z0-9]+`)
)

// MakeSlug makes a slug from an idea's text: its first words in lower-case
// ASCII letters and digits, joined by "-", at most 5 words and 40
// characters. Text with no such word gives "idea". The slug may be reserved
// or taken; the command that stores it makes it unique.
func MakeSlug(text string) string {
	words := wordPat.FindAllString(strings.ToLower(text), maxSlugWords)
	slug := ""
	for _, w := range words {
		next := w
		if slug != "" {
			next = slug + "-" + w
		}
		if len(next) > maxSlugLen {
			if slug == "" {
				slug = w[:maxSlugLen]
			}
			break
		}
		slug = next
	}
	if slug == "" {
		return fallbackSlug
	}
	return slug
}

// ValidSlug says why s cannot be a slug a person chose, or nil when it can:
// lower-case letters and digits in words joined by "-", at most 40
// characters, and not the name of a subcommand. Whether it is taken is the
// command's check.
func ValidSlug(s string) error {
	switch {
	case s == "":
		return refuse(ErrSlugInvalid, "the slug is empty")
	case len(s) > maxSlugLen:
		return refuse(ErrSlugInvalid, "the slug %q is %d characters; at most %d", s, len(s), maxSlugLen)
	case !slugPat.MatchString(s):
		return refuse(ErrSlugInvalid, "the slug %q must be lower-case letters and digits, in words joined by single dashes", s)
	case reserved[s]:
		return refuse(ErrSlugInvalid, "the slug %q is the name of a subcommand; choose another", s)
	}
	return nil
}

// numbered returns base with "-n" on the end, base cut short (and any dash
// left at its end dropped) so the whole stays within 40 characters.
func numbered(base string, n int) string {
	suffix := "-" + strconv.Itoa(n)
	if keep := maxSlugLen - len(suffix); len(base) > keep {
		base = strings.TrimRight(base[:keep], "-")
	}
	return base + suffix
}

// unique returns base, or base numbered from 2 up, the first that is not
// reserved and that taken says is free.
func unique(base string, taken func(string) (bool, error)) (string, error) {
	for n := 1; ; n++ {
		slug := base
		if n > 1 {
			slug = numbered(base, n)
		}
		if reserved[slug] {
			continue
		}
		used, err := taken(slug)
		if err != nil {
			return "", fmt.Errorf("look up the slug %q: %w", slug, err)
		}
		if !used {
			return slug, nil
		}
	}
}
