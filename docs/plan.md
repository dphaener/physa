# Plan

Draft 0.3, 2026-11-23. Uses `.physa/glossary.md` terms only. Reset around the core (vision §5, decision `core-and-line`); the plan as it stood before, steps 0–11, is in `docs/archive/plan.md`.

## What is built

- **The map, read by agents** (01, 09): `.physa/` in capability folders, `physa map index` and `physa map slice`, `physa check map` and `physa map fmt`.
- **Changes checked against the map** (01, 03): `Capability:` trailers, anchor checks, `physa check` in CI on every PR, slice records.
- **Learning** (08): `physa map learn` reads merged changes and their sessions and drafts one batched `physa/learn` PR for stewards to review.
- **Init** (02): `physa init` finds its own sources, proposes capabilities and recovers each one's rules, decisions, questions and terms, every entry citing its source; it estimates its cost and asks before the large spend (10). A large repo's propose runs in shares and a merge (02 §2.3.1). Gather reads twelve months of merged PRs by default, with no PR limit unless `--max-prs` sets one, waits out the forge's rate limits, and keeps what it has when the forge stops it, marking the stored gather stopped with its date and reason, so `--propose` and `--recover` warn and still run (02 §2.2, 15 §7). `physa init --areas` divides the repo's source files into areas for the code-first phases (15 §3), stored in init's record in `.git/physa`; the full run does not run it yet. `physa init --walk` walks each stored area and names its capabilities with their footprints, kept in that same record; it is a phase alone, and the full run does not run it yet. `physa init --place` puts each gathered PR and change on the capabilities that own most of its files, by the stored footprints, with no session; it is a phase alone too, and the full run does not run it yet. Tried on nebari, where its PR is still open.
- **The engine, without agents** (03): one engine per machine with its local store, events and durable intents; ideas and pursuits from the CLI (`physa idea`, `physa pursue`, `physa pursuit`, with a pursuit's plan of changes); spend counted and weighed from each adapter's cost (10); engineering evidence from version control and the forge (`physa evidence`).
- **The skill for people's agents** (13): `physa skill show` prints the guide a person's agent loads, embedded in the binary; `setup`, `status` and `remove` are built for Claude Code, Codex, OpenCode and Pi, with the session hook that runs `physa skill session` for Claude Code and Codex; init's notice (13 §5) is built.

## What comes next

Designed in the open, starting from the core (vision §5) and keeping to it: whatever a design adds beyond the core is workflow a team brings, not something Physa prescribes. Until ideas can be shared between clones, that design work happens in docs and PRs in this repo, not in Physa's own store.

The open parts, in no fixed order:

- How strength is worked out, and from which evidence that means the same in any workflow (core item 1; the earlier model is in `docs/archive/architecture/05-earned-release-and-strength.md`).
- How a signal is read and resolved, and by what (core item 10), with and without earned release.
- How a team plugs in its own workflow, and what Physa needs back from it to keep the core true.
- Sharing ideas and pursuits between people.
- How agents run the work (harnesses), and the views.
