# Map · Decisions

## Contents

- [The glossary changes only on purpose](#the-glossary-changes-only-on-purpose)
- [In the repo, hidden directory](#in-the-repo-hidden-directory)
- [Superseded decisions fold into their replacement](#superseded-decisions-fold-into-their-replacement)

## Entries

### The glossary changes only on purpose
`glossary-intentional` · decided by dphaener, in their words · 2026-11-19 · from docs/architecture/01-map-and-slice.md
Nothing learned from PRs edits the glossary. A person changes it when they mean to.
Because: the glossary is the team's chosen language, not a record of how people happen to talk.
Rejected:
- Proposing new or clarified terms from what PRs and reviews say.
- Flagging avoided words in commits and PRs: polices people, mostly false alarms (27 on the first map, nearly all ordinary English).

### In the repo, hidden directory
`in-repo` · decided by dphaener, in their words · 2026-11-19 · from docs/architecture/01-map-and-slice.md
The map lives in `.physa/` in the team's repo.
Because: reviewable in PRs, shared through git, trusted because it is visible.
Rejected:
- Outside the repo, with generated files committed: less visible, so less trusted.
- A visible `physa/` directory: more clutter for little gain.

### Superseded decisions fold into their replacement
`fold-superseded` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/01-map-and-slice.md
A superseded or reversed decision becomes a `Rejected:` item on the decision that replaced it. Git is the archive.
Because: the history that matters stays beside the current choice, without growing the map.
