---
promise: Every idea is measured against the signal it claimed, and nothing else.
stewards: [dphaener]
---

# Signals

## Rules

### Measure only what ideas claim
`claims-only` · from docs/vision.md
Physa records only the events a signal names. It never becomes general analytics.
Because: owning a narrow measurement is a product; owning analytics is a second company.

### Judgment is named
`named-judgment` · from docs/vision.md
A person's assessment counts as evidence, with their name, weighted below measurement and always shown as judgment.
Because: design and low-traffic ideas could never gain strength otherwise.

### Say when it can't tell
`cant-tell` · from docs/vision.md
With too little flow to decide, Physa says "not enough flow to tell yet" and never shows noise as a result.
Because: small products rarely get enough traffic for statistics.

### Signal events decay with their idea
`events-decay` · from docs/vision.md
When an idea is established or withers, its events are proposed for removal.
Because: tracking plans rot like flags do.

## Decisions

### Engineering evidence from git and CI; runtime through OpenTelemetry
`engineering-evidence` · decided by dphaener, in their words · 2026-11-19 · from docs/vision.md
Reverts, rework, defects and lead time come from git, PRs and CI. Errors and performance come through OpenTelemetry.
Because: architecture ideas need evidence too, and monitoring is not ours to build.

### One SDK for flags and signal events
`one-sdk` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/vision.md
The same SDK evaluates flags and records signal events.
Because: exposure and outcome must join on the same person.
Rejected:
- Building product analytics.
- Integrating only with existing analytics: leaves solo developers with nothing.
