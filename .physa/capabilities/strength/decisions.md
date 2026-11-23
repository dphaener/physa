# Strength · Decisions

## Contents

- [Strength is always on](#strength-is-always-on)
- [Strength is decaying flow](#strength-is-decaying-flow)
- [Weights and decay are settings, built to be tuned](#weights-and-decay-are-settings-built-to-be-tuned)

## Entries

### Strength is always on
`strength-always-on` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/archive/architecture/05-earned-release-and-strength.md
Strength is only the number, worked out from evidence that is always recorded, so it is always on. Views show it as experimental until calibration has data. Earned release feeds it and needs it.
Because: once strength means only the number, switching it off switched off nothing but a reading of what already happens.
Rejected:
- Strength opt-in while experimental (decided 2026-11-20, in their words: "it should be opt-in to begin with"): it made turning off strength sound like turning off flags.

### Strength is decaying flow
`decaying-flow` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/archive/architecture/05-earned-release-and-strength.md
Each piece of evidence is a contribution; strength is their sum, each halving by age (14 days for ideas, 90 for capabilities), kept as a running value with a recent breakdown.
Because: it is the rule Physarum's tubes follow, and it is exact without replaying history.

### Weights and decay are settings, built to be tuned
`tunable-strength` · decided by dphaener, in their words · 2026-11-20 · from docs/archive/architecture/05-earned-release-and-strength.md
Weights, decay and thresholds are versioned advanced settings, never code; evidence never holds a weight.
Because: they "will definitely need to be tuned, and potentially customized by each team" (the owner).
