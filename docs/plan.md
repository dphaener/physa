# Build plan

Draft 0.2, 2026-11-21. Uses `.physa/glossary.md` terms only. Builds on `docs/vision.md` and architecture 01–07. Items marked (Q) are open questions for the owner.

## Principles

- **Every step is something the owner uses the day it lands.** No step is only foundation.
- **The wedge first.** The map is what wins developers' trust (vision §4), and it needs no engine. The engine comes after the map has proved itself on real repos.
- **Physa builds Physa.** Each step is used on Physa's own repos before anyone else's, then on nebari (a 534-commit Rails app) as the brownfield test and woodshop as the greenfield one.
- **The owner's harness builds it from day one** (`owner-harness-first`). Until Physa's engine can host it (step 7), it runs as a plain Claude Code plugin, the way afk did. **Rhiza is retired and plays no part in building Physa** (the owner: "I don't want Rhiza involved in building this AT ALL").
- **Small, with budgets.** Every package has a size budget from its first commit; the engine is the part most likely to grow by accretion (03 §13).
- **YAGNI.** Nothing in a step that a later step needs but this one does not.

## The steps

Each step: what lands, what the owner can do with it, and when it is done. Commands use the grouped names (`grouped-cli`, 01 §8).

### 0. Audit and slim the owner's harness

- **Status: done** (2026-11-20). The audit is in `docs/research/harness-audit.md`; the slimmed harness is **dharness** (Q6), adopted after a comparison the owner cut short.
- **Why first:** everything after step 0 is built by this harness, and it had not been audited since several model releases (the owner: "Before we build anything with my harness I want to audit it and slim it down").
- **Lands:** an audit of afk and forge-cli, the harnesses Rhiza was built on; a slimmed harness as its own plugin repo; before-and-after measurements on real tasks.
- **The owner can:** build Physa with a harness that is smaller, cheaper in tokens, and no less trusted.
- **Done when:** the slimmed harness matches or beats the current one on rework and tokens over the same tasks, and every rule left in it has a reason.

### 1. The map, read by agents

