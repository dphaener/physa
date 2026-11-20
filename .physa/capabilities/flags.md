---
promise: With flags on, every built idea can be shown, widened or turned off for real users without a deploy, and its flag never outlives it.
stewards: [dphaener]
---

# Flags

## Rules

### Flags behind every built idea, once flags are on
`flag-when-on` · from docs/vision.md
With flags on, built ideas ship behind a flag; teams set exemptions by policy.
Because: landing code and exposing users must be separate steps.

### A flag lives exactly as long as its idea
`flag-lifetime` · from docs/vision.md
Established: removing the flag is proposed. Withered: removing the code is proposed.
Because: stale flags are hidden debt; decay removes them by design.

## Decisions

### Flags are opt-in
`flags-opt-in` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/05-flags-signals-strength.md
The whole flags feature (physa serve, the provider, experiments, guardrails, automatic rollback) is opt-in and off by default, with the product signals that need it.
Because: "If I as a solo developer just want to YOLO everything, I don't care about going through all of that ceremony."
Rejected:
- Built ideas behind a flag by default.

### Own the meaning, integrate the switch
`openfeature` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/vision.md
Physa owns the link between an idea and its flag, the flag's lifetime, signals and rollback. Evaluation goes through OpenFeature, so teams keep LaunchDarkly, Statsig or PostHog.
Because: the meaning is new; the switch is commoditized, and flag vendors are moving toward intent.
Rejected:
- Owning the whole flag stack: fights established vendors on infrastructure.

### A self-hosted flag service in the core
`own-flag-service` · decided by dphaener, choosing an option · 2026-11-19 · from docs/vision.md
The open-source core includes a small flag service as the default OpenFeature provider.
Because: instant rollback must work for a solo developer with no vendor and no hosted layer.

### One server next to production, no SDK of our own
`physa-serve` · decided by dphaener, accepting a proposal · 2026-11-20 · from docs/architecture/05-flags-signals-strength.md
`physa serve` (the same binary, one SQLite file) evaluates flags and takes signal events. Apps use OpenFeature SDKs with a thin Physa provider: OFREP to evaluate, OpenFeature's tracking API for events. One flag per idea, keyed by its slug.
Because: a production app cannot ask a laptop whether a flag is on, and teams should not adopt a new SDK to try Physa.
