package reading

import (
	"strconv"

	"github.com/dphaener/physa/internal/store"
)

// Each cluster number below is a first guess, to be tuned from real runs
// (measure-before-budget).
const (
	// ClusterMaxFiles is the most files a change may touch and still count
	// for clusters; a larger change says little about what belongs together.
	ClusterMaxFiles = 20
	// ClusterMinChanges is the fewest changes a pair of files must share to
	// be clustered.
	ClusterMinChanges = 3
	// ClusterPaths is the most paths listed for one cluster.
	ClusterPaths = 8
)

// BytesPerTokenTenths is the bytes per token every estimate uses, in tenths:
// 2.3. It was measured on #25's real propose prompts: 178,659 bytes read as
// 66,069 input tokens on Physa (2.70 a token) and 321,819 as 135,135 on
// nebari (2.38); the lower, rounded down, so estimates err high.
const BytesPerTokenTenths = 23

// BytesTokens estimates the tokens in n bytes, rounding up.
func BytesTokens(n int) int { return (n*10 + BytesPerTokenTenths - 1) / BytesPerTokenTenths }

// Tokens estimates the tokens in s; see BytesTokens.
func Tokens(s string) int { return BytesTokens(len(s)) }

// Figure is the bytes per token as people read it: "2.3".
func Figure() string {
	return strconv.Itoa(BytesPerTokenTenths/10) + "." + strconv.Itoa(BytesPerTokenTenths%10)
}

// Thousands writes n with commas: 14300 is "14,300".
func Thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := ""
	if n < 0 {
		neg, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return neg + s
}

// GatherTokens estimates the tokens of everything g holds to read: its
// docs, its changes' messages and files, and its PRs' titles, bodies,
// linked issues and comments. It is the gather's estimate, and what
// recovery's ratio is measured against.
func GatherTokens(g store.Gather) int {
	n := 0
	for _, d := range g.Docs {
		n += len(d.Body)
	}
	for _, c := range g.Changes {
		n += len(c.Message)
		for _, p := range c.Files {
			n += len(p)
		}
	}
	for _, p := range g.PRs {
		n += len(p.Title) + len(p.Body)
		for _, is := range p.Issues {
			n += len(is)
		}
		for _, c := range p.Comments {
			n += len(c.Body)
		}
	}
	return BytesTokens(n)
}
