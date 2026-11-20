---
promise: The map gets better with every PR, without anyone curating it.
stewards: [dphaener]
---

# Learning

## Rules

### Learn at the PR
`learn-at-pr` · from docs/architecture/02-init-and-learning.md
The map learns where every agent's work ends up: the PR. While it is open, the change is checked against the map; after it merges, its review is learned from.
Because: it is the one place every agent's work passes through, whichever agent wrote it.

### Non-blocking unless the team says otherwise
`non-blocking` · from docs/architecture/02-init-and-learning.md
The PR check reports a neutral status by default. Teams make specific findings blocking by policy.
Because: a new tool that blocks merges gets removed on day two.

### The map update PR never merges itself
`no-self-merge` · from docs/architecture/02-init-and-learning.md
Everything learned after merge is batched into one map update PR that stewards review.
Because: nothing changes silently.

## Decisions

### The PR check does not review code
`no-code-review` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/07-records-and-verification.md
The PR check verifies only the capability each change serves and the map. No agent judges the change; what needs judgment is learned after merge.
Because: "We shouldn't care about reviewing someone else's code, that's their problem."
Rejected:
- A reviewer agent in the PR check: an unverified opinion, tokens on every push, and a second pass over what the team's own checks already did.

### Physa records what it observes
`record-observed` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/07-records-and-verification.md
Slice records, tokens, rework and asks come from `physa slice` and the engine's sessions, not from agents reporting them. Agents declare only intent, in `Capability:`.
Because: "Physa has access to every agent log correct? It knows what the agent did, right? That can be used can it not?"
Rejected:
- A `Loaded:` trailer: a claim, and it overstated; in #12 none of the agents that wrote the code loaded a slice.

### Checks and sync run here
`checks-run-here` · decided by dphaener, in their words · 2026-11-20 · from docs/architecture/07-records-and-verification.md
The PR check and `physa sync` run on the developer's machine, before or after a PR; facts come from version control, and digests travel through it.
Because: "I feel like that piece and the sync need to happen here, either before or after a PR."
Rejected:
- Results kept in GitHub check runs: ties the core to one forge and to CI Physa doesn't control.

### Recurring findings become proposed rules
`findings-to-rules` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/03-engine-and-store.md
When the same kind of finding recurs on a capability, learning proposes a rule on the next `physa/learn` PR.
Because: a rule in the slice costs a few lines; the same lesson paid in review costs a build, a round and a fix.
