# Architecture 01: the map format and the slice contract

Draft 0.1, 2026-11-19. Uses `.physa/glossary.md` terms only. Items marked (Q) are open questions for the owner. Capabilities and the product became folders in 09; §2, §3, §5, §6 and §9 say so and point there, and the entry formats in §3 are unchanged.

## 1. Constraints this design must meet

From the vision:

- Small footprint: a 2–3 line pointer in the team's AGENTS.md, a small map, nothing generated in bulk.
- Readable by people and reviewable in PRs, without the CLI.
- Holds why, never what: no file paths, no description of code.
- Anchored to changes (commit trailers and PR numbers), never to files.
- Agents choose; the CLI assembles. No matching, no ranking, no embeddings.
- Deterministic: the same map and the same request give the same slice.
- Many agents work in parallel, so concurrent PRs must rarely conflict on the map.
- Strength, usage and decay timers are churn: they never live in the repo.

## 2. On disk

```
.physa/                     (Q1: hidden directory)
  README.md                 how to read the map without the CLI
  product/                  the product's promise, product-wide rules, decisions, questions
  glossary.md               terms
  capabilities/
    delivery/               one folder per capability (09 §2)
      index.md              its promise, a short overview, links
      rules.md              every rule
      decisions.md          contents, then the decisions
      questions.md          contents, then the open questions
    billing/
    ...
```

