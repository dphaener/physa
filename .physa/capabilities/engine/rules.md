# Engine · Rules

## Contents

- [The engine never judges](#the-engine-never-judges)
- [The engine owns gates, identity and records](#the-engine-owns-gates-identity-and-records)
- [The engine supervises its own sessions](#the-engine-supervises-its-own-sessions)
- [Count "needed a person"](#count-needed-a-person)
- [Run records never enter the map](#run-records-never-enter-the-map)
- [Migrations are numbered once, with no gaps](#migrations-are-numbered-once-with-no-gaps)

## Entries

### The engine never judges
`engine-no-judgment` · from docs/vision.md
The engine moves work, enforces appetites, circuit breakers and gates, supervises runs and keeps the record. Judgment belongs to agents and people.
Because: "the daemon should be dumb but durable" (the owner, about Rhiza).

### The engine owns gates, identity and records
`engine-owns-gates` · from docs/research/harness-spec.md
No harness can grade its own work: verdicts come from the engine's records.
Because: it is what made the owner's harness trustworthy.

### The engine supervises its own sessions
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

### Migrations are numbered once, with no gaps
`migrations-numbered-once` · from docs/architecture/03-engine-and-store.md
The store's schema changes are numbered migrations, applied in order when it opens. Their numbers run 1, 2, 3 with none shared and none missing; otherwise opening the store fails and names the files.
Because: a second file with a number already applied would be skipped without a word, leaving the schema behind the code.
