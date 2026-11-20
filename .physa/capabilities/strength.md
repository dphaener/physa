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

### Strength is always on
`strength-always-on` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/05-earned-release-and-strength.md
Strength is only the number, worked out from evidence that is always recorded, so it is always on. Views show it as experimental until calibration has data. Earned release feeds it and needs it.
Because: once strength means only the number, switching it off switched off nothing but a reading of what already happens.
Rejected:
- Strength opt-in while experimental (decided 2026-11-20, in their words: "it should be opt-in to begin with"): it made turning off strength sound like turning off flags.

### Strength is decaying flow
`decaying-flow` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/05-earned-release-and-strength.md
Each piece of evidence is a contribution; strength is their sum, each halving by age (14 days for ideas, 90 for capabilities), kept as a running value with a recent breakdown.
Because: it is the rule Physarum's tubes follow, and it is exact without replaying history.

### Weights and decay are settings, built to be tuned
`tunable-strength` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/05-earned-release-and-strength.md
Weights, decay and thresholds are versioned advanced settings, never code; evidence never holds a weight.
Because: they "will definitely need to be tuned, and potentially customized by each team" (the owner).

## Questions

### How is strength computed?
`strength-formula` · raised by dphaener · 2026-11-19
A first model is in docs/architecture/05-earned-release-and-strength.md. Whether its weights and decay are right can only be learned from real use.
