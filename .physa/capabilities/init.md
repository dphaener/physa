---
promise: A team with an existing codebase gets a cited, reviewable map in minutes, without writing anything first.
stewards: [dphaener]
---

# Init

## Rules

### Uncertain knowledge becomes a question
`question-not-guess` · from docs/architecture/02-init-and-learning.md
When sources suggest a rule but don't establish it, init writes a question, never a rule. There are no confidence scores in the map.
Because: one confident wrong rule costs more trust than ten honest questions.

### The first map arrives as a PR
`init-as-pr` · from docs/architecture/02-init-and-learning.md
Init commits to `physa/init` and opens a PR listing what it added, with sources, questions raised, entries dropped and tokens spent.
Because: the person reviews the map like any change before it is trusted.

### Init has an appetite
`init-appetite` · from docs/architecture/02-init-and-learning.md
Init shows its token budget before it starts and stops at it, keeping what it finished.
Because: it spends the customer's tokens.

### A known footprint
`init-footprint` · from docs/architecture/02-init-and-learning.md
Init adds only the map and two or three marked lines in AGENTS.md.
Because: teams with existing agent docs must not find Physa rewriting them.

## Decisions

### GitHub first
`github-first` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/02-init-and-learning.md
Init and the PR check read PRs and reviews through `gh`. Other hosts come later.
Because: one host done well before several done badly.

## Questions

### What does the person see while init runs?
`init-progress` · raised by dphaener · 2026-11-19
Init can take minutes on a large codebase. Progress, and how it is shown, are not yet designed.
