---
promise: Work keeps moving through its lifecycle without a person or an orchestrating agent pushing it.
stewards: [dphaener]
---

# Engine

## Rules

### The engine never judges
`engine-no-judgment` · from docs/vision.md
The engine moves work, enforces appetites, circuit breakers and gates, supervises runs and keeps the record. Judgment belongs to agents and people.
Because: "the daemon should be dumb but durable" (the owner, about Rhiza).

### The engine owns gates, identity and records
`engine-owns-gates` · from docs/research/harness-spec.md
No harness can grade its own work: verdicts come from the engine's records.
Because: it is what made the owner's harness trustworthy.

### Supervision is built in
`built-in-supervision` · from docs/research/rhiza-v1-lessons.md
Stall detection, resource limits and session cleanup belong to the engine.
Because: Rhiza needed watch, stall and cleanup scripts around it, and five resource incidents in 17 days.

### Count "needed a person"
`count-interventions` · from docs/research/rhiza-v1-lessons.md
Every time a person had to step in to keep work moving is counted, from the first run.
Because: "no orchestrator" was Rhiza's goal six times and was never measured until the end.

### Run records never enter the map
`no-run-records` · from docs/research/rhiza-v1-model-audit.md
Runs, sessions and their evidence live in the engine's store and expire.
Because: 76% of Rhiza's model was agent activity.

## Decisions

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
