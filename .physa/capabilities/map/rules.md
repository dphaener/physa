# Map · Rules

## Contents

- [Why, never what](#why-never-what)
- [Anchored to changes, not files](#anchored-to-changes-not-files)
- [One folder per capability](#one-folder-per-capability)
- [Reading the map never replays history](#reading-the-map-never-replays-history)
- [Agents speak in glossary terms](#agents-speak-in-glossary-terms)

## Entries

### Why, never what
`why-not-what` · from docs/architecture/01-map-and-slice.md
The map holds promises, rules, decisions, questions and terms. It never describes code and never names file paths; a rule about a place says so in words.
Because: agents read code and find files themselves; what they can't recover is why. A description of code is a worse copy that rots.

### Anchored to changes, not files
`change-anchors` · from docs/architecture/01-map-and-slice.md
Every rule and decision cites the commits or PRs that made it real. New commits carry the anchor as a trailer.
Because: commits never move, so anchors never rot, and a decision's changes are what a rollback reverts.

### One folder per capability
`file-per-capability` · from docs/architecture/09-capability-folders.md
A capability's rules, decisions and questions live in its own folder, with an index and one file for each kind of entry.
Because: a person reads one place per area, and parallel PRs on different capabilities never conflict.

### Reading the map never replays history
`no-replay` · from docs/research/rhiza-v1-lessons.md
The files are the current state. What aggregates history (strength, decay, usage) lives in the engine's store, never in the read path.
Because: Rhiza replayed a 127 MB event log on every read; `rhiza check` took 15.3 s until a projection was bolted on.

### Agents speak in glossary terms
`one-word` · from docs/vision.md
The glossary is part of the map and travels in every slice, so agents use its terms when they talk with people, write commits and PR descriptions, and write the map. It guides agents; nothing checks people's words.
Because: agents and people confuse each other when one thing has three names, and a tool that polices words gets removed.
