---
promise: Any change, idea or decision can be undone as a unit, cheapest way first.
stewards: [dphaener]
---

# Rollback

## Rules

### Cheapest first
`cheapest-first` · from docs/vision.md
Turn the flag off; then revert the anchored changes; then reverse the decision and list what relied on it.
Because: most problems are fixed instantly by a flag, with no deploy.

### Every rollback is evidence
`rollback-evidence` · from docs/vision.md
A rollback is recorded with who and why, and weakens the strength of what it undid.
Because: what had to be undone should carry less weight next time.

### Show dependents before reverting
`show-dependents` · from docs/vision.md
Before reverting, Physa shows the later changes that touch what is being undone.
Because: a revert that breaks what came after is a second mistake.

## Decisions

### Rollback designed first
`rollback-first` · decided by dphaener · 2026-11-19 · from docs/research/rhiza-v1-lessons.md
Rollback is part of the design from the start, through change anchors and flags.
Because: in Rhiza, how to undo an autonomous run's work stayed an open question for 24 days.
