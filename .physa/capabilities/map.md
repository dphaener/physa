---
promise: A team's product knowledge (why the product is the way it is) lives in their repo, small, readable and reviewable like code.
stewards: [dphaener]
---

# Map

## Rules

### Why, never what
`why-not-what` · from docs/architecture/01-map-and-slice.md
The map holds promises, rules, decisions, questions and terms. It never describes code and never names file paths; a rule about a place says so in words.
Because: agents read code and find files themselves; what they can't recover is why. A description of code is a worse copy that rots.

### Anchored to changes, not files
`change-anchors` · from docs/architecture/01-map-and-slice.md
Every rule and decision cites the commits or PRs that made it real. New commits carry the anchor as a trailer.
Because: commits never move, so anchors never rot, and a decision's changes are what a rollback reverts.

### One file per capability
`file-per-capability` · from docs/architecture/01-map-and-slice.md
A capability's rules, decisions and questions live in its own file.
Because: a person reads one page per area, and parallel PRs on different capabilities never conflict.

### Reading the map never replays history
`no-replay` · from docs/research/rhiza-v1-lessons.md
The files are the current state. What aggregates history (strength, decay, usage) lives in the engine's store, never in the read path.
Because: Rhiza replayed a 127 MB event log on every read; `rhiza check` took 15.3 s until a projection was bolted on.

### Agents speak in glossary terms
`one-word` · from docs/vision.md
The glossary is part of the map and travels in every slice, so agents use its terms when they talk with people, write commits and PR descriptions, and write the map. It guides agents; nothing checks people's words.
Because: agents and people confuse each other when one thing has three names, and a tool that polices words gets removed.

## Decisions

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
