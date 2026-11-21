# Physa

Physa is product management rebuilt for agents: a product is a network of ideas whose strength comes from evidence. This repo holds the open-source core (Go). The hosted team layer (Rails) lives in its own repo.

The repo is early: the Go module, `cmd/physa`, its code rules, the map reader, `physa check` and `physa map` with their subcommands (`index`, `slice`, `changes`, `records`, `hooks`, and `learn` with and without `--signals`, reading session logs, and its `physa/learn` branch), `physa init --gather`, the version-control interface, the forge interface, the adapter for Claude Code and the store exist; the engine does not yet.

<!-- physa -->
Before changing code, run `go run ./cmd/physa map index`, then `go run ./cmd/physa map slice <capabilities>` for the ones your task touches.
No CLI? Read .physa/README.md.
Run `go run ./cmd/physa map hooks` once per clone so your commits carry `Session:`.
<!-- /physa -->

## Read first

1. `.physa/glossary.md`: the only words we use for Physa's concepts.
2. `.physa/product.md`, then the files in `.physa/capabilities/` the index shows your task touches: Physa's own map, with the rules and decisions that apply.
3. `docs/vision.md`: what Physa is and why.
4. `docs/architecture/`: settled designs, numbered in order.
5. `docs/research/`: background. Read when a design refers to it.

Anchor commits with `Capability: <capability>` (its file name in .physa/capabilities, without .md).

Commit messages follow Conventional Commits, written with the `commit-style` skill (`type(scope): Subject`, then a short prose body). Older commits in this repo don't; don't copy them.

## Language

- Speak and write in glossary terms: in conversation with the owner, commits, PR descriptions, docs and code. Don't use the words a term's _Avoid_ list names.
- Never write "ADR", in any form, anywhere.
- The glossary changes only when the owner means to change it. If a new concept seems to need a word, ask; don't add one.

## Working with the owner

- The owner decides; agents propose. Ask before deciding anything that is hard to reverse, surprising, or a real trade-off.
- Ask in numbered rounds with a recommended answer each, worded so "yes" accepts it.
- Record what was settled in the relevant doc, in the owner's terms.

## Code

- Plain Go, mostly the standard library. Small packages, each with one job and a short doc stating its promise in glossary terms.
- The core imports nothing above it: a package under `internal/` never imports `cmd/` or `plugins/`. CI enforces this with `go run ./tools/codecheck`.
- Every package stays under 800 non-blank lines of non-test Go and has a doc comment stating its promise (`tools/codecheck`). Going over means cutting scope, not raising the budget.
- Commits are small and atomic, usually one per PR.
