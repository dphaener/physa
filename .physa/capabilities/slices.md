---
promise: An agent gets exactly the product knowledge its task needs, in one call, before it starts.
stewards: [dphaener]
---

# Slices

## Rules

### Agents choose, the CLI assembles
`agent-chooses` · from docs/architecture/01-map-and-slice.md
The agent reads the index and names the capabilities it needs. The CLI assembles them and never matches or ranks.
Because: the agent knows its task; matching text to capabilities is flaky, and embeddings are heavy.

### Same request, same slice
`deterministic-slice` · from docs/architecture/01-map-and-slice.md
Everything in a slice except its current-state section depends only on the map and the request.
Because: agents and people must be able to trust and reproduce what was loaded.

### Product rules and promises are never cut
`never-cut` · from docs/architecture/01-map-and-slice.md
When a slice is over budget, rejected options go first, then questions, reasons, decisions and current state. Product rules, promises and rules stay.
Because: they are what an agent most needs; if they alone don't fit, the map is too big.

### Fast from day one
`fifty-ms` · from docs/architecture/01-map-and-slice.md
`physa index` and `physa slice` answer in under 50 ms on a map twice the default budget, enforced by a test.
Because: Rhiza only got its speed target after the pain.

## Decisions

### The index is derived, not stored
`derived-index` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/01-map-and-slice.md
`physa index` builds the index from the capability files on every call.
Because: storing it would duplicate every promise.

### Pull at the start, check at the PR
`pull-then-check` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/02-init-and-learning.md
Agents pull a slice when they start; the PR check catches what they missed. Nothing is injected mid-session.
Because: hooks are not universal across agents, but every agent's work ends at a PR.
Rejected:
- Hooks that inject context as files are touched: not universal.
- Generated AGENTS.md files as the main delivery: loads everything along a path, whatever the task.
