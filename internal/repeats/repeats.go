// Package repeats finds capabilities and rules that may repeat each other
// across areas, for a session to judge. It only finds candidates: it makes no
// judgment, and it works over plain values, so it reads no session, store or
// record.
package repeats

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// nearPercent is the word overlap, in percent, from which two rules are a near
// repeat.
const nearPercent = 45

// rarest is how many of its rarest words index each rule.
const rarest = 4

var word = regexp.MustCompile(`[a-z0-9]+`)

var stop = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("a an the of to in on for and or is are be by with as at it its this that when from not no can may must " +
		"only their they them than then into each any all one") {
		m[w] = true
	}
	return m
}()

// Capability is a capability's name and the area that holds it.
type Capability struct {
	Name string `json:"name"`
	Area string `json:"area"`
}

// Rename is a name the walk gave a capability because another area held the
// wanted one first: Area wanted From and was given To.
type Rename struct {
	Area string `json:"area"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Rule is one entry of a capability's rules, decisions or questions.
type Rule struct {
	ID         string `json:"id"`
	Capability string `json:"capability"`
	Area       string `json:"area"`
	Kind       string `json:"kind"`
	Heading    string `json:"heading"`
	Body       string `json:"body"`
}

// Input is what the finders read.
type Input struct {
	Capabilities []Capability
	Renamed      []Rename
	Rules        []Rule
}

// SuffixPair is a capability the walk renamed, with the capability of another
// area that kept the original name.
type SuffixPair struct {
	Original Capability `json:"original"`
	Renamed  Capability `json:"renamed"`
	Reason   string     `json:"reason"`
}

// RuleGroup is rules in at least two areas that repeat each other, exactly or
// nearly. Exact is true when every rule has the same Normal form.
type RuleGroup struct {
	Rules []Rule `json:"rules"`
	Exact bool   `json:"exact"`
}

// CapabilityGroup is capabilities in at least two areas that may be one
// promise, with the evidence that joined them.
type CapabilityGroup struct {
	Capabilities []Capability `json:"capabilities"`
	Evidence     []string     `json:"evidence"`
}

// Result is every candidate found.
type Result struct {
	Pairs        []SuffixPair      `json:"pairs"`
	Rules        []RuleGroup       `json:"rules"`
	Capabilities []CapabilityGroup `json:"capabilities"`
}

// Find runs every finder over in. Its output is sorted and does not depend on
// the order of the input.
func Find(in Input) Result {
	pairs := SuffixPairs(in.Capabilities, in.Renamed)
	rules := RuleGroups(in.Rules)
	return Result{Pairs: pairs, Rules: rules, Capabilities: CapabilityGroups(in.Capabilities, pairs, rules)}
}

// Normal is the exact-duplicate form of an entry: lower case, words only, with
// spacing and punctuation set aside. Lines that start with "Because:" are left
// out, since two entries may state one rule for different reasons.
func Normal(heading, body string) string {
	var kept []string
	for _, l := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "Because:") {
			kept = append(kept, l)
		}
	}
	return strings.Join(word.FindAllString(strings.ToLower(heading+" "+strings.Join(kept, " ")), -1), " ")
}

// SuffixPairs pairs each renamed capability with the capability of another
// area that holds the original name. The walk's Renamed list is the source;
// the name pattern (walk, walk-2) only pairs a renamed name with its original.
func SuffixPairs(caps []Capability, renamed []Rename) []SuffixPair {
	byName := map[string]Capability{}
	for _, c := range caps {
		byName[c.Name] = c
	}
	var out []SuffixPair
	seen := map[string]bool{}
	for _, r := range renamed {
		n, ok := strings.CutPrefix(r.To, r.From+"-")
		if v, err := strconv.Atoi(n); !ok || err != nil || v < 2 {
			continue
		}
		orig, o := byName[r.From]
		ren, k := byName[r.To]
		if !o || !k || orig.Area == ren.Area || seen[r.To] {
			continue
		}
		seen[r.To] = true
		out = append(out, SuffixPair{orig, ren, fmt.Sprintf("the walk renamed %q to %q in area %q because area %q held %q first", r.From, r.To, ren.Area, orig.Area, r.From)})
	}
	slices.SortFunc(out, func(a, b SuffixPair) int {
		return cmp.Or(cmp.Compare(a.Original.Name, b.Original.Name), cmp.Compare(a.Renamed.Name, b.Renamed.Name))
	})
	return out
}

type dsu []int

func (d dsu) find(i int) int {
	for d[i] != i {
		d[i] = d[d[i]]
		i = d[i]
	}
	return i
}

func (d dsu) union(a, b int) {
	if a, b = d.find(a), d.find(b); a != b {
		d[max(a, b)] = min(a, b)
	}
}

func words(r Rule) map[string]bool {
	m := map[string]bool{}
	for _, w := range word.FindAllString(strings.ToLower(r.Heading+" "+r.Body), -1) {
		if len(w) > 2 && !stop[w] {
			m[w] = true
		}
	}
	return m
}

// RuleGroups groups rules that repeat each other across areas, exactly (the
// same kind and the same Normal) or nearly (word overlap of at least 45
// percent, each rule indexed by its rarest four words). A group whose rules
// all sit in one area is never returned.
func RuleGroups(rules []Rule) []RuleGroup {
	rs := slices.Clone(rules)
	slices.SortFunc(rs, func(a, b Rule) int { return cmp.Compare(a.ID, b.ID) })
	d := make(dsu, len(rs))
	for i := range d {
		d[i] = i
	}
	exact := map[[2]string]int{}
	for i, r := range rs {
		if n := Normal(r.Heading, r.Body); n != "" {
			k := [2]string{r.Kind, n}
			if j, ok := exact[k]; ok {
				d.union(i, j)
			} else {
				exact[k] = i
			}
		}
	}
	ws := make([]map[string]bool, len(rs))
	df := map[string]int{}
	for i, r := range rs {
		ws[i] = words(r)
		for w := range ws[i] {
			df[w]++
		}
	}
	top := make([][]string, len(rs))
	index := map[string][]int{}
	for i := range rs {
		for w := range ws[i] {
			top[i] = append(top[i], w)
		}
		slices.SortFunc(top[i], func(a, b string) int { return cmp.Or(cmp.Compare(df[a], df[b]), cmp.Compare(a, b)) })
		top[i] = top[i][:min(rarest, len(top[i]))]
		for _, w := range top[i] {
			index[w] = append(index[w], i)
		}
	}
	for i := range rs {
		for _, w := range top[i] {
			for _, j := range index[w] {
				if j <= i || rs[i].Area == rs[j].Area || rs[i].Kind != rs[j].Kind || d.find(i) == d.find(j) {
					continue
				}
				inter := 0
				for x := range ws[i] {
					if ws[j][x] {
						inter++
					}
				}
				if union := len(ws[i]) + len(ws[j]) - inter; inter*100 >= union*nearPercent {
					d.union(i, j)
				}
			}
		}
	}
	members := map[int][]Rule{}
	for i, r := range rs {
		members[d.find(i)] = append(members[d.find(i)], r)
	}
	var out []RuleGroup
	for _, g := range members {
		areas := map[string]bool{}
		same := true
		for _, r := range g {
			areas[r.Area] = true
			same = same && Normal(r.Heading, r.Body) == Normal(g[0].Heading, g[0].Body) && r.Kind == g[0].Kind
		}
		if len(areas) > 1 {
			out = append(out, RuleGroup{g, same})
		}
	}
	slices.SortFunc(out, func(a, b RuleGroup) int { return cmp.Compare(a.Rules[0].ID, b.Rules[0].ID) })
	return out
}

// CapabilityGroups groups capabilities, in different areas, that share a
// suffix pair or a rule repeat. Each group carries its evidence: the rename,
// or the headings of the repeated rules.
func CapabilityGroups(caps []Capability, pairs []SuffixPair, groups []RuleGroup) []CapabilityGroup {
	byName := map[string]Capability{}
	for _, c := range caps {
		byName[c.Name] = c
	}
	for _, g := range groups {
		for _, r := range g.Rules {
			if _, ok := byName[r.Capability]; !ok {
				byName[r.Capability] = Capability{r.Capability, r.Area}
			}
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	slices.Sort(names)
	at := map[string]int{}
	for i, n := range names {
		at[n] = i
	}
	d := make(dsu, len(names))
	for i := range d {
		d[i] = i
	}
	type link struct {
		to       string
		evidence string
	}
	var links []link
	for _, p := range pairs {
		d.union(at[p.Original.Name], at[p.Renamed.Name])
		links = append(links, link{p.Original.Name, p.Reason})
	}
	for _, g := range groups {
		var heads []string
		first := g.Rules[0].Capability
		for _, r := range g.Rules {
			d.union(at[first], at[r.Capability])
			heads = append(heads, fmt.Sprintf("%q (%s)", r.Heading, r.Capability))
		}
		links = append(links, link{first, "rules repeated across areas: " + strings.Join(heads, ", ")})
	}
	members := map[int][]Capability{}
	for i, n := range names {
		members[d.find(i)] = append(members[d.find(i)], byName[n])
	}
	evidence := map[int][]string{}
	for _, l := range links {
		root := d.find(at[l.to])
		evidence[root] = append(evidence[root], l.evidence)
	}
	var out []CapabilityGroup
	for root, g := range members {
		areas := map[string]bool{}
		for _, c := range g {
			areas[c.Area] = true
		}
		if len(g) > 1 && len(areas) > 1 && len(evidence[root]) > 0 {
			ev := evidence[root]
			slices.Sort(ev)
			out = append(out, CapabilityGroup{g, slices.Compact(ev)})
		}
	}
	slices.SortFunc(out, func(a, b CapabilityGroup) int { return cmp.Compare(a.Capabilities[0].Name, b.Capabilities[0].Name) })
	return out
}
