// Package review holds the reviewing skill Physa ships and the reviewer's
// output. It assembles what the reviewer of one change reads, from the
// change's diff, its PR title and body, the index and the slices for the
// capabilities its trailers name, and it accepts only output that is well
// formed and names what the map holds. Output that is not is refused whole,
// with every problem named: no item is ever dropped or repaired.
package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dphaener/physa/internal/mapfile"
)

// Version is the version of the reviewer's output this physa reads and
// writes.
const Version = 1

// The values of RuleAtStake.Respected.
const (
	RespectedYes     = "yes"
	RespectedNo      = "no"
	RespectedUnclear = "unclear"
)

// The values of Edit.Section: the section of a map file an entry goes in.
const (
	SectionRules     = "rules"
	SectionDecisions = "decisions"
	SectionQuestions = "questions"
)

// Product is the capability name the reviewer uses for product.md. It is
// valid wherever a capability is named except in Touched.
const Product = "product"

// Review is the reviewer's judgment of one change. Field order is the JSON
// field order.
type Review struct {
	Version        int             `json:"version"`
	Touched        []string        `json:"touched"` // capability names, never product
	Rules          []RuleAtStake   `json:"rules"`
	Contradictions []Contradiction `json:"contradictions"`
	Edits          []Edit          `json:"edits"`
}

// RuleAtStake is a rule the change could keep or break, and whether it
// looks respected.
type RuleAtStake struct {
	Capability string `json:"capability"` // a capability name or Product
	Rule       string `json:"rule"`       // the rule's slug
	Respected  string `json:"respected"`  // RespectedYes, RespectedNo or RespectedUnclear
	Why        string `json:"why"`
}

// Contradiction is an existing decision the change goes against.
type Contradiction struct {
	Capability string `json:"capability"` // a capability name or Product
	Decision   string `json:"decision"`   // the decision's slug
	Why        string `json:"why"`
}

// Edit is a map entry the change itself makes true, written in full as it
// would sit in the capability's file.
type Edit struct {
	Capability string `json:"capability"` // a capability name or Product
	Section    string `json:"section"`    // SectionRules, SectionDecisions or SectionQuestions
	Entry      string `json:"entry"`
	Why        string `json:"why"`
}

// MarshalJSON writes r with every empty list as [], never null.
func (r Review) MarshalJSON() ([]byte, error) {
	type plain Review
	p := plain(r)
	if p.Touched == nil {
		p.Touched = []string{}
	}
	if p.Rules == nil {
		p.Rules = []RuleAtStake{}
	}
	if p.Contradictions == nil {
		p.Contradictions = []Contradiction{}
	}
	if p.Edits == nil {
		p.Edits = []Edit{}
	}
	return json.Marshal(p)
}

// InvalidError is output the reviewer returned that Physa refuses. It names
// every problem found, in the order of the output.
type InvalidError struct {
	Problems []string
}

func (e *InvalidError) Error() string {
	return "the reviewer's output is invalid: " + strings.Join(e.Problems, "; ")
}

// wire is the output as decoded, so that a missing or null key is told
// apart from an empty list.
type wire struct {
	Version        *int             `json:"version"`
	Touched        *[]string        `json:"touched"`
	Rules          *[]RuleAtStake   `json:"rules"`
	Contradictions *[]Contradiction `json:"contradictions"`
	Edits          *[]Edit          `json:"edits"`
}

