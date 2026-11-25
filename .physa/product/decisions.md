# Physa · Decisions

## Contents

- [Open-source core, closed hosted layer](#open-source-core-closed-hosted-layer)
- [Go core, Rails hosted layer](#go-core-rails-hosted-layer)
- [Named Physa](#named-physa)
- [Strength replaces mode](#strength-replaces-mode)
- [A core it defends, workflow beyond it](#a-core-it-defends-workflow-beyond-it)
- [Grouped commands](#grouped-commands)
- [Apache-2.0 for the core](#apache-20-for-the-core)

## Entries

### Open-source core, closed hosted layer
`open-core` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/vision.md
The core is everything one developer or one repo needs, fully working, under a permissive license. The hosted layer is where people, repos and production data come together, and is closed.
Because: the open core is how developers bring Physa into their teams.
Rejected:
- Source-available hosted layer: no advantage worth the exposure.

### Go core, Rails hosted layer
`go-and-rails` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/vision.md
The core is Go, kept small and plain. The hosted layer is Rails, and reads the map by calling the core binary.
Because: the core must install with one command anywhere; the hosted layer is where most screens live and where the owner is strongest.
Rejected:
- All Ruby: a Ruby CLI fails the one-command install.
- All TypeScript: no strength of the owner's, large binaries.
- Rust core: hardest for the owner to review.

### Named Physa
`name` · decided by dphaener, choosing an option · 2026-11-19 · from docs/vision.md
The product is Physa, after Physarum.
Because: Physarum, the slime mold whose networks inspired the design.
Rejected:
- Rhiza: no longer fits the product.
- Hypha: an existing developer CLI uses it, with the same metaphor.
- Phloem, Tendril, Loam, Graft: spelling, collisions or connotations.

### Strength replaces mode
`strength-not-mode` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/vision.md
How proven an idea or capability is comes from measured strength. Strictness and agent autonomy follow from it by team policy.
Because: a mode someone sets goes stale; a slime mold's tubes are as thick as their flow.
Rejected:
- Explore and stable modes: set by hand, and mixed four ideas into one switch.
- Named states for ideas: the same problem again.

### A core it defends, workflow beyond it
`core-and-line` · decided by dphaener, in their words · 2026-11-23 · from docs/vision.md
Physa defends a core and guards the line around it: strength tracked, the map holding why and anchored to changes, sources cited, one word for one thing, people deciding, an engine that enforces and never judges, every change undoable, spend always counted, and a pursuit that names its signal. Beyond the line, teams bring their own workflow; Physa prescribes no process, and the Shape Up flow of shaping, pitches and bets is gone.
Because: "we have core things about how the product works that we MUST defend, and we HAVE to guard that line, and beyond that we want users to be able to bring their own workflows." Forcing everyone down a Shape Up path was too prescriptive.
Rejected:
- Ceremony is opt-in (`ceremony-opt-in`, 2026-11-20): one built-in Shape Up flow (idea, bet, changes, landed) with shaping, sign-offs and earned release switched on as a team grows, and appetite always on. It still prescribed the path.
- The full process by default, with exemptions.

### Grouped commands
`grouped-cli` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/01-map-and-slice.md
Commands come in groups named by glossary nouns, with verbs as subcommands, and a bare group does its most common job. Only what is run constantly or once per repo is top-level, and each thing has one way to do it, with no aliases.
Because: "Rather than having 100 commands, we should have grouped commands that have sub commands."
Rejected:
- A flat list of top-level commands: it grows with every feature and hides how commands relate.

### Apache-2.0 for the core
`core-license` · decided by dphaener, accepting a proposal · 2026-11-25 · from docs/vision.md
The core is licensed under Apache-2.0, with the copyright held by Darin Haener in NOTICE. Files carry no license headers.
Because: its patent grant and contribution terms cover outside PRs without a separate agreement, and its trademark clause keeps the name Physa for the hosted layer when the core is forked.
Rejected:
- MIT: shorter, but silent on patents, contributions and the name.
- BSD-3-Clause: the same gaps as MIT.
