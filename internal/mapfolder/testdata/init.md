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

### Init refuses a mapped repo before spending
`refuse-mapped-repo` · 71bd819
Init refuses a repo that already holds a map before it gathers or starts an agent, and points to learning instead. When no default branch can be found, it looks for a map in the working tree.
Because: a repo with a map grows through learning, and init must not spend the customer's tokens to find that out.

### Installing a hook never disables the repo's own
`keep-existing-hooks` · 0989e23
Installing the Session hook never changes where the repo's hooks are run from, and never silently disables a hook the repo already keeps.
Because: moving the hooks location bypasses the hooks a team already runs, without anyone noticing.

## Decisions

### GitHub first
`github-first` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/02-init-and-learning.md
Init and the PR check read PRs and reviews through `gh`. Other hosts come later.
Because: one host done well before several done badly.

### Changes before the map are exempt from the Capability trailer
`pre-map-changes-exempt` · decided by Claude as arbiter, choosing an option · 2026-11-21 · 0989e23
The change check skips changes made before the map existed, including the commit that adds the map.
Because: on a new codebase the commit that adds the map has no capability it could name.
Rejected:
- Name one capability the commit adds: a new codebase has none.
- No trailer, accepting the finding: every first map would arrive with a finding.

### The Session hook stays local
`session-hook-local` · decided by Claude as arbiter, choosing an option · 2026-11-21 · 0989e23
In a repo that doesn't ship one, the Session hook is kept locally with Physa's own store and is never committed by init.
Because: init adds only the map and the AGENTS.md lines, and a hook committed on the init branch isn't checked out until its PR merges, so it couldn't be installed yet.
Rejected:
- Commit the hook with the repo's tools on the init branch: breaks init's footprint, and isn't on the checked-out branch until merge.
- Commit the hook inside the map's folder: likewise unavailable until merge.

### Teams can't replace Physa's skills
`skills-not-replaceable` · decided by dphaener, in their words · 2026-11-21 · d219c03
Physa's skills (mapping, recovering, learning) ship embedded in the binary, and a team can't replace them with its own.
Because: "That steps on our core functionality that we own and could end up with loss of trust in the product due to users making bad decisions."
Rejected:
- A flag naming a replacement skill file, or a replacement kept in the map's folder: either hands the core of the product to the team.

## Questions

### What does the person see while init runs?
`init-progress` · raised by dphaener · 2026-11-19
Init can take minutes on a large codebase. Progress, and how it is shown, are not yet designed.
