# Architecture 09: capability folders and traversal

Draft 0.2, 2026-11-21; Q1–Q10 settled. Uses `.physa/glossary.md` terms only. Builds on 01 and 08; would replace 01 §2, §3 and §8.2 where they differ. Items marked (Q) are open questions for the owner.

Learning makes capabilities grow. One run over five changes added about 30 lines to `init`, and the hard limit of 150 lines a file would fail the first busy capability's learn PR within weeks. This design turns each capability into a folder with an index, so that people and agents read a short overview and follow links to exactly what they need, instead of a slice cutting by size without judgement.

## 1. Settled with the owner (2026-11-21)

- **The limit protects both readers.** "I think it protects both. If we took a smart brevity approach to writing capabilities we can keep it succint and easy to read for both humans and agents and protect agent context at the same time."
- **An index, and traversal by links.** "Having an index allows agents to traverse it and get only what they need (slice can recommend a path, the agent can use the "directory" to find the right path, best of both worlds?). A human can do exactly the same. Read a brief overview, and through links click through to exactly the pieces they want to see."
- **Rules are always read in full.** "rules are important and should always be read in full."
- **Not one file per entry.** "I think one file per decision is probably a bit much. That will get overwhelming."
- **The agent extracts, guided by a skill, instead of the slice cutting.** "If something grows past an arbitrary size we are cutting things without any kind of reasoning. ... The agent is then armed with well defined indexes and structure that it can use to extract what it needs. And a "slice" skill can tell it how to do so efficiently."
- **`Capability:` names the whole capability**, never a part of one. The folder splits a capability by kind of entry, not by topic; a capability that covers two topics is two capabilities.
- **No links between capabilities.** Work on several capabilities is usually several bets: "If multiple capabilities are involved, I think we decided early on that it should probably be two bets. In cases where that cannot be avoided, the plan will clearly name that and it will be obvious to the agent that multiple capabilities will need to be sliced."
- **Bare slice returns the index and the rules**, "Maybe a pointer to the skill, or the skill itself."
- **A strict, enforced format.** "As long as there is some kind of clear demarcation and structure so an agent can grep/sed it and pull only that out. We need to decide on a format and enforce it."
- **The CLI knows the format; agents never learn it.** "Right, the CLI just KNOWS the format, the agent never has to learn it. It can use it to grab the decision headers, question headers. Then from that it can use the CLI again to just grab the entries it wants. Determistic retrieval." The CLI only navigates; which entries bear on a task stays the agent's judgment (`agent-chooses`).
- **Migration** is one PR, or two small ones, decided once the structure is.

## 2. The folder

```
.physa/
  README.md
  product/            (Q1)
    index.md
    rules.md
    decisions.md
    questions.md
  glossary.md
  capabilities/
    init/
      index.md        promise, stewards, a short overview, links
      rules.md        every rule; always sliced in full
      decisions.md    contents, then the decisions
      questions.md    contents, then the open questions
```

- **The folder name is the capability's slug**, as the file name is today. `Capability: init` is unchanged.
- **Every capability is a folder** (Q2), even one with two rules: one shape for the check, the skill and people. A kind with no entries has no file, and the index says so.
- **Entry formats are unchanged** (01 §3.1, §3.2): heading, meta line, body, `Because:`, `Rejected:`. Only where an entry lives changes.

## 3. The files

### 3.1 index.md

```markdown
---
promise: A team with an existing codebase gets a cited, reviewable map in minutes, without writing anything first.
stewards: [dphaener]
---

# Init

Init maps a repo that has no map: it gathers the sources, proposes the capabilities, recovers each one from its history and docs, and writes the map to a `physa/init` PR. A new codebase gets a short round of questions instead.

- [Rules](rules.md)
- [Decisions](decisions.md)
- [Questions](questions.md)
```

- **The overview is a few sentences** (Q3) in glossary terms: what the capability covers, so a reader can tell whether their task belongs here. It is the new thing a capability must have; today's promise line is too short to tell neighbours apart.
- **Who writes it**: init when it recovers the capability; learning proposes edits when entries change what it covers; stewards approve, as with any entry.
- **No counts** are written in the index; `physa map index` derives them, as it does today (`derived-index`).

### 3.2 rules.md, decisions.md and questions.md

Each starts with a contents section, then the entries:

```markdown
# Init · Decisions

## Contents

- [GitHub first](#github-first)
- [Changes before the map are exempt from the Capability trailer](#changes-before-the-map-are-exempt-from-the-capability-trailer)
- [The Session hook stays local](#the-session-hook-stays-local)

## Entries

### GitHub first
`github-first` · decided by dphaener, accepting a proposal · 2026-11-19 · from docs/architecture/02-init-and-learning.md
Init and the PR check read PRs and reviews through `gh`. Other hosts come later.
Because: one host done well before several done badly.
```

- **One contents line per entry**: its heading, linked to the heading's own anchor, which GitHub and most renderers make from the wording. In rules.md the heading is the rule itself, so the contents alone state every rule; the body behind the link holds the `Because:` and the anchors. Rule headings must therefore be the rule, stated briefly, not a topic (Q4).
- **The demarcation** (Q5): plain markdown, no extra markup. Under `## Entries`, every entry starts at its `###` heading, the meta line right after it starts with the slug, and the entry ends just before the next `###` or at the end of the file. `###` is used for nothing else, and no two headings in a file may be the same, so every heading's anchor is unique. One pattern cuts an entry out by its slug:

  ```
  awk '/^### /{h=$0;p=0;next} h&&/^`session-hook-local`/{print h;p=1} p' decisions.md
  ```

- **Rewording a heading changes its link.** The generated contents follow it automatically; only links from outside (a PR comment, another doc) break. The slug is the stable name agents and `--entry` use, so that is accepted.

- **The contents are generated, never hand-kept.** `physa check map` fails when a file's contents and its entries differ (an entry missing from the contents, a stale heading, a different order). A command rewrites them (Q6). Init and learning always write them.

## 4. Slicing

### 4.1 Bare slice

`physa map slice init` returns, in order:

1. **Header**: the capabilities chosen.
2. **Product rules**, in full (the floor, as today).
3. **For each capability**: index.md's promise and overview; rules.md in full; the contents of decisions.md and questions.md, each line with its slug. No decision or question bodies.
4. **Current state** (as today, 01 §8.2).
5. **Terms** that appear in the text above (as today).
6. **Reading further**: the slice skill (§5).
7. **How to anchor** (as today).

Each contents line is a heading and its slug:

```
Decisions (take with physa map slice --entry <slug>...)
- github-first: GitHub first
- pre-map-changes-exempt: Changes before the map are exempt from the Capability trailer
- session-hook-local: The Session hook stays local
```

The contents tell the agent what decisions and questions exist without loading them. It judges which its task touches and takes those with a second call.

### 4.2 Taking entries

```
physa map slice --entry <slug>...
```

prints exactly those entries, in full, from wherever they live in the map, and nothing else. Retrieval is deterministic: the slugs name the entries, so the same call always returns the same text. It costs no tokens, and it is written to the store like any slice, so Physa records exactly which entries each agent read (`record-observed`); session logs stay a supplement, for agents without the CLI.

The two calls are the whole traversal: the slice for the floor and the headings, `--entry` for the bodies. No other commands are added until slice records show agents reaching for one (`measure-before-budget`); a word search would drift towards matching, and following references between entries needs the slug-reference form 01 Q7 leaves open.

### 4.2a The format is the CLI's contract

The strict format (§3.2) exists so the CLI and people can rely on it; an agent using the CLI never parses it. `physa check map` enforces it, so the CLI can trust it and fails loudly, naming the file and line, if it ever meets a map the check would reject.

### 4.3 What changes in the slice contract

- **`--budget` and the cut order go** (Q7). The floor is the index, the rules and the contents; everything else is chosen by the agent, not cut by size.
- **`--with decisions|questions|all` stays**, for people and CI that want everything at once.
- **`deterministic-slice` holds**: the same request still gives the same slice. What the agent then reads is its own choice, and is recorded.
- **`never-cut` is reworded**: product rules, promises, overviews, rules and contents are always in a slice.
- **The 50 ms test** (`fifty-ms`) covers `--entry` too.

## 5. The slice skill

Embedded in Physa like its other skills and not replaceable by a team (`skills-not-replaceable`). The CLI does the navigating, so the skill teaches only the judgment: which entries to take, and what to do with them. Its draft:

> **Reading further**
>
> The rules above always apply; there are no more to fetch. Below them are the headings of this capability's decisions and questions. Take the ones your task touches, in one call: `physa map slice --entry <slug> <slug>...`.
>
> - **A decision bears on your task** if its heading names something you will change, choose between, or might undo. When in doubt, take it.
> - **Read each `Rejected:` list.** Those options were tried or argued and turned down; don't bring one back without saying why.
> - **An open question your task would answer**: don't settle it silently. Ask, or say in your PR how you answered it.
> - **Slice another capability only when your plan names it.**
> - **If your task would break a rule or reverse a decision**, stop and ask.
>
> Name the capability in your commit's `Capability:` trailer.

- **Delivery** (Q8): about 200 tokens, printed in every slice as its "Reading further" section, so nothing needs installing and every agent has it at the moment it chooses what to read.
- **It is tested like the other skills**: a fixture task and map, and a check that the agent took the decisions its task touches and no others.
- **Without the CLI**: `.physa/README.md` carries the same judgment, plus what the CLI otherwise hides: the folder layout, the contents lists, and how to cut one entry out by its slug (§3.2).

## 6. Size limits

| What | Limit | Why |
|---|---|---|
| index.md | about 30 lines (Q9) | always sliced |
| rules.md | about 150 lines | always sliced in full |
| decisions.md, questions.md | none (Q9) | read through contents and `--entry`; a person browses by links |
| product | the same per file | |

Over a limit still fails the check, with a message that proposes a merge, a prune, or a split into two capabilities. 01 §3.3's lifecycle (superseded decisions become `Rejected:` items, decayed entries are removed) is what keeps decisions.md from growing without end; learning proposes those prunes, on signals, as it proposes entries.

## 7. Who writes the map

- **Init** writes the folders directly (02 §2.6).
- **Learning** writes each proposal into the right file and regenerates the contents (08 §6). The learn PR's diff then shows the new entry and its contents line, never a moved file.
- **People** edit any file by hand; the check tells them when the contents are stale and the command fixes them.

## 8. Migration

Physa's own 13 capabilities and product.md are the only map in use (woodshop's branch was deleted; nebari's init has not been written). So no migration command: the PR that changes the format converts Physa's map with a one-off program it does not keep, and `physa check map` on the result is the test. Updates in the same PR, or a second small one (Q10):

- 01 §2, §3, §8.2 and §9; the capability `slices` (the reworded `never-cut`, `deterministic-slice` unchanged, `measure-before-budget` noted); README text (01 §6).
- `internal/mapfile`, `internal/mapcheck`, `internal/mapslice`, `internal/mapindex`, init's writer, learning's proposer.

## 9. Settled with the owner (2026-11-21)

The owner accepted every recommendation below ("yes").