// Parse reads the reviewer's answer: the last fenced json block in text, or
// the whole text when it is a bare JSON object. It decodes the block,
// refusing unknown keys, and checks it against m: the version, that every
// capability, rule and decision named is one m holds, and that every value
// is in its set and every text is present. Anything wrong is an
// *InvalidError naming every problem.
func Parse(text string, m *mapfile.Map) (Review, error) {
	if m == nil {
		return Review{}, errors.New("no map to check the reviewer's output against")
	}
	block, ok := lastJSONBlock(text)
	if !ok {
		return Review{}, &InvalidError{Problems: []string{"the answer has no fenced json block"}}
	}
	var w wire
	dec := json.NewDecoder(strings.NewReader(block))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return Review{}, &InvalidError{Problems: []string{"the json block does not decode: " + err.Error()}}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Review{}, &InvalidError{Problems: []string{"the json block holds more than one value"}}
	}
	var r Review
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if w.Version == nil {
		add("version is missing")
	} else if r.Version = *w.Version; r.Version != Version {
		add("version is %d; this physa reads version %d", r.Version, Version)
	}
	for _, k := range []struct {
		name    string
		missing bool
	}{{"touched", w.Touched == nil}, {"rules", w.Rules == nil}, {"contradictions", w.Contradictions == nil}, {"edits", w.Edits == nil}} {
		if k.missing {
			add("%s is missing: write [] for an empty list", k.name)
		}
	}
	if w.Touched != nil {
		r.Touched = *w.Touched
	}
	if w.Rules != nil {
		r.Rules = *w.Rules
	}
	if w.Contradictions != nil {
		r.Contradictions = *w.Contradictions
	}
	if w.Edits != nil {
		r.Edits = *w.Edits
	}
	v := validator{m: m, add: add}
	v.touched(r.Touched)
	for i, rule := range r.Rules {
		at := fmt.Sprintf("rules[%d]", i)
		v.entry(at, "rule", rule.Capability, rule.Rule, mapfile.Rule)
		v.oneOf(at+".respected", rule.Respected, RespectedYes, RespectedNo, RespectedUnclear)
		v.text(at+".why", rule.Why)
	}
	for i, c := range r.Contradictions {
		at := fmt.Sprintf("contradictions[%d]", i)
		v.entry(at, "decision", c.Capability, c.Decision, mapfile.Decision)
		v.text(at+".why", c.Why)
	}
	for i, e := range r.Edits {
		at := fmt.Sprintf("edits[%d]", i)
		v.file(at+".capability", e.Capability)
		v.oneOf(at+".section", e.Section, SectionRules, SectionDecisions, SectionQuestions)
		v.text(at+".entry", e.Entry)
		v.text(at+".why", e.Why)
	}
	if len(problems) > 0 {
		return Review{}, &InvalidError{Problems: problems}
	}
	return r, nil
}

type validator struct {
	m   *mapfile.Map
	add func(format string, args ...any)
}

func (v validator) touched(names []string) {
	seen := map[string]bool{}
	for i, n := range names {
		at := fmt.Sprintf("touched[%d]", i)
		switch {
		case n == Product:
			v.add("%s: product.md is not a capability; name capabilities only", at)
		case v.capability(n) == nil:
			v.add("%s: %q is not a capability in the map", at, n)
		case seen[n]:
			v.add("%s: %q is listed more than once", at, n)
		}
		seen[n] = true
	}
}

// file reports a problem when name is neither Product nor a capability in
// the map, and returns the file it names.
func (v validator) file(at, name string) *mapfile.File {
	if name == Product {
		if v.m.Product == nil {
			v.add("%s: the map has no product.md", at)
		}
		return v.m.Product
	}
	f := v.capability(name)
	if f == nil {
		v.add("%s: %q is not a capability in the map", at, name)
	}
	return f
}

// entry checks that slug is an entry of kind k in the file capability
// names.
func (v validator) entry(at, key, capability, slug string, k mapfile.Kind) {
	f := v.file(at+".capability", capability)
	if f == nil {
		return
	}
	for _, e := range f.Entries {
		if e.Slug != slug {
			continue
		}
		if e.Kind != k {
			v.add("%s.%s: %q is a %s in %s, not a %s", at, key, slug, e.Kind, capability, k)
		}
		return
	}
	v.add("%s.%s: %q is not a %s in %s", at, key, slug, k, capability)
}

func (v validator) oneOf(at, value string, allowed ...string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	v.add("%s: %q is not one of %s", at, value, strings.Join(allowed, ", "))
}

func (v validator) text(at, s string) {
	if strings.TrimSpace(s) == "" {
		v.add("%s is empty", at)
	}
}

func (v validator) capability(name string) *mapfile.File {
	for _, c := range v.m.Capabilities {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// lastJSONBlock returns the content of the last closed fence opened by a
// line of three or more backticks followed by json, or text itself when,
// trimmed, it is a bare JSON object.
func lastJSONBlock(text string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var (
		found  string
		ok     bool
		open   string // the opening fence's backticks; empty outside a fence
		isJSON bool
		start  int
	)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if open == "" {
			ticks := len(t) - len(strings.TrimLeft(t, "`"))
			if ticks >= 3 {
				open, start = t[:ticks], i+1
				isJSON = strings.EqualFold(strings.TrimSpace(t[ticks:]), "json")
			}
			continue
		}
		if strings.HasPrefix(t, open) && strings.Trim(t, "`") == "" {
			if isJSON {
				found, ok = strings.Join(lines[start:i], "\n"), true
			}
			open = ""
		}
	}
	if ok {
		return found, true
	}
	if t := strings.TrimSpace(text); strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}") {
		return t, true
	}
	return "", false
}
