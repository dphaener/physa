# Init · Rules

## Contents

- [Uncertain knowledge becomes a question](#uncertain-knowledge-becomes-a-question)
- [The first map arrives as a PR](#the-first-map-arrives-as-a-pr)
- [Init has an appetite](#init-has-an-appetite)
- [A known footprint](#a-known-footprint)
- [Init refuses a mapped repo before spending](#init-refuses-a-mapped-repo-before-spending)
- [Installing a hook never disables the repo's own](#installing-a-hook-never-disables-the-repos-own)

## Entries

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
