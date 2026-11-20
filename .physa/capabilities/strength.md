---
promise: Every idea and capability shows how much real flow it carries, and what stops carrying flow fades.
stewards: [dphaener]
---

# Strength

## Rules

### Measured, never set by hand
`measured-strength` · from docs/vision.md
Strength comes from evidence: attention before code, then usage, rollout, signal results, stability and citations. Nobody types it in.
Because: a label someone sets goes stale; that was Rhiza's failure.

### Decay proposes, never deletes
`decay-proposes` · from docs/vision.md
Decay lowers strength and, past a point, proposes removal on the map update PR. A person accepts it.
Because: nothing changes silently.

### Capabilities fade too
`capabilities-fade` · from docs/vision.md
A capability whose use declines becomes a candidate for removal.
Because: products should shed what nobody uses.

## Decisions

### Strength starts opt-in
`strength-opt-in` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/05-flags-signals-strength.md
Computing strength is opt-in while it is experimental. Flags, signals, results and rollback do not depend on it.
Because: "one of the core differentiators, and also a highly experimental feature … it should be opt-in to begin with."

### Strength is decaying flow
`decaying-flow` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/05-flags-signals-strength.md
Each piece of evidence is a contribution; strength is their sum, each halving by age (14 days for ideas, 90 for capabilities), kept as a running value with a recent breakdown.
Because: it is the rule Physarum's tubes follow, and it is exact without replaying history.

### Weights and decay are settings, built to be tuned
`tunable-strength` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/05-flags-signals-strength.md
Weights, decay and thresholds are versioned advanced settings, never code; evidence never holds a weight.
Because: they "will definitely need to be tuned, and potentially customized by each team" (the owner).

## Questions

### How is strength computed?
`strength-formula` · raised by dphaener · 2026-11-19
A first model is in docs/architecture/05-flags-signals-strength.md. Whether its weights and decay are right can only be learned from real use.
