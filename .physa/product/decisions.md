# Physa · Decisions

## Contents

- [Open-source core, closed hosted layer](#open-source-core-closed-hosted-layer)
- [Go core, Rails hosted layer](#go-core-rails-hosted-layer)
- [Named Physa](#named-physa)
- [Strength replaces mode](#strength-replaces-mode)
- [Ceremony is opt-in](#ceremony-is-opt-in)
- [Grouped commands](#grouped-commands)

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

### Ceremony is opt-in
`ceremony-opt-in` · decided by dphaener, choosing an option · 2026-11-20 · from docs/architecture/05-earned-release-and-strength.md
The map and the engine are the core. Earned release (with the product signals it needs), shaping and sign-offs are opt-in and off for a solo developer, whose path is idea, bet, changes, landed. Appetite, its circuit breaker, engineering evidence and strength stay on.
Because: a solo developer "just want[s] to YOLO everything" (the owner); a team grows into ceremony instead of opting out of it.
Rejected:
- The full process by default, with exemptions.

### Grouped commands
`grouped-cli` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/01-map-and-slice.md
Commands come in groups named by glossary nouns, with verbs as subcommands, and a bare group does its most common job. Only what is run constantly or once per repo is top-level, and each thing has one way to do it, with no aliases.
Because: "Rather than having 100 commands, we should have grouped commands that have sub commands."
Rejected:
- A flat list of top-level commands: it grows with every feature and hides how commands relate.
