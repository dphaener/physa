# Physa

Physa is product management rebuilt for agents: a product is a network of ideas whose strength comes from evidence. This repo holds the open-source core (Go). The hosted team layer (Rails) lives in its own repo.

The repo is in design. There is no code yet.

## Read first

1. `.physa/glossary.md`: the only words we use for Physa's concepts.
2. `.physa/product.md`, then the files in `.physa/capabilities/` your task touches: Physa's own map, with the rules and decisions that apply.
3. `docs/vision.md`: what Physa is and why.
4. `docs/architecture/`: settled designs, numbered in order.
5. `docs/research/`: background. Read when a design refers to it.

Anchor commits with `Capability: <file name in .physa/capabilities>`.

## Language

- Use glossary terms exactly. Never use a term's _Avoid_ aliases, in code, docs, commits or conversation.
- Never write "ADR", in any form, anywhere.
- When a new concept needs a word, propose a glossary entry before using it.

## Working with the owner

- The owner decides; agents propose. Ask before deciding anything that is hard to reverse, surprising, or a real trade-off.
- Ask in numbered rounds with a recommended answer each, worded so "yes" accepts it.
- Record what was settled in the relevant doc, in the owner's terms.

## Code (once it exists)

- Plain Go, mostly the standard library. Small packages, each with one job and a short doc stating its promise in glossary terms.
- The core imports nothing above it; CI enforces this.
- Every package has a size budget. Going over means cutting scope, not raising the budget.
- Commits are small and atomic, usually one per PR.
