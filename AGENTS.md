# Physa

Physa is product management rebuilt for agents: a product is a network of ideas whose strength comes from evidence. This repo holds the open-source core (Go). The hosted team layer (Rails) lives in its own repo.

The core is one repo and one binary holding two products: **the map** (`init`, `map`, `check` and `learn`, which make, read, check and keep a team's map), usable alone, and **the engine** (ideas, pursuits and evidence, with the engine process that moves them), on top of it.

The repo is early: the Go module, `cmd/physa`, its code rules, the map reader, `physa check` and `physa map` with their subcommands (`index`, `slice`, `changes`, `records`, `hooks`, `fmt`, and `learn` with and without `--signals`, reading session logs, and its `physa/learn` branch), bare `physa init` with `--write`, `--push`, `--pr`, `--hooks` and `--answers`, and its phases `--gather`, `--propose` and `--recover`, the version-control interface, the forge interface, the adapter for Claude Code, stating its model's window, propose in shares when one session cannot hold its prompt, and the store, the engine process (`physa engine`), its local API (`physa api`), the engine's state tables, events and durable intents (`internal/state`, `internal/repos`), ideas and pursuits from the CLI (`physa idea`, `physa pursue`, `physa pursuit`, over the engine's API) and the output package exist; the rest of the engine does not yet: runs.

<!-- physa -->
Before changing code, run `go run ./cmd/physa map index`, then `go run ./cmd/physa map slice <capabilities>` for the ones your task touches.
No CLI? Read .physa/README.md.
Run `go run ./cmd/physa map hooks` once per clone so your commits carry `Session:`.
<!-- /physa -->

## Read first

1. `.physa/glossary.md`: the only words we use for Physa's concepts.
2. `.physa/product/`, then the folders in `.physa/capabilities/` the index shows your task touches: Physa's own map, with the rules and decisions that apply.
3. `docs/vision.md`: what Physa is and why.
4. `docs/architecture/`: settled designs, numbered in order. `docs/archive/` holds designs set aside at the reset of 2026-11-23 (vision §5): reference, not settled.
5. `docs/plan.md`: what is built and what comes next.
6. `docs/research/`: background. Read when a design refers to it.

Anchor commits with `Capability: <capability>` (its folder name in .physa/capabilities).

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

- Plain Go, mostly the standard library. Three exceptions, chosen by the owner: Charm's libraries for the CLI on a terminal and one WebSocket library for the engine's API (03 §16), and Cobra (github.com/spf13/cobra) for the command tree. Fang is left out for now, because it is experimental and can wrap the root later in one call. Small packages, each with one job and a short doc stating its promise in glossary terms.
- The core imports nothing above it: a package under `internal/` never imports `cmd/` or `plugins/`. CI enforces this with `go run ./tools/codecheck`.
- The map never imports the engine: a package under `internal/` on the map's side, including the foundations both use (version control, the forge, the adapter, the store, output), never imports a package on the engine's side. The engine may import the map's. `engineSide` in `tools/codecheck` lists the engine's packages; a new engine package goes there.
- Every package stays under 800 non-blank lines of non-test Go and has a doc comment stating its promise (`tools/codecheck`). Going over means cutting scope, not raising the budget.
- Commits are small and atomic, usually one per PR.
