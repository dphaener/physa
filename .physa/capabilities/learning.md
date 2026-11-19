---
promise: The map gets better with every PR, without anyone curating it.
stewards: [dphaener]
---

# Learning

## Rules

### Learn at the PR
`learn-at-pr` · from docs/architecture/02-init-and-learning.md
The map learns where every agent's work ends up: the PR. While it is open, the change is checked against the map; after it merges, its review is learned from.
Because: it is the one place every agent's work passes through, whichever agent wrote it.

### Non-blocking unless the team says otherwise
`non-blocking` · from docs/architecture/02-init-and-learning.md
The PR check reports a neutral status by default. Teams make specific findings blocking by policy.
Because: a new tool that blocks merges gets removed on day two.

### The map update PR never merges itself
`no-self-merge` · from docs/architecture/02-init-and-learning.md
Everything learned after merge is batched into one map update PR that stewards review.
Because: nothing changes silently.

## Decisions

### What was loaded travels in a trailer
`loaded-trailer` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/02-init-and-learning.md
Agents add `Loaded: <capabilities>` to their commits, so CI can measure slice misses and excess with no store.
Because: CI cannot see a developer's local store.

### GitHub keeps results in the open-source core
`results-in-checks` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/02-init-and-learning.md
Each PR check writes its structured results into the check run; `physa sync` pulls them into the local store.
Because: results feed strength and must not live in the repo; the open core must work without the hosted layer.
