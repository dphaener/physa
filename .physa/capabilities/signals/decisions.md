# Signals · Decisions

## Contents

- [Engineering evidence is always on](#engineering-evidence-is-always-on)
- [Engineering evidence from git and CI; runtime through OpenTelemetry](#engineering-evidence-from-git-and-ci-runtime-through-opentelemetry)
- [One SDK for flags and signal events](#one-sdk-for-flags-and-signal-events)
- [Teams set when a result is clear](#teams-set-when-a-result-is-clear)

## Entries

### Engineering evidence is always on
`engineering-always-on` · decided by dphaener, in their words · 2026-11-20 · from docs/archive/architecture/05-earned-release-and-strength.md
Reverts, rework, defects and lead time from git, PRs and CI are recorded whatever else is switched on or off.
Because: "it should be always on no matter what" (the owner); it needs nothing in the app.

### Engineering evidence from git and CI; runtime through OpenTelemetry
`engineering-evidence` · decided by dphaener, in their words · 2026-11-19 · from docs/vision.md
Reverts, rework, defects and lead time come from git, PRs and CI. Errors and performance come through OpenTelemetry.
Because: architecture ideas need evidence too, and monitoring is not ours to build.

### One SDK for flags and signal events
`one-sdk` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/vision.md
The same SDK evaluates flags and records signal events.
Because: exposure and outcome must join on the same person.
Rejected:
- Building product analytics.
- Integrating only with existing analytics: leaves solo developers with nothing.

### Teams set when a result is clear
`clear-threshold` · decided by dphaener, in their words · 2026-11-20 · from docs/archive/architecture/05-earned-release-and-strength.md
Results come from a Bayesian comparison; a result is clear past a probability each team or developer sets, 95% by default.
Because: "that is for individuals/teams to decide".
Rejected:
- p-values and significance language.
