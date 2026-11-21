package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/adapter/claude"
)

// agents are the agents Physa can draft with, by name; each is built on demand.
var agents = map[string]func() adapter.Adapter{
	"claude": func() adapter.Adapter { return claude.New() },
}

// defaultAgent is the agent used when neither --agent nor PHYSA_AGENT names one.
const defaultAgent = "claude"

// agentFor returns the agent called name; an empty name falls back to
// PHYSA_AGENT, then to claude. Physa holds no credentials: the agent reads
// whatever it needs from the environment it inherits.
func agentFor(name string) (adapter.Adapter, error) {
	if name == "" {
		name = os.Getenv("PHYSA_AGENT")
	}
	if name == "" {
		name = defaultAgent
	}
	mk, ok := agents[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(slices.Sorted(maps.Keys(agents)), ", "))
	}
	return mk(), nil
}
