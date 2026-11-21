# Harnesses · Rules

## Contents

- [What prompts promise, the engine enforces](#what-prompts-promise-the-engine-enforces)
- [The core never imports a plugin](#the-core-never-imports-a-plugin)

## Entries

### What prompts promise, the engine enforces
`enforce-promises` · from docs/research/harness-spec.md
Read-only checkouts for verifiers and checks, check briefs built by the engine, explicit caps on retries and fix cycles.
Because: the owner's harness promised isolation its runtime did not give.

### The core never imports a plugin
`no-plugin-imports` · from docs/vision.md
Harness and view plugins use the same public interface as the hosted layer.
Because: Rhiza's core imported its own TUI and web, and its web layer had to copy 33 types.
