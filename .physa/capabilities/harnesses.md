---
promise: Teams bring their own agent workflows, skills and agents, and Physa hosts them.
stewards: [dphaener]
---

# Harnesses

## Rules

### What prompts promise, the engine enforces
`enforce-promises` · from docs/research/harness-spec.md
Read-only checkouts for verifiers and checks, check briefs built by the engine, explicit caps on retries and fix cycles.
Because: the owner's harness promised isolation its runtime did not give.

### The core never imports a plugin
`no-plugin-imports` · from docs/vision.md
Harness and view plugins use the same public interface as the hosted layer.
Because: Rhiza's core imported its own TUI and web, and its web layer had to copy 33 types.

## Decisions

### The owner's harness is the first
`owner-harness-first` · decided by dphaener, in their words · 2026-11-19 · from docs/research/harness-spec.md
The harness descended from forge and shipit through afk builds Physa from day one, and is the first harness plugin.
Because: the owner trusts it, and building with it proves harnesses are pluggable.

### A Rex view plugin
`rex-plugin` · decided by dphaener, in their words · 2026-11-19 · from docs/vision.md
A Rex layer for the owner and Rex power users, as the first view plugin.
Because: it proves views plug in, alongside harnesses.

### A harness is files, an adapter is code
`harness-files` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/04-harness-interface.md
A harness is a manifest and role prompts, installed per machine from a git URL at a pinned commit; the engine runs no harness code. Adapters, one per agent, are compiled into the core, Claude Code first.
Because: the engine must enforce what a harness promises, and it cannot enforce code it runs on the harness's behalf.

### Isolation is the engine's
`engine-isolation` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/04-harness-interface.md
Check roles get a read-only checkout and briefs the engine builds; their reads of build records are refused. The engine re-runs the Checks: block and acceptance itself. Caps per change are enforced by the engine.
Because: in Rhiza, checks shared the build worktree and conductor retries had no cap.

### The team picks the merge method
`team-merge-method` · decided by dphaener, in their words · 2026-11-19 · from docs/architecture/04-harness-interface.md
Squash, merge commit or rebase is a team setting. Changes are anchored by PR number and every session's commit carries the trailers.
Because: "a very personal decision, the developer/team should decide this".
Rejected:
- Always squashing to one commit per change.
