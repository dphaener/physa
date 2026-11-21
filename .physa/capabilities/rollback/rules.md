# Rollback · Rules

## Contents

- [Cheapest first](#cheapest-first)
- [Every rollback is evidence](#every-rollback-is-evidence)
- [Show dependents before reverting](#show-dependents-before-reverting)

## Entries

### Cheapest first
`cheapest-first` · from docs/vision.md
Turn the flag off, when earned release is on; then revert the anchored changes; then reverse the decision and list what relied on it.
Because: most problems are fixed instantly by a flag, with no deploy.

### Every rollback is evidence
`rollback-evidence` · from docs/vision.md
A rollback is recorded with who and why, and weakens the strength of what it undid.
Because: what had to be undone should carry less weight next time.

### Show dependents before reverting
`show-dependents` · from docs/vision.md
Before reverting, Physa shows the later changes that touch what is being undone.
Because: a revert that breaks what came after is a second mistake.
