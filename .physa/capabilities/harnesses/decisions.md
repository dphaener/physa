# Harnesses · Decisions

## Contents

- [The owner's harness is the first](#the-owners-harness-is-the-first)
- [A Rex view plugin](#a-rex-view-plugin)
- [A harness is files, an adapter is code](#a-harness-is-files-an-adapter-is-code)
- [A harness is whatever meets Physa's contract](#a-harness-is-whatever-meets-physas-contract)
- [Native subagents inside a role](#native-subagents-inside-a-role)
- [Isolation is the engine's](#isolation-is-the-engines)
- [The team picks the merge method](#the-team-picks-the-merge-method)

## Entries

### The owner's harness is the first
`owner-harness-first` · decided by dphaener, in their words · 2026-11-19 · from docs/research/harness-spec.md
The harness descended from forge and shipit through afk builds Physa from day one, and is the first harness plugin.
Because: the owner trusts it, and building with it proves harnesses are pluggable.

### A Rex view plugin
`rex-plugin` · decided by dphaener, in their words · 2026-11-19 · from docs/vision.md
A Rex layer for the owner and Rex power users, as the first view plugin.
Because: it proves views plug in, alongside harnesses.

### A harness is files, an adapter is code
`harness-files` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/archive/architecture/04-harness-interface.md
A harness is prompts and skills; the engine runs no harness code. Adapters, one per agent, are compiled into the core, Claude Code first.
Because: the engine must enforce what a harness promises, and it cannot enforce code it runs on the harness's behalf.

### A harness is whatever meets Physa's contract
`harness-contract` · decided by dphaener, accepting a proposal · 2026-11-26 · from docs/architecture/16-engine-runs-work.md
Physa defines a contract and hosts nothing: it calls start, result and stop, and listens for state, asks, calls, findings and an outcome per acceptance item, through files, environment, JSON and exit codes, bound to the team's own commands by a file in the repo. A team takes the levels it wants; running roles one by one is the deepest, not the entry point. Each run records its level.
Because: the owner, playing a team that won't change its harness ("I'm not changing my harness"), then: "Let's think about this as an API with strict boundaries."
Rejected:
- Every harness installed into one skeleton, every role an engine session, the plugin's prompts never edited (`install-harness`, 2026-11-20): it reached inside a harness, and a team's working harness didn't fit.
- A harness format written for Physa: a rewrite nobody makes.
- Redirecting native subagent spawns with hooks: per-agent tricks, and hooks are not universal.

### Native subagents inside a role
`native-subagents` · decided by dphaener, in their words · 2026-11-20 · from docs/archive/architecture/04-harness-interface.md
Within a role, native subagents (research, exploring) are normal and share the role's workspace, identity and records. Only starting a role goes through the engine.
Because: research agents when planning "should 100% be a normal thing that is allowed" (the owner).

### Isolation is the engine's
`engine-isolation` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/archive/architecture/04-harness-interface.md
Check roles get a read-only checkout and briefs the engine builds; their reads of build records are refused. The engine re-runs the Checks: block and acceptance itself. Caps per change are enforced by the engine.
Because: in Rhiza, checks shared the build worktree and conductor retries had no cap.

### The team picks the merge method
`team-merge-method` · decided by dphaener, in their words · 2026-11-19 · from docs/archive/architecture/04-harness-interface.md
Squash, merge commit or rebase is a team setting. Changes are anchored by PR number and every session's commit carries the trailers.
Because: "a very personal decision, the developer/team should decide this".
Rejected:
- Always squashing to one commit per change.
