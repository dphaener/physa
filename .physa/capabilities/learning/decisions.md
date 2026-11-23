# Learning · Decisions

## Contents

- [The PR check does not review code](#the-pr-check-does-not-review-code)
- [Physa records what it observes](#physa-records-what-it-observes)
- [Checks and sync run here](#checks-and-sync-run-here)
- [Recurring findings become proposed rules](#recurring-findings-become-proposed-rules)
- [Answers by the arbiter are credited to the arbiter](#answers-by-the-arbiter-are-credited-to-the-arbiter)

## Entries

### The PR check does not review code
`no-code-review` · decided by dphaener, in their words · 2026-11-20 · from docs/archive/architecture/07-records-and-verification.md
The PR check verifies only the capability each change serves and the map. No agent judges the change; what needs judgment is learned after merge.
Because: "We shouldn't care about reviewing someone else's code, that's their problem."
Rejected:
- A reviewer agent in the PR check: an unverified opinion, tokens on every push, and a second pass over what the team's own checks already did.

### Physa records what it observes
`record-observed` · decided by dphaener, in their words · 2026-11-20 · from docs/archive/architecture/07-records-and-verification.md
Slice records, tokens, rework and asks come from what Physa observes: `physa map slice` records, the agent's session logs found through the `Session:` trailer, and later the engine's sessions. Agents never report them. Agents declare only intent, in `Capability:`.
Because: "Physa has access to every agent log correct? It knows what the agent did, right? That can be used can it not?"
Rejected:
- A `Loaded:` trailer: a claim, and it overstated; in #12 none of the agents that wrote the code loaded a slice.

### Checks and sync run here
`checks-run-here` · decided by dphaener, in their words · 2026-11-20 · from docs/archive/architecture/07-records-and-verification.md
The PR check and `physa sync` run on the developer's machine, before or after a PR; facts come from version control, and digests travel through it.
Because: "I feel like that piece and the sync need to happen here, either before or after a PR."
Rejected:
- Results kept in GitHub check runs: ties the core to one forge and to CI Physa doesn't control.

### Recurring findings become proposed rules
`findings-to-rules` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/03-engine-and-store.md
When the same kind of finding recurs on a capability, learning proposes a rule on the next `physa/learn` PR.
Because: a rule in the slice costs a few lines; the same lesson paid in review costs a build, a round and a fix.

### Answers by the arbiter are credited to the arbiter
`arbiter-answers-credited` · decided by dphaener, in their words · 2026-11-21 · from docs/architecture/08-learning.md
When the arbiter answers on a person's behalf, the answer is credited to Claude as arbiter, never to that person.
Because: "Yes the arbiter should always answer in this way so it's attributable. If there is a more durable way to do this we should explore it."
