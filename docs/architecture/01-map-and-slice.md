# Architecture 01: the map format and the slice contract

Draft 0.1, 2026-11-19. Uses `.physa/glossary.md` terms only. Items marked (Q) are open questions for the owner.

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
  product.md                the product's promise, product-wide rules, decisions, questions
  glossary.md               terms
  capabilities/
    delivery.md             one file per capability: its promise, rules, decisions, questions
    billing.md
    ...
```

- **One file per capability** (Q2). A capability's rules, decisions and questions live in its file. A person reads one page to understand one area; a slice for a capability is mostly that file. Two PRs touching different capabilities never conflict; two touching the same one usually change different entries, which git merges.
- **product.md** holds what belongs to no single capability: the product's own promise, product-wide rules (for example "never log customer emails"), product-level decisions, and questions.
- **The index is not stored.** `physa index` derives it from the capability files. Storing it would duplicate every promise.
- **Nothing else is in the repo.** Strength, slice records, usage and decay timers live in the local store (and the hosted layer).

## 3. A capability file

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

## 5. product.md

Same entry formats as a capability file, with the product's own promise in the front matter. Its rules are included in every slice (they are the always-loaded floor). Its decisions and questions are included when the slice asks for them or when the budget allows.

## 6. README.md

Fixed text written by `physa init`, the fallback for agents without the CLI:

> This is the product's map. Read product.md first. Each file in capabilities/ is one promise the product makes, with its rules and decisions. Read the ones related to your task. Anchor commits with `Capability: <file name>`.

## 7. The AGENTS.md pointer

Added by `physa init` between markers, and nothing else:

```markdown
<!-- physa -->
Before changing code, run `physa index`, then `physa slice <capabilities>` for the ones your task touches.
No CLI? Read .physa/README.md.
<!-- /physa -->
```

## 8. Commands

### 8.1 `physa index`

```
$ physa index
delivery   Webhooks reach customers at least once, in order per endpoint.    3 rules · 2 decisions · 1 question
billing    Customers are charged exactly once per period, in their currency.   5 rules · 4 decisions
...
```

One line per capability, sorted by name. With local data available it adds strength and a marker for capabilities with growing ideas or recent rollbacks. Target: under ~1,500 tokens for about 50 capabilities.

### 8.2 `physa slice`

```
physa slice <capability>... [--with decisions|questions|all] [--budget <tokens>] [--format md|json]
physa slice --change <bet>     (for planned work: the capabilities were chosen when it was shaped)
```

Output sections, always in this order:

1. **Header**: the capabilities chosen and the budget used.
2. **Product rules** from product.md. Always included.
3. **For each capability**: its promise, then its rules, decisions and questions.
4. **Current state** (from the local store and flag service, not the repo): live flags and ideas growing on these capabilities, recent rollbacks and reverts, guardrails. Marked as a snapshot with its time.
5. **Terms** that appear in the text above.
6. **Left out**: entries cut by the budget, each with the command that shows it.
7. **How to anchor**: the trailers to use for this work.

**Budget order** when the slice is too big, cut from the bottom first: rejected options, then questions, then decisions' `Because:` lines, then decisions, then current state. Product rules, promises and rules are never cut; if they alone exceed the budget, the map is over its size budget and `physa check` says so.

**Determinism**: sections 1–3 and 5–7 depend only on the map and the request. Section 4 is a dated snapshot.

**Recording**: every slice is written to the local store (who asked, which entries, when) so that the PR check can measure misses and excess later.

**JSON**: the same content as structured data, used by harnesses and by the hosted layer.

### 8.3 `physa check`

Runs locally and in CI. It fails on:

- a malformed file or meta line;
- a duplicate slug, or a reference to a slug that does not exist;
- a rule or decision without an anchor or a `Because:` line;
- an anchor that does not resolve to a commit or PR in this repo;
- a capability file or the whole map over its size budget.

## 9. Size budgets (Q5)

| What | Default budget |
|---|---|
| A capability file | about 150 lines |
| product.md | about 100 lines |
| Capabilities in the index | about 50; beyond that, propose merging |
| A slice | about 3,000 tokens |

Teams can change them. Going over a budget never blocks work; it produces a proposed merge or prune on the next PR.

## 9a. Read path and speed

Lesson from v1: its model was an append-only event log (about 27,000 entries, 127 MB) replayed on every read; `rhiza check` took 15.3 s, and a SQLite projection (milestone 39, #603, #606) had to be added afterwards.

- **The map's files are its current state.** Reading the map never replays history. Superseded entries are edited in place; git is the archive.
- **What really aggregates history uses the engine's SQLite store from day one**: strength and decay (from slice records, PR outcomes, signal events and rollbacks), the slice's current-state section, and anchor checks against git.
- **A latency budget is a test from day one**: `physa index` and `physa slice` answer in under 50 ms on a generated map at twice the default budget (100 capabilities).
- **No cache until that test fails.** If it does, the cache is keyed by file content hashes and rebuilt automatically, never a second source of truth.

## 10. Settled with the owner (2026-11-19)

- **Q1** Hidden `.physa/` directory.
- **Q2** One file per capability, with its rules, decisions and questions inside.
- **Q3** The one-line meta format with a stable slug.
- **Q4** Superseded and reversed decisions become `Rejected:` items on their replacement; git is the archive.
- **Q5** Default budgets as in §9, changeable by the team.
- **Q6** Ideas, pitches and bets stay out of the map: they live in the engine's store locally and in the hosted layer. When a pitch is bet on, its rabbit holes and no-gos move into the map as rules or rejected options.
- The read-path rule and latency test in §9a.
