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
When a slice is over its budget, product.md's decisions and questions go first, then rejected options, terms, questions, reasons and decisions. Product rules, promises and rules stay.
Because: they are what an agent most needs; if they alone don't fit, the map is too big.

### Fast from day one
`fifty-ms` · from docs/architecture/01-map-and-slice.md
`physa map index` and `physa map slice` answer in under 50 ms on a generated map of 100 capabilities, enforced by a test.
Because: Rhiza only got its speed target after the pain.

## Decisions

### The index is derived, not stored
`derived-index` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/01-map-and-slice.md
`physa map index` builds the index from the capability files on every call.
Because: storing it would duplicate every promise.

### Pull at the start, check at the PR
`pull-then-check` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/02-init-and-learning.md
Agents pull a slice when they start; the PR check catches what they missed. Nothing is injected mid-session.
Because: hooks are not universal across agents, but every agent's work ends at a PR.
Rejected:
- Hooks that inject context as files are touched: not universal.
- Generated AGENTS.md files as the main delivery: loads everything along a path, whatever the task.

### Measure before setting budgets
`measure-before-budget` · decided by dphaener, in their words · 2026-11-20 · #10
A slice has no default budget and the map has no limit on how many capabilities it holds. Budgets get tuned from run data.
Because: if we collect run data we will know from real runs how to tune these things as the product matures.
Rejected:
- A 3,000-token default slice budget: it cut the capability's own reasons first.
- A limit of about 50 capabilities: it would force merging real promises.
