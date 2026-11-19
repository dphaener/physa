---
promise: Teams building with agents decide well, see how their product changes, move fast with confidence, and can undo any mistake.
---

# Physa

## Rules

### Never write "ADR"
`no-adr` · from docs/vision.md
Never use "ADR" or "architecture decision record", in code, docs, commits or conversation. Say "decision".
Because: the owner rejects the term; one word for one thing.

### Agents run on the customer's side
`agents-customer-side` · from docs/vision.md
Physa never runs an agent of its own. Every agent step uses the customer's agent and keys.
Because: it keeps trust simple and costs low, and teams bring their own agents.

### Source code never leaves the customer
`source-stays` · from docs/vision.md
The hosted layer receives the map, PR metadata, records and signal events, never source.
Because: developer trust comes first, and code is the thing they guard most.

### Nothing changes silently
`no-silent-change` · from docs/vision.md
Physa proposes; people accept. Every change to the map, a flag or a policy arrives as something a person can accept or refuse.
Because: a tool that edits behind people's backs loses their trust.

### Every claim cites its source
`cite-sources` · from docs/vision.md
Every rule and decision names the change or document it comes from.
Because: people can check it, and stale knowledge can be found.

### Opinionated about mechanisms, not policy
`teams-set-policy` · from docs/vision.md
Physa decides how things work. Teams decide who may bet on what, how strict each level of stakes is, and how much agents may do.
Because: teams work differently, and the owner wants the core opinionated but the edges flexible.

### No internal IDs on screens
`no-ids-on-screens` · from docs/research/rhiza-v1-lessons.md
No surface a person sees shows internal identifiers.
Because: "it's nonsense to a user" (the owner, about Rhiza).

## Decisions

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

## Questions

### How is the hosted layer priced?
`pricing` · raised by dphaener · 2026-11-19
Postponed until the product exists.
