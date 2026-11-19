---
promise: Every built idea can be shown, widened or turned off for real users without a deploy, and its flag never outlives it.
stewards: [dphaener]
---

# Flags

## Rules

### Behind a flag by default
`flag-by-default` · from docs/vision.md
Built ideas ship behind a flag. Teams set exemptions by policy.
Because: landing code and exposing users must be separate steps.

### A flag lives exactly as long as its idea
`flag-lifetime` · from docs/vision.md
Established: removing the flag is proposed. Withered: removing the code is proposed.
Because: stale flags are hidden debt; decay removes them by design.

## Decisions

### Own the meaning, integrate the switch
`openfeature` · decided by dphaener · 2026-11-19 · from docs/vision.md
Physa owns the link between an idea and its flag, the flag's lifetime, signals and rollback. Evaluation goes through OpenFeature, so teams keep LaunchDarkly, Statsig or PostHog.
Because: the meaning is new; the switch is commoditized, and flag vendors are moving toward intent.
Rejected:
- Owning the whole flag stack: fights established vendors on infrastructure.

### A self-hosted flag service in the core
`own-flag-service` · decided by dphaener · 2026-11-19 · from docs/vision.md
The open-source core includes a small flag service as the default OpenFeature provider.
Because: instant rollback must work for a solo developer with no vendor and no hosted layer.
