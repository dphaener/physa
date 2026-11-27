// Package consolidateprompt holds the skills of init's consolidation, one for
// each step a session judges (merging capabilities that are one promise,
// consolidating one area's entries, lifting rules the whole product keeps),
// writes each session's message with every entry fenced as data, and reads
// each reply. A reply only names capabilities and entries by id: an item in
// another shape, or naming one its session was not given, is returned as a
// fault with why, never read.
package consolidateprompt

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/capabilitytext"
	"github.com/dphaener/physa/internal/consolidating"
	"github.com/dphaener/physa/internal/draft"
	"github.com/dphaener/physa/internal/jsonblock"
	"github.com/dphaener/physa/internal/recovering"
	"github.com/dphaener/physa/internal/repeats"
)

// Merge is the skill of step 1: which candidate groups of capabilities from
// different areas are one promise.
//
//go:embed merge.md
var Merge string

// Consolidate is the skill of step 3: one area's near-duplicate entries
// merged, rules moved and answered questions dropped.
//
//go:embed consolidate.md
var Consolidate string

// Lift is the skill of step 4: which groups of rules repeated across areas
// are one rule the product keeps.
//
//go:embed lift.md
var Lift string

// AnswerNow is the turn a session gets when its reply cannot be read, with
// why.
const AnswerNow = "Your answer could not be read: %v. Answer now, as the skill says: end with exactly one fenced block marked `json` and nothing after it."

// entry writes e of capability c as a session is shown it: its id, its kind
// and, with where, its capability and area, then its exact text, fenced.
func entry(w *bytes.Buffer, c capabilitytext.Capability, e capabilitytext.Entry, where bool) {
	fmt.Fprintf(w, "\nEntry `%s` (%s", consolidating.ID(c.Name, e.Slug), e.Kind)
	if where {
		fmt.Fprintf(w, ", capability `%s`, area `%s`", draft.OneLine(c.Name), draft.OneLine(c.Area.String()))
	}
	w.WriteString("):\n\n")
	draft.Fence(w, e.Text())
}

// MergeMessage is step 1's message: the capabilities, every one of caps
// when every is true, else only those of groups, by name, area and promise,
// then each candidate group with what joined it.
func MergeMessage(caps []capabilitytext.Capability, groups []repeats.CapabilityGroup, every bool) string {
	in := map[string]bool{}
	for _, g := range groups {
		for _, m := range g.Capabilities {
			in[m.Name] = true
		}
	}
	var w bytes.Buffer
	w.WriteString("## The capabilities\n\n")
	var rows []string
	for _, c := range caps {
		if every || in[c.Name] {
			rows = append(rows, fmt.Sprintf("%s (area %s): %s", draft.OneLine(c.Name), draft.OneLine(c.Area.String()), draft.OneLine(c.Promise())))
		}
	}
	draft.Fence(&w, strings.Join(rows, "\n"))
	w.WriteString("\n## The candidate groups\n")
	for i, g := range groups {
		fmt.Fprintf(&w, "\n### Group %d\n\n", i+1)
		draft.Fence(&w, "Capabilities: "+Names(g)+"\nWhat joined them:\n- "+strings.Join(g.Evidence, "\n- "))
	}
	w.WriteString("\nDecide which capabilities of each group are one promise, as the skill says.\n")
	return w.String()
}

// Names is g's capabilities, comma-separated.
func Names(g repeats.CapabilityGroup) string {
	var n []string
	for _, m := range g.Capabilities {
		n = append(n, m.Name)
	}
	return strings.Join(n, ", ")
}

// AreaMessage is step 3's message for caps, capabilities of one area: each
// with its promise and its entries.
func AreaMessage(caps []capabilitytext.Capability) string {
	var w bytes.Buffer
	n := 0
	for _, c := range caps {
		n += len(c.Entries())
	}
	fmt.Fprintf(&w, "## The area `%s`\n\nIt has %d capabilities and %d entries given here.\n", draft.OneLine(caps[0].Area.String()), len(caps), n)
	for _, c := range caps {
		fmt.Fprintf(&w, "\n### Capability `%s`\n\nPromise:\n\n", draft.OneLine(c.Name))
		draft.Fence(&w, draft.OneLine(c.Promise()))
		for _, e := range c.Entries() {
			entry(&w, c, e, false)
		}
	}
	w.WriteString("\nConsolidate this area's entries, as the skill says.\n")
	return w.String()
}

