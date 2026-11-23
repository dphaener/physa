# Ideas · Decisions

## Contents

- [Pursuits replace bets and pitches](#pursuits-replace-bets-and-pitches)
- [An idea is a thin tube from the start](#an-idea-is-a-thin-tube-from-the-start)
- [Ideas live only in the local store for now](#ideas-live-only-in-the-local-store-for-now)

## Entries

### Pursuits replace bets and pitches
`pursue-against-signal` · decided by dphaener, choosing an option · 2026-11-23 · #68
A pursuit is the moment a person puts their name on building an idea, against a signal. It needs only the signal: an idea's text is the only write-up. A signal has four parts, all required when the pursuit starts: the claim (what should change, in the team's words), the direction (what counts as for and what counts as against), the window (the day it is read, today or later) and the resolver (a named person or a named check). The appetite is optional: with none there is no circuit breaker, and spend is still counted and shown. Appetites are tokens, and hills and countable scopes stay gone.
Because: "I feel like we are potentially being too prescriptive by forcing people down a shape up path" (the owner); a pursuit naming its signal is the part of the flow that belongs to the core (vision §5).
Rejected:
- Shape Up's pitches, bets and shaping, borrowed as a starting point (2026-11-19): a pitch of seven parts before a bet, and an appetite always required.
- Six-week cycles and cooldown: sized to human building speed.

### An idea is a thin tube from the start
`idea-is-tube` · decided by dphaener, in their words · 2026-11-19 · from docs/vision.md
An idea exists the moment a person wants to try something, and belongs to them. A signal is required by a pursuit, not before.
Because: the PM's hunch, the designer's layout and the developer's architecture are each already a feeler.
Rejected:
- Ideas waiting in a queue until they earn a tube.
- "Probe" as the name for an idea in use.

### Ideas live only in the local store for now
`ideas-local-store` · decided by dphaener, choosing an option · 2026-11-21 · 319dc9d
Ideas and pursuits live only in the repo's local store, which is never committed. Until the hosted layer exists they are gone on a re-clone and invisible on a second machine or to a teammate.
Because: it is the simplest place, and enough while Physa is built from one machine; mirroring comes with team digests and the hosted layer.
Rejected:
- The store, mirrored to git notes: team digests on notes are already planned for a later step.
- Committed files in the repo: ideas stay out of the map, since work is not knowledge.
