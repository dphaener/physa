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
