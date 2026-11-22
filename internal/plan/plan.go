// Package plan reads a bet's plan: the changes a bet will land, each
// anchored to one capability with its acceptance, checked before the engine
// stores it.
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Change is one planned change: what it does, its single capability, the
// acceptance that proves it, what it must not do, and the changes it comes
// after. It holds no file lists: the acceptance is the contract.
type Change struct {
	Name       string   `json:"name"`
	Capability string   `json:"capability"`
	What       string   `json:"what"`
	Acceptance []string `json:"acceptance"`
	MustNot    []string `json:"must_not"`
	After      []string `json:"after"`
}

// Plan is the changes a bet will land.
type Plan struct {
	Changes []Change `json:"changes"`
}

// Names lists the plan's change names in order.
func Names(p Plan) []string {
	names := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		names = append(names, c.Name)
	}
	return names
}

var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxName = 40

// wire is a change as written: the capability stays raw so an array is seen.
type wire struct {
	Name       string          `json:"name"`
	Capability json.RawMessage `json:"capability"`
	What       string          `json:"what"`
	Acceptance []string        `json:"acceptance"`
	MustNot    []string        `json:"must_not"`
	After      []string        `json:"after"`
}

// Parse reads the plan's JSON and checks it against the capabilities in the
// map. Unknown fields are refused. Every problem is collected and returned
// together as one error, each naming its change.
func Parse(data []byte, capabilities []string) (Plan, error) {
	var in struct {
		Changes []wire `json:"changes"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return Plan{}, fmt.Errorf("the plan is not valid: %w", err)
	}
	if dec.More() {
		return Plan{}, errors.New("the plan is not valid: text follows the plan")
	}
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	if len(in.Changes) == 0 {
		add("the plan has no changes")
	}
	if len(capabilities) == 0 && len(in.Changes) > 0 {
		add("the repo has no map, so no capability can be checked")
	}
	p := Plan{}
	seen := map[string]bool{}
	for i, w := range in.Changes {
		label := fmt.Sprintf("change %d", i+1)
		switch {
		case w.Name == "":
			add("%s has no name", label)
		case !slug.MatchString(w.Name):
			label = fmt.Sprintf("change %q", w.Name)
			add("%s: the name %q is not a slug", label, w.Name)
		case len(w.Name) > maxName:
			label = fmt.Sprintf("change %q", w.Name)
			add("%s: the name is longer than %d characters; use at most %d characters", label, maxName, maxName)
		default:
			label = fmt.Sprintf("change %q", w.Name)
			if seen[w.Name] {
				add("%s is repeated", label)
			}
			seen[w.Name] = true
		}
		if strings.TrimSpace(w.What) == "" {
			add("%s says nothing of what it does", label)
		}
		capName, problem := capability(w.Capability)
		if problem == "" && capName != "" && len(capabilities) > 0 && !slices.Contains(capabilities, capName) {
			problem = fmt.Sprintf("names %q, which is not a capability in the map", capName)
		}
		if problem != "" {
			add("%s %s", label, problem)
		}
		if !hasFailureCase(w.Acceptance) {
			add("%s acceptance holds no failure case and no stated reason (start an entry with \"failure case:\" or \"no failure case:\" and say why)", label)
		}
		p.Changes = append(p.Changes, Change{Name: w.Name, Capability: capName, What: w.What,
			Acceptance: w.Acceptance, MustNot: w.MustNot, After: w.After})
	}
	problems = append(problems, afterProblems(p)...)
	if len(problems) > 0 {
		return Plan{}, errors.New("the plan has problems: " + strings.Join(problems, "; "))
	}
	return p, nil
}

// capability decodes a raw capability: one slug, as a string or an array of
// exactly one string. The second result is the problem, empty when none.
func capability(raw json.RawMessage) (string, string) {
	const two = "names two capabilities: it should have been split"
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		switch len(list) {
		case 0:
			return "", "names no capability"
		case 1:
			return single(list[0])
		}
		return "", two
	}
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		if len(raw) == 0 || string(raw) == "null" {
			return "", "names no capability"
		}
		return "", "names a capability that is not text"
	}
	return single(s)
}

func single(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "names no capability"
	}
	if strings.ContainsAny(s, ", /") {
		return "", "names two capabilities: it should have been split"
	}
	return s, ""
}

// hasFailureCase reports whether acceptance states a failure case, or a
// reason there is none, each with text after the colon.
func hasFailureCase(acceptance []string) bool {
	for _, a := range acceptance {
		l := strings.ToLower(strings.TrimSpace(a))
		for _, prefix := range []string{"failure case:", "no failure case:"} {
			if rest, ok := strings.CutPrefix(l, prefix); ok && strings.TrimSpace(rest) != "" {
				return true
			}
		}
	}
	return false
}

// afterProblems checks the after lists: unknown names, self, and cycles.
func afterProblems(p Plan) []string {
	var out []string
	known := map[string]bool{}
	for _, c := range p.Changes {
		known[c.Name] = true
	}
	graph := map[string][]string{}
	for _, c := range p.Changes {
		for _, a := range c.After {
			switch {
			case a == c.Name:
				out = append(out, fmt.Sprintf("change %q comes after itself", c.Name))
			case !known[a]:
				out = append(out, fmt.Sprintf("change %q comes after %q, which is not in the plan", c.Name, a))
			default:
				graph[c.Name] = append(graph[c.Name], a)
			}
		}
	}
	const (
		fresh = iota
		onPath
		done
	)
	state := map[string]int{}
	var path []string
	var visit func(n string)
	visit = func(n string) {
		state[n] = onPath
		path = append(path, n)
		for _, m := range graph[n] {
			switch state[m] {
			case fresh:
				visit(m)
			case onPath:
				from := slices.Index(path, m)
				cycle := append(slices.Clone(path[from:]), m)
				out = append(out, "the changes form a cycle through after: "+strings.Join(cycle, " -> "))
			}
		}
		path = path[:len(path)-1]
		state[n] = done
	}
	for _, c := range p.Changes {
		if state[c.Name] == fresh && c.Name != "" {
			visit(c.Name)
		}
	}
	return out
}