// LiftMessage is step 4's message: the product's promise, said not to be
// stated yet when stated is false, then each group's rules, found in caps,
// with their capability and area.
func LiftMessage(promise string, stated bool, caps []capabilitytext.Capability, groups []repeats.RuleGroup) string {
	var w bytes.Buffer
	w.WriteString("## The product's promise\n\n")
	if !stated {
		w.WriteString("Not yet stated: the product's promise has not been written, so judge each group by its rules alone.\n")
	} else {
		draft.Fence(&w, draft.OneLine(promise))
	}
	w.WriteString("\n## Groups of rules from different areas\n")
	for i, g := range groups {
		fmt.Fprintf(&w, "\n### Group %d\n", i+1)
		for _, r := range g.Rules {
			name, slug, _ := strings.Cut(r.ID, "/")
			if at := slices.IndexFunc(caps, func(c capabilitytext.Capability) bool { return c.Name == name }); at >= 0 {
				if e := caps[at].Entry(slug); e != nil {
					entry(&w, caps[at], *e, true)
				}
			}
		}
	}
	w.WriteString("\nDecide which to lift, as the skill says.\n")
	return w.String()
}

// Shown is the ids of groups' rules a lift session is shown, and each
// group's.
func Shown(groups []repeats.RuleGroup) (shown []string, each [][]string) {
	for _, g := range groups {
		var ids []string
		for _, r := range g.Rules {
			ids = append(ids, r.ID)
		}
		shown, each = append(shown, ids...), append(each, ids)
	}
	return shown, each
}

// Check says why m is refused against the candidate groups its session was
// given: fewer than two capabilities, or not all in one group; empty when
// it is not.
func (m MergeItem) Check(groups []repeats.CapabilityGroup) string {
	if len(m.Capabilities) < 2 {
		return "a merge needs two capabilities or more"
	}
	for _, g := range groups {
		if !slices.ContainsFunc(m.Capabilities, func(n string) bool {
			return !slices.ContainsFunc(g.Capabilities, func(x repeats.Capability) bool { return x.Name == n })
		}) {
			return ""
		}
	}
	return "its capabilities are not all in one candidate group code found and the session was given"
}

// Choices is r as the choices code applies to caps, the capabilities its
// session was given: every item naming an id or a capability not among them
// is a fault, never applied.
func (r AreaReply) Choices(caps []capabilitytext.Capability) (consolidating.AreaChoices, []Fault) {
	var ch consolidating.AreaChoices
	var faults []Fault
	ids, names := map[string]bool{}, map[string]bool{}
	for _, c := range caps {
		names[c.Name] = true
		for _, e := range c.Entries() {
			ids[consolidating.ID(c.Name, e.Slug)] = true
		}
	}
	shown := func(item string, entries []string, capability ...string) bool {
		for _, id := range entries {
			if !ids[id] {
				faults = append(faults, Fault{item, id + " is not among the entries its session was given"})
				return false
			}
		}
		for _, n := range capability {
			if !names[n] {
				faults = append(faults, Fault{item, fmt.Sprintf("%q is not among the capabilities its session was given", n)})
				return false
			}
		}
		return true
	}
	for _, m := range r.Merge {
		if shown("merge "+m.Keep+" with "+strings.Join(m.Drop, ", "), append([]string{m.Keep}, m.Drop...)) {
			ch.Merges = append(ch.Merges, consolidating.MergeChoice{Keep: m.Keep, Drop: m.Drop})
		}
	}
	for _, m := range r.Move {
		if shown("move "+m.ID+" to "+m.To, []string{m.ID}, m.To) {
			ch.Moves = append(ch.Moves, consolidating.MoveChoice{ID: m.ID, To: m.To})
		}
	}
	for _, d := range r.Drop {
		if shown("drop "+d.ID+" as answered by "+d.AnsweredBy, []string{d.ID, d.AnsweredBy}) {
			ch.Drops = append(ch.Drops, consolidating.DropChoice{ID: d.ID, AnsweredBy: d.AnsweredBy})
		}
	}
	return ch, faults
}