- **Status: done** (2026-11-20; #4, #8–#10).
- **Lands:** the Go module and its rules in CI (the import rule, package size budgets, a promise doc per package); `physa check map`, `physa map index` and `physa map slice` over `.physa/` (01 §8); the 50 ms latency test on a generated map (01 §9a).
- **The owner can:** have every agent building Physa load slices of Physa's own hand-written map, and have CI reject a malformed map.
- **Done when:** the harness's builders run `physa map slice` instead of reading the map by hand, and the map is checked on every PR.

### 2. Changes checked against the map

- **Status: landed** (2026-11-20; #11–#17); done when the next builds show it in use.
- **Lands:** reading `Capability:` trailers; anchor checks against version control and, when present, the forge; `physa check` verifying each change's capability and the map (07 §5), with `physa check changes` and `physa map changes`; slice records written by `physa map slice` and listed by `physa map records` (07 §4.1); the version-control interface with git as its adapter (07 §6); the store with only the tables these need; the grouped commands (`grouped-cli`).
- **The owner can:** run `physa check` on any branch, here, before or after a PR, and see each change's capability and whether the map holds; see which slices each session loaded.
- **Done when:** `physa check` runs as a step of every Physa build, and the slice records of real builds show which agents loaded which slices.
- **Revised 2026-11-20** by 07: the reviewer agent, the `Loaded:` trailer and the GitHub Action with check-run results were built (#11–#13) and removed (#15).

### 3. Learning

- **Status: landed** (2026-11-21; design 08, #19–#23); the first learn PR (#29) merged. Done when a week of PRs shows it.
- **Lands:** the learning step after merge, run on the developer's machine (07 §1), with its own token appetite; the batched `physa/learn` PR that never merges itself; the three skills Physa ships (mapping, recovering, learning; 02 §4).
- **The owner can:** watch Physa's map improve from its own PRs, reviewing one batched PR instead of writing entries.
- **Done when:** a week of Physa PRs produces a `physa/learn` PR the owner mostly accepts.

### 3a. Capability folders

- **Status:** designed (09, settled 2026-11-21).
- **Lands,** in two PRs (09 Q10): first the folder format, `physa check map` and `physa map fmt`, with Physa's own map converted; then the slice that returns the floor and the headings, `physa map slice --entry`, the slice skill, and `--budget` removed.
- **Why now:** learning makes capability files grow past their limit within weeks, and nebari's init (step 4) should write the new format, not the old.
- **Done when:** Physa's map is in folders, and agents building the next step take decisions with `--entry` (seen in slice records).

### 4. Init, and the first public release

- **Status:** in progress (2026-11-21): gather (4a, `physa init --gather`), propose (4b, `physa init --propose`) and recover with check (4c, `physa init --recover`) landed; the PR on `physa/init` (4d, `physa init --write`) is done, and the trials on nebari and woodshop follow.
- **Lands:** `physa init` (02 §2): gather, propose capabilities, recover per capability, check, the PR on `physa/init`, with its token appetite.
- **Tried on:** nebari (brownfield), after a first change in nebari removes everything Rhiza put there (its 15 MB `.rhiza/`, and Rhiza's traces in README, CLAUDE.md, `bin/gate` and its specs, to be listed exactly before removing); then woodshop (greenfield).
- **The owner can:** put a living map on any repo in one command.
- **Release:** **v0.1, open source** (Q3). The wedge, complete: init, slices, the change check, learning.
- **Done when:** nebari's init PR is good enough to merge with light edits, and `physa check` stays useful on it for two weeks.

### 5. The engine, without agents

- **Lands:** one engine per machine, its socket API, the store's state tables, events and durable intents at `.git/physa/physa.db` (03 §3–§5; the store itself landed in step 2); ideas, pitches (optional), bets, the plan (`physa bet plan set`), appetite in tokens and its circuit breaker; evidence recorded from here on (05 Q14), including engineering evidence from version control, and from CI where a team runs one.
- **The owner can:** keep Physa's own ideas and bets in Physa, from the CLI. No backlog file, no issue tracker.
- **Done when:** Physa's next ideas live only in Physa, and the engine survives being killed at any moment.

### 6. Agents run bets: the default harness

- **Lands:** the rest of the agent adapter (04 §10; its first rows landed in step 2's #13); sessions, workspaces and supervision (03 §6–§7); the slice delivered in every role's brief and recorded (07 §4.1); the seven records, gates and caps (04 §6–§8), with `physa check` as a gate before the PR; shipping through the forge, GitHub first (push, PR, CI wait, merge, with the team's merge method); Physa's default harness (conduct, build, check, fix); asks; interventions counted from the first run (03 §8); rework recorded (03 §8a); team digests on `refs/notes/physa` and `physa sync` (07 §7).
- **The owner can:** bet on a small Physa idea and have it built, checked and landed with nobody pushing it.
- **Done when:** a small Physa bet lands end to end with no intervention.

### 7. The owner's harness, installed

- **Lands:** `physa harness install` and the harness settings review (04 §2.2); the owner's harness installed like anyone's.
- **The switch:** from here, Physa is built by the owner's harness running in Physa's engine, instead of as a plain Claude Code plugin.
- **The owner can:** compare rework and tokens between the default harness and his own on real bets.
- **Done when:** three Physa bets land through the owner's harness in Physa with no intervention.

### 8. Views

- **Lands:** Needs you in the CLI; watching a live session (`physa watch`, 03 §7.5); the single-user phone web served by the engine; the Rex plugin (`rex-plugin`).
- **The owner can:** keep work moving from his phone, and watch agents work in Rex panes.
- **Done when:** a day of Physa work is run from Needs you alone, notifications included.

### 9. Strength

- **Lands:** strength computed from the evidence recorded since step 5 (05 §6), shown as experimental; the strength settings file and `physa strength preview` (05 §6.5); decay proposals; strength in the index and slices; stakes worked out from it.
- **Also lands:** the network view (06 §3), as a bet that starts from the spike on `spike/network-view`: its drawing becomes the core package `internal/network` (577 lines today, under the 800-line budget), with the spike's snapshot tests and fixtures, built by the owner's harness. Alternative for the owner: the drawing needs only numbers, so it could be built earlier against fixture strengths, after step 5, for the phone web in step 8.
- **The owner can:** see which of Physa's ideas and capabilities carry flow, with the breakdown, and tune the settings against real history.
- **Done when:** the strengths on Physa and nebari match the owner's sense of them often enough to argue with, and preview explains every surprise.

### 10. Earned release

- **Lands:** `physa release serve`; flags per idea; the Ruby provider (OFREP evaluation plus Physa's tracking), then TypeScript and Go; product signals and results; guardrails and automatic rollback; experiments (05 §2–§5).
- **Tried on:** nebari, a real Rails app with real users.
- **The owner can:** land agent-written changes to nebari dark and release them as far as their evidence earns.
- **Release:** **v1.0, open source** (Q3): the map, the engine, harnesses, strength and earned release.
- **Done when:** a nebari idea is released step by step to everyone, or withdrawn, on its own evidence.

### 11. The hosted layer begins

- **Lands:** the Rails repo; the product-level map across Physa's two repos (the dogfood the two-repo split was chosen for); team Needs you; learning combined across people.
- **Done when:** the owner runs Physa and the hosted repo as one product from the hosted layer.

## What each step leaves out

So that no step grows: no views before step 8 beyond the CLI, no strength numbers before step 9 (evidence is still recorded from step 5), nothing next to production before step 10, nothing hosted before step 11.

## Open questions

- **Q1** Until step 7, the slimmed harness runs as a plain Claude Code plugin to build Physa. Rhiza is not involved.
- **Q2** The wedge (steps 1–4) comes before the engine (steps 5–7), as the vision's wedge decision implies.
- **Q3** Two public releases: v0.1 after step 4 (the wedge), v1.0 after step 10. Nothing is pushed publicly before v0.1, and the research notes with the owner's words are checked before any push.
- **Q4** nebari, with Rhiza removed from it first, is the brownfield test for init (step 4) and the first app for earned release (step 10); woodshop is the greenfield test (the owner).
- **Q5** Ruby is the first provider for earned release, because the owner's apps and the likely first teams are Rails.
- **Q6** The slimmed harness, built from afk and forge, becomes its own plugin repo, **dharness** (`~/code/dharness`, a fresh repo without afk's history; the owner): used directly in Claude Code from step 0, and installed into Physa at step 7.
