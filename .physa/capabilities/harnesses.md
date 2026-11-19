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
`owner-harness-first` · decided by dphaener · 2026-11-19 · from docs/research/harness-spec.md
The harness descended from forge and shipit through afk builds Physa from day one, and is the first harness plugin.
Because: the owner trusts it, and building with it proves harnesses are pluggable.

### A Rex view plugin
`rex-plugin` · decided by dphaener · 2026-11-19 · from docs/vision.md
A Rex layer for the owner and Rex power users, as the first view plugin.
Because: it proves views plug in, alongside harnesses.