- **One folder per capability** (Q2, reshaped by 09). A capability's rules, decisions and questions live in its folder, split by kind; a person reads the index, then follows links. Two PRs touching different capabilities never conflict; two touching the same one usually change different entries, which git merges.
- **product/** holds what belongs to no single capability: the product's own promise, product-wide rules (for example "never log customer emails"), product-level decisions, and questions. It has the same files as a capability.
- **The index is not stored.** `physa map index` derives it from the capability folders. Storing it would duplicate every promise.
- **Nothing else is in the repo.** Strength, slice records, usage and decay timers live in the local store (and the hosted layer).

## 3. A capability

The entry formats below are unchanged. Since 09 the text lives in a folder: the front matter and title in `index.md`, and each `## Rules`, `## Decisions` and `## Questions` section in its own file under a contents list (09 §3). The example is the capability as one text, the shape init's recovery writes and Physa splits.

````markdown
---
promise: Webhooks reach customers at least once, in order per endpoint.
stewards: [dana]
---

# Delivery

## Rules

### Retries go through the queue
`queue-retries` · #1290
Retry only by enqueueing a job, never from an in-process loop.
Because: a deploy dropped about 3,000 in-flight retries.

## Decisions

### Six delivery attempts
`retry-limit` · decided by dana · 2026-03-02 · #1102 #1140
Six attempts with exponential backoff, then dead-letter.
Because: support needs a bounded window, and endpoints are rarely down longer than a weekend.
Rejected:
- Unlimited retries: floods endpoints that stay down for days.
- Three attempts: too short for weekend outages (tried in #980, reverted in #1012).

## Questions

### Should ordering be per customer instead of per endpoint?
`ordering-scope` · raised by sam · 2026-11-01
````

### 3.1 The meta line (Q3)

Every entry starts with a heading, then one meta line in backticks-and-dots form:

```
`slug` · decided by <person> · <date> · <anchors>
```

- **Slug**: the entry's stable name, lowercase with hyphens, unique across the whole map. It is what trailers and other entries use. Headings can be reworded freely; the slug stays.
- **Anchors**: PR numbers (`#1102`) or short commit hashes. Every rule and decision needs at least one, except entries recovered at setup from sources that are not changes (those cite the doc instead: `from CONTRIBUTING.md`).
- **People and dates** are only on decisions and questions, where they matter.
- **How a decision was reached** follows the person, one of three: `in their words` (the person stated it), `choosing an option` (the person picked from options offered), `accepting a proposal` (the person agreed to an agent's or someone else's recommendation). Example: `` `openfeature` · decided by dphaener, accepting a proposal · 2026-11-19 · #42 ``. It keeps rubber-stamped agreement visible: in Rhiza, the recommended option was accepted 88% of the time.
- One line keeps entries scannable on GitHub and parseable without a schema language.

### 3.2 Entry kinds

| Kind | Body | Required |
|---|---|---|
| Rule | What must always or never be true. | `Because:` line, at least one anchor |
| Decision | What was chosen. | `Because:`, who, date, anchors; `Rejected:` list when there were options |
| Question | The question itself; optional context. | who, date |

### 3.3 Lifecycle inside the file (Q4)

- **Superseded decision**: the old entry is removed and becomes a `Rejected:` item on the decision that replaced it, with its anchor. History stays where it is useful, and git keeps the rest.
- **Reversed decision**: same, with the rollback's anchor.
- **Withered idea**: recorded as a decision with the idea as a rejected option.
- **Answered question**: becomes a decision or a rule, or is removed.
- **Decayed entry**: removed through a proposed change on a PR. Git history is the archive.

## 4. glossary.md

Pocock format, one entry per term:

```markdown
**Endpoint**: A customer's URL that receives webhooks.
_Avoid_: hook URL, target, destination
```

The glossary exists so agents speak to people in concrete terms. It guides agents and is never enforced: no check flags people's words. It changes only when the team means to change it; nothing learned from PRs edits it.

Terms are product-wide. A slice includes the terms whose name appears in the text it assembled (a plain word match on the term and its aliases: deterministic, not semantic).

## 5. product/

The same folder and entry formats as a capability (09 §2, §3), with the product's own promise in the front matter. Its rules are included in every slice (they are the always-loaded floor). Its decisions and questions are included when the slice asks for them or when the budget allows.

## 6. README.md

Fixed text written by `physa init`, the fallback for agents without the CLI:

See `.physa/README.md` for the text; 09 §5 says what it carries. It tells an agent to read `product/` first, to read each capability's `rules.md` in full and its decisions and questions through their contents, how to cut one entry out by its slug, and to anchor commits with `Capability: <folder name>`. A test keeps init's copy the same as Physa's own.

## 7. The AGENTS.md pointer

Added by `physa init` between markers, and nothing else:

```markdown
<!-- physa -->
Before changing code, run `physa map index`, then `physa map slice <capabilities>` for the ones your task touches.
No CLI? Read .physa/README.md.
<!-- /physa -->
```

## 8. Commands

Settled with the owner on 2026-11-20 (`grouped-cli`): "Rather than having 100 commands, we should have grouped commands that have sub commands." Four principles:

1. Groups are glossary nouns and subcommands are verbs.
2. Very few top-level commands: only what a person or agent runs constantly or once per repo.
3. A bare group does its most common job.
4. One way to do each thing, with no aliases.

The full planned tree. `check`, `map` and `physa init`, with its gather, propose and recover phases, are built today; everything else is planned.

- `physa init` (02), built: a bare run gathers, proposes, recovers and checks under one appetite; `--write` commits the map to `physa/init`, `--push` pushes it, `--pr` opens or refreshes its PR, `--hooks` installs the hook, and `--answers` gives a new codebase's answers. Phases alone: `--gather`, `--propose`, `--recover`. A map may have no `capabilities/` directory yet, as a new codebase's map has none (02 §2.8).
- `physa check [map | changes [range]]` (07 §5), built. Bare `check` runs the map check and, when a range is given or `origin/main..HEAD` resolves, the change check on it.
- `physa map [index | slice | changes | records | learn | hooks]` (01, 07 §4.1), built. Bare `map` prints the index.
- `physa map learn` (08), built (signals, drafting and the physa/learn branch with --write, --push and --pr); it also reads session logs (08 §5).
- `physa map hooks` (08 §5), built, in any repo: a repo that ships `tools/hooks/prepare-commit-msg` points `core.hooksPath` at `tools/hooks`, as Physa does. Any other repo gets Physa's embedded hook written to `.git/hooks/prepare-commit-msg` when `core.hooksPath` is unset and no such hook exists, leaving `core.hooksPath` alone; otherwise it refuses with exit 2 and says how to add the hook by hand. Either way commits carry `Session:`.
- `physa sync` (07 §7), planned, after the engine.
- `physa engine start|status|stop` (03), planned.
- `physa run <bet>` and `physa watch` (03 §7.5), planned, top-level like `check`.
- `physa ask ...` (03 §9), planned.
- `physa harness install|show|settings` (04 §2), planned.
- `physa bet plan ...` (03 §10a), planned; a plan is set with `physa bet plan set`.
- `physa strength` and `physa strength preview` (05 §6.5), planned.
- `physa release serve|flags ...` (05), planned and opt-in; it replaces `physa serve`.
- `physa version` and `physa help`; `--help` on every group and subcommand.

### 8.1 `physa map index`

```
$ physa map index
delivery   Webhooks reach customers at least once, in order per endpoint.    3 rules · 2 decisions · 1 question
billing    Customers are charged exactly once per period, in their currency.   5 rules · 4 decisions
...
```

One line per capability, sorted by name. With local data available it adds strength and a marker for capabilities with growing ideas or recent rollbacks. Measured cost: about 140 bytes a line.

### 8.2 `physa map slice`

```
physa map slice <capability>... [--with decisions|questions|all] [--budget <tokens>] [--format md|json]
physa map slice --change <bet>     (for planned work: the capabilities were chosen when it was shaped)
```

Output sections, always in this order:

1. **Header**: the capabilities chosen and the budget used.
2. **Product rules** from product/rules.md. Always included.
3. **For each capability**: its promise, then its rules, decisions and questions.
4. **Current state** (from the local store and flag service, not the repo): live flags and ideas growing on these capabilities, recent rollbacks and reverts, guardrails. Marked as a snapshot with its time.
5. **Terms** that appear in the text above.
6. **Left out**: entries cut by the budget, each with the command that shows it.
7. **How to anchor**: the trailers to use for this work.

**Budget**: a slice has no default budget. Without `--budget` it holds everything for the requested capabilities, cuts nothing and has no Left out section.

**Cut order** when `--budget` is given and the slice is too big, the pieces lowest in the output first within each step:

1. product/'s decisions and questions;
2. rejected options;
3. terms, whole entries;
4. questions;
5. decisions' `Because:` lines;
6. decisions.

Product rules, promises and the capabilities' rules are never cut, nor decisions or questions the request asked for with `--with`. If those alone exceed the budget, the slice says so and names what they hold. Terms count only when they appear in the requested capabilities' own text, never in product/'s.

**Determinism**: sections 1–3 and 5–7 depend only on the map and the request. Section 4 is a dated snapshot.

**Recording**: every slice is written to the local store (who asked, which entries, when) so that the PR check can measure misses and excess later.

**JSON**: the same content as structured data, used by harnesses and by the hosted layer.

### 8.3 `physa check map`

Runs locally and in CI. It fails on:

- a malformed file or meta line;
- a duplicate slug, or a reference to a slug that does not exist;
- a rule or decision without an anchor or a `Because:` line;
- an anchor that does not resolve to a commit or PR in this repo;
- a file of a capability or of product/ over its size budget (09 §6).

Settled with the owner (2026-11-20):

- A `from <path>` anchor must name a file in the repo.
- Commit-hash anchors resolve against the local repo: an anchor naming no commit fails, as does one in a directory that is not a git repository. A shallow clone is named in the message, since its history may be missing the commit.
- `#N` PR anchors resolve against GitHub through `gh`; an anchor naming no PR fails. When `gh` is missing, unauthenticated or unreachable, `physa check map` says so and checks them for form only, never failing on that alone.
- Over a size budget fails the check, with a message that proposes a merge or a prune. The §9 defaults are read as hard limits: `rules.md` over 150 lines, `index.md` over 30 (09 §6). (The limit of more than 50 capability files was dropped the same day: `measure-before-budget`.)

**Slug references** (Q7): a reference from one entry to another needs a written form before `physa check map` can find references to slugs that do not exist. Until it has one, the check tests only slug shape and uniqueness.

## 9. Size budgets (Q5)

Replaced by 09 §6: `index.md` about 30 lines, `rules.md` about 150, `decisions.md` and `questions.md` none, the same for product/. The single-file budgets this section first set (a capability file about 150 lines, product.md about 100) no longer apply.

Slices have no default budget: an agent passes `--budget` when it wants one. There is no limit on how many capabilities a map holds; budgets get tuned from run data (`measure-before-budget` in .physa/capabilities/slices.md).

Teams can change them. Going over a budget never blocks work; it produces a proposed merge or prune on the next PR.

## 9a. Read path and speed

Lesson from v1: its model was an append-only event log (about 27,000 entries, 127 MB) replayed on every read; `rhiza check` took 15.3 s, and a SQLite projection (milestone 39, #603, #606) had to be added afterwards.

- **The map's files are its current state.** Reading the map never replays history. Superseded entries are edited in place; git is the archive.
- **What really aggregates history uses the engine's SQLite store from day one**: strength and decay (from slice records, PR outcomes, signal events and rollbacks), the slice's current-state section, and anchor checks against git.
- **A latency budget is a test from day one**: `physa map index` and `physa map slice` answer in under 50 ms on a generated map of 100 capabilities.
- **No cache until that test fails.** If it does, the cache is keyed by file content hashes and rebuilt automatically, never a second source of truth.

## 10. Settled with the owner (2026-11-19)

- **Q1** Hidden `.physa/` directory.
- **Q2** One file per capability, with its rules, decisions and questions inside.
- **Q3** The one-line meta format with a stable slug.
- **Q4** Superseded and reversed decisions become `Rejected:` items on their replacement; git is the archive.
- **Q5** Default budgets as in §9, changeable by the team.
- **Q6** Ideas, pitches and bets stay out of the map: they live in the engine's store locally and in the hosted layer. When a pitch is bet on, its rabbit holes and no-gos move into the map as rules or rejected options.
- The read-path rule and latency test in §9a.