// Fault is an item of a reply that is not in its wanted shape, or a key the
// skill does not ask for: it is never read.
type Fault struct {
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

// MergeItem is one merge step 1's session chose.
type MergeItem struct {
	Capabilities []string `json:"capabilities"`
	Name         string   `json:"name"`
	PromiseOf    string   `json:"promise_of"`
	Why          string   `json:"why"`
}

// MergeReply is step 1's reply as read.
type MergeReply struct {
	Merge  []MergeItem
	Faults []Fault
}

// AreaMerge keeps one entry and drops others into it.
type AreaMerge struct {
	Keep string   `json:"keep"`
	Drop []string `json:"drop"`
	Why  string   `json:"why"`
}

// AreaMove moves a rule to another capability of its area.
type AreaMove struct {
	ID  string `json:"id"`
	To  string `json:"to"`
	Why string `json:"why"`
}

// AreaDrop drops a question another entry answers.
type AreaDrop struct {
	ID         string `json:"id"`
	AnsweredBy string `json:"answered_by"`
	Why        string `json:"why"`
}

// AreaReply is step 3's reply as read.
type AreaReply struct {
	Merge  []AreaMerge
	Move   []AreaMove
	Drop   []AreaDrop
	Faults []Fault
}

// LiftItem is one lift step 4's session chose.
type LiftItem struct {
	Keep string   `json:"keep"`
	With []string `json:"with"`
	Why  string   `json:"why"`
}

// LiftReply is step 4's reply as read.
type LiftReply struct {
	Lift   []LiftItem
	Faults []Fault
}

// The wanted shapes, named in each fault.
const (
	mergeShape     = `{"capabilities":["<name>","<name>"],"name":"<one of them>","promise_of":"<one of them>","why":"<one line>"}`
	areaMergeShape = `{"keep":"<entry id>","drop":["<entry id>"],"why":"<one line>"}`
	moveShape      = `{"id":"<rule id>","to":"<capability>","why":"<one line>"}`
	dropShape      = `{"id":"<question id>","answered_by":"<entry id>","why":"<one line>"}`
	liftShape      = `{"keep":"<entry id>","with":["<entry id>"],"why":"<one line>"}`
)

// ParseMerge reads step 1's reply. The error, for a reply with no closed
// json block or one that is not an object, is what the answer-now turn
// names.
func ParseMerge(text string) (MergeReply, error) {
	var r MergeReply
	f, err := read(text, map[string]func(json.RawMessage) string{
		"merge": each(&r.Merge, mergeShape),
	})
	r.Faults = f
	return r, err
}

// ParseArea reads step 3's reply, as ParseMerge does.
func ParseArea(text string) (AreaReply, error) {
	var r AreaReply
	f, err := read(text, map[string]func(json.RawMessage) string{
		"merge": each(&r.Merge, areaMergeShape),
		"move":  each(&r.Move, moveShape),
		"drop":  each(&r.Drop, dropShape),
	})
	r.Faults = f
	return r, err
}

// ParseLift reads step 4's reply, as ParseMerge does.
func ParseLift(text string) (LiftReply, error) {
	var r LiftReply
	f, err := read(text, map[string]func(json.RawMessage) string{
		"lift": each(&r.Lift, liftShape),
	})
	r.Faults = f
	return r, err
}

// read decodes the last json block of text as an object and gives each key
// its raw value. A key no reader takes is a fault, and so is each item a
// reader refuses, named by its key and place.
func read(text string, keys map[string]func(json.RawMessage) string) ([]Fault, error) {
	block, ok := jsonblock.Last(text)
	if !ok {
		return nil, errors.New("it has no closed fenced block marked `json`")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(block), &top); err != nil || top == nil {
		if err == nil {
			err = errors.New("null")
		}
		return nil, fmt.Errorf("its json block is not one object: %v", err)
	}
	var faults []Fault
	names := make([]string, 0, len(top))
	for k := range top {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		r, ok := keys[k]
		if !ok {
			faults = append(faults, Fault{"the key " + k, "it is not a key the skill asks for, so nothing under it is read"})
			continue
		}
		for _, f := range strings.Split(r(top[k]), "\n") {
			if f != "" {
				item, reason, _ := strings.Cut(f, "\t")
				faults = append(faults, Fault{k + " " + item, reason})
			}
		}
	}
	return faults, nil
}

// each reads a list of items into out, each strictly in shape; it returns
// one line per refused item, "item N\treason", or none.
func each[T any](out *[]T, shape string) func(json.RawMessage) string {
	return func(raw json.RawMessage) string {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return ""
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Sprintf("list\tit is not a list of items shaped %s", shape)
		}
		var faults []string
		for i, it := range items {
			var v T
			d := json.NewDecoder(bytes.NewReader(it))
			d.DisallowUnknownFields()
			if err := d.Decode(&v); err != nil {
				faults = append(faults, fmt.Sprintf("item %d\tit is not shaped %s (%v), so nothing it names is read", i+1, shape, err))
				continue
			}
			*out = append(*out, v)
		}
		return strings.Join(faults, "\n")
	}
}

// Fit lays items into batches whose messages each fit a session of system
// in window (recovering.Fits): all of them when they fit, else each half
// laid the same way. A single item whose message does not fit is returned
// in tooLarge, never sent.
func Fit[T any](system string, window int, items []T, msg func([]T) string) (batches [][]T, tooLarge []T) {
	switch {
	case len(items) == 0:
		return nil, nil
	case recovering.Fits(system, msg(items), window):
		return [][]T{items}, nil
	case len(items) == 1:
		return nil, items
	}
	b1, t1 := Fit(system, window, items[:len(items)/2], msg)
	b2, t2 := Fit(system, window, items[len(items)/2:], msg)
	return append(b1, b2...), append(t1, t2...)
}
