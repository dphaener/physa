---
promise: Changes land and reach users at a bar set by what is at stake, with the evidence for each one in plain sight.
stewards: [dphaener]
---

# Confidence

## Rules

### Stakes are worked out, never guessed
`computed-stakes` · from docs/vision.md
Stakes are the strength of what a change touches, times its exposure, reduced by how easily it is undone. The engine computes them.
Because: a guessed risk score is an estimate by another name.

### Landing and exposing are separate bars
`two-bars` · from docs/vision.md
With earned release on, landing code behind an off flag and exposing users each have their own stakes and their own bar. With it off, landing is exposing: one bar, whose stakes include everyone the change reaches.
Because: most of the speed comes from landing freely and exposing carefully, and without a flag there is nothing in between.

### The engine re-runs the checks
`rerun-checks` · from docs/research/harness-spec.md
A change's case uses checks the engine ran itself, never an agent's report of them.
Because: gate verdicts come from records, never from what an agent says.

### Guardrails roll back on their own
`guardrail-rollback` · from docs/vision.md
If a guardrail degrades while an idea earns its release, the engine turns the flag off and records why.
Because: it carries out a rule the team set in advance; it does not judge.

## Decisions

### Fewer, meaningful sign-offs
`meaningful-signoffs` · decided by dphaener, choosing an option · 2026-11-19 · from docs/vision.md
People sign off only when the stakes require it. Physa never tracks how people review.
Because: in Rhiza the recommended option was taken 88% of the time; frequent sign-offs become rubber stamps.
Rejected:
- Recording whether a person looked at the case: intrusive to developers.