- **Q1** Does product.md become a folder too? Settled: yes. It is at 97 of its 100 lines, and the same shape everywhere is simpler for the check, the skill and people.
- **Q2** Is every capability a folder, even a small one? Settled: yes, one shape.
- **Q3** Is the overview required, and how long? Settled: required, three sentences at most, enforced through index.md's limit.
- **Q4** Must a rule's heading be the rule itself? Settled: yes, so the rules' contents state every rule. The check can't judge it; the mapping and learning skills write them that way, and review catches the rest.
- **Q5** The demarcation: plain headings, the meta line's slug, and the check's ban on `###` for anything else and on duplicate headings in a file. Settled (the owner asked whether a tag is needed; it isn't). The cost: links from outside the map break when a heading is reworded. The alternative is an `<a id="slug"></a>` line before each heading, for links that never change.
- **Q6** Which command rewrites the contents? Settled: `physa map fmt`, which also normalises spacing. The alternative is `physa check map --fix`.
- **Q7** Remove `--budget`? Settled: yes. Keeping it means keeping the cut order nobody chose (the owner: "cutting things without any kind of reasoning").
- **Q8** The skill in every slice, or a pointer? Settled: in every slice. Now that the CLI does the navigating it is about 200 tokens, and it arrives exactly when the agent decides what to read.
- **Q9** The limits: index.md about 30 lines, rules.md about 150, decisions and questions unlimited. Settled, tuned later from slice records (`measure-before-budget`).
- **Q10** One PR or two: format, check and migration first, then slice, `--entry` and the skill? Settled: two, so the map is converted and checked before anything reads it the new way.

## 10. Built in the first PR

The first of the two PRs (Q10) landed the folder format, `physa check map`, `physa map fmt` and Physa's own map in folders. Calls made while building it, closed by Claude as arbiter (2026-11-21), for the owner to review:

- **The anchor algorithm** is GitHub's, for the headings Physa writes: lowercase, every character other than a letter, a digit, a space, a hyphen or an underscore dropped, each space turned into a hyphen. Combining marks are dropped, which GitHub keeps; no heading in Physa's map has one. The check rejects `[`, `]`, `<`, `>` and `*` in a heading, and two headings in a file with the same anchor, the title and the two sections included.
- **The overview's length** is held by index.md's 30-line limit and nothing else. There is no sentence counter; "three sentences at most" (Q3) is the guidance the skills give.
- **An old layout is reported, not read.** A `.physa/product.md` or a `.physa/capabilities/<slug>.md` is a problem naming the file, with a message saying to convert it; `physa map fmt` stops with the same message and changes nothing. Physa keeps no converter.
- **History and init read both layouts.** The pre-map exemption for `Capability:` (07) and init's refusal of a repo that already has a map each accept `.physa/product.md` and `.physa/product/index.md`; a slug is found in a capability's folder or, in an old commit, its single file.
- **The recovering skill still writes one block**, a capability as one text, and Physa splits it into the folder's files. The recovered entries stay byte for byte; without an overview the promise stands in as the overview, until a person or learning edits it. An overview that breaks a limit is dropped at settle, like an entry.
- **The slice's `file` names the folder**, with a trailing slash, in both the markdown and the JSON.
- **`physa map fmt` regenerates the contents and also index.md's links**, so they follow the kind files on disk in index order, and it normalises spacing. It never deletes a file and never makes one; a file outside the format is reported with its line and left alone. `--check` changes nothing and exits 1 when fmt would.
- **The `file-per-capability` rule is reworded**, the one entry of the map changed: "One folder per capability", from this document, its slug kept. It stated a layout this design replaced.
- **Init and learning write folders.** Init drops, with a reason in the PR body, an entry the check would reject (an empty heading, a rejected character, a repeated anchor) and a capability whose text does not split, which then holds its promise only. Learning appends each proposal to its kind file, regenerates the contents with the code `physa map fmt` uses, and drops a proposal whose file is already outside the format.

## 11. Built in the second PR

The second PR (Q10) landed the slice that returns the floor and the headings, `physa map slice --entry`, the slice skill and the removal of `--budget` (§4, §5). Calls made while building it, closed by Claude as arbiter (2026-11-21), for the owner to review:

- **The slice checks form only.** Before it slices, the slice runs the map check with anchors left unresolved: no commit or PR is looked up, so it never opens the repository or calls the forge. A map the check would reject for form fails the slice loudly, naming the file and line (§4.2a); one whose anchors point nowhere still slices. The map is read once, by the check, and the slice uses what it read.
- **`--entry` takes slugs as bare words** after it (`--entry a b c`), or `--entry=a`, repeated if wanted. It cannot be mixed with capability names or `--with`, and it needs at least one slug. It prints exactly those entries in full, from the product or any capability, in the order named, once each; a slug named twice is taken at its first place. Any unknown slug fails with exit 2, names every unknown slug and prints nothing. `--format json` gives `{"entries": [...]}`, each entry with its capability (`product` for the product's).
- **The product's headings** are in a bare slice like a capability's: its promise, its rules in full and the headings of its decisions and questions. Its overview is not shown. `--entry` takes the product's entries by slug like any other.
- **The store migration** (`0007_slice_entries.sql`) adds an `entries` column to slice records and drops `budget`. A record's line shows the entries taken, with or without capabilities. A real copy of the old store migrated with every row kept.
- **The skill prints in the learning drafter's slice too**, because the drafter reads the same slice as any agent and chooses from the same headings.
- **The README is an embedded file.** Its text moved from Go source to `internal/mapfolder/readme.md`, embedded in the binary; `.physa/README.md` is a byte copy, and a test keeps init's copy and Physa's own the same.
- **The latency test takes a median.** `fifty-ms` is tested on the whole slice path, check included, over a generated map of 100 capabilities: every case is warmed once, then run in seven rounds across all cases, with the heap collected before each run, and the median of the seven must be under 50 ms. This holds when other builds load the machine, which a single timing did not.

All of the design's questions are settled and built. What is left is §5's tests of the skill with a fixture task, and the choice of which slices to measure from slice records (`measure-before-budget`).
