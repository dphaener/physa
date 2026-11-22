# Slices · Decisions

## Contents

- [The index is derived, not stored](#the-index-is-derived-not-stored)
- [Pull at the start, check at the PR](#pull-at-the-start-check-at-the-pr)
- [Measure before setting budgets](#measure-before-setting-budgets)

## Entries

### The index is derived, not stored
`derived-index` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/01-map-and-slice.md
`physa map index` builds the index from the capability folders on every call.
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
A slice has no token budget (`--budget` is gone: the slice gives the index, the rules and the slugs, and `--entry` takes the rest) and the map has no limit on how many capabilities it holds. Sizes get tuned from run data.
Because: if we collect run data we will know from real runs how to tune these things as the product matures.
Rejected:
- A 3,000-token default slice budget: it cut the capability's own reasons first.
- A limit of about 50 capabilities: it would force merging real promises.
