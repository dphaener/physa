# Engine · Decisions

## Contents

- [A conductor on a durable engine](#a-conductor-on-a-durable-engine)
- [One engine per machine, one store per repo](#one-engine-per-machine-one-store-per-repo)
- [Side effects are durable intents](#side-effects-are-durable-intents)
- [Every run in its own hidden worktree](#every-run-in-its-own-hidden-worktree)
- [Headless sessions, watchable from any view](#headless-sessions-watchable-from-any-view)
- [Stalls go to Needs you](#stalls-go-to-needs-you)
- [Appetite is tokens](#appetite-is-tokens)
- [Rework is recorded for every change](#rework-is-recorded-for-every-change)

## Entries

### A conductor on a durable engine
`conductor-on-engine` · decided by dphaener, in their words · 2026-11-13 · from docs/research/rhiza-v1-lessons.md
One conductor agent per piece of work, on an engine that only enforces and records.
Because: the conductor shipped four features cleanly in one night.
Rejected:
- A deterministic 56-step workflow: about 75 minutes per run, and 76% of runs needed a second review cycle.

### One engine per machine, one store per repo
`one-engine` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/03-engine-and-store.md
One engine per machine is the only writer, started by the CLI on demand, with a foreground mode for CI. Each repo has one store at `.git/physa/physa.db`; state tables are the truth, and events are never replayed.
Because: Rhiza kept state in a journal, about 10 loose JSON files and three lock copies.
Rejected:
- Replaying an event log to answer reads: Rhiza's 127 MB log made `rhiza check` take 15 seconds.

### Side effects are durable intents
`durable-intents` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/03-engine-and-store.md
Anything that touches the outside world is written down before it happens and is safe to repeat, so the engine can be killed at any moment and pick up where it was.
Because: "dumb but durable" (the owner, about Rhiza).

### Every run in its own hidden worktree
`run-worktrees` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/03-engine-and-store.md
Each run works in its own worktree and branch under `.git/physa/work/`, removed when the run ends.
Because: Rhiza needed sweepers and left 15 GB of dead worktree indexes.

### Headless sessions, watchable from any view
`headless-watchable` · decided by dphaener, in their words · 2026-11-19 · from docs/architecture/03-engine-and-store.md
Sessions run headless. Watching a live session is offered by the engine's API to every view, not only the Rex plugin.
Because: headless removes stuck dialogs; the owner still wants to watch agents work.
Rejected:
- Watching only through the Rex plugin: "I'm not certain it HAS to be a Rex plugin".

### Stalls go to Needs you
`stalls-need-you` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/03-engine-and-store.md
A session without progress for 15 minutes goes to Needs you; teams can allow one automatic restart first.
Because: a fixer that never said done stalled a Rhiza run for 9 hours.

### Appetite is tokens
`appetite-tokens` · decided by dphaener, in their words · 2026-11-19 · from docs/architecture/03-engine-and-store.md
A bet's appetite is tokens, as reported by adapters, counted from when the bet is accepted and building starts. The steward is told at 80%; at 100% the circuit breaker stops it.
Because: "Time doesn't really matter as much. Agents are fast." Tokens are what is actually spent: "Call it what it is."
Rejected:
- Time: "time doesn't matter at all anymore".
- Money: tokens priced per model hide what is really spent.

### Rework is recorded for every change
`record-rework` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/03-engine-and-store.md
The engine credits every commit and token to the session and role that produced it, and records review rounds, fix against build tokens, and findings per check. Agents only.
Because: much is spent after builds fixing what checks asked for, and teams need numbers to tune their harness.
