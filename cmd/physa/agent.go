package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
)

// defaultAgent is the agent used when neither --agent nor PHYSA_AGENT names one.
const defaultAgent = "claude"

// agentFor returns the agent of d called name; an empty name falls back to
// PHYSA_AGENT, then to claude. Physa holds no credentials: the agent reads
// whatever it needs from the environment it inherits.
func (d deps) agentFor(name string) (adapter.Adapter, error) {
	if name == "" {
		name = d.proc().Getenv("PHYSA_AGENT")
	}
	if name == "" {
		name = defaultAgent
	}
	mk, ok := d.agents[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(slices.Sorted(maps.Keys(d.agents)), ", "))
	}
	return mk(), nil
}
