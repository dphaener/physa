# Physa: the guide for your agent

Physa keeps a product's knowledge, its map, in the repo and keeps work anchored to it. This guide says when Physa applies, what its words mean, and what to do for the tasks a person gives you. Follow it before you change code, and again before you say the work is done.

## When it applies

Physa applies in a repo with a `.physa/` folder. That folder holds the map. Read it through the commands below, not by walking the folder.

A repo with no `.physa/` folder has no map yet. A person builds one with `physa init`; this guide says nothing more about it. Work in that repo as you would without Physa.

## Physa's terms

Use these words as Physa means them, in conversation, commits, PR descriptions and docs. The words after _Avoid_ mean something else, so leave them out.

- **Map**: The product's knowledge, kept in the repo. It says why the product is the way it is, never what the code does. _Avoid_: model, graph, wiki.
- **Capability**: A promise the product makes to its users, kept by many changes over time. _Avoid_: feature, module, epic.
- **Rule**: Something that must always or never be true, with its reason. _Avoid_: constraint, guideline, convention.
- **Decision**: A hard-to-reverse choice with its reason and the options rejected.
- **Question**: An undecided matter that shapes upcoming work. _Avoid_: open item, TBD.
- **Term**: A word with one agreed meaning, defined in the glossary at `.physa/glossary.md`.
- **Anchor**: The link from a map entry to the change that made it real; new changes carry it as a commit trailer.
- **Index**: Every capability with its one-line promise; read it to choose what to load. _Avoid_: catalog.
- **Slice**: The part of the map assembled for one task: chosen capabilities with their rules, decisions, questions and terms. _Avoid_: context pack, context dump.
- **Skill**: A guide Physa ships in its binary to teach an agent its part; this document is the one for you. _Avoid_: plugin, prompt pack.
- **Idea**: A person's intent to try something with the product, owned by the person who adds it. _Avoid_: ticket, backlog item, story, probe.
- **Pursuit**: A person's decision to build an idea, with their name on its signal. Only people pursue. _Avoid_: bet, sprint, commitment.
- **Signal**: What an idea expects to see if it is right: a claim, a direction, a window and a resolver. _Avoid_: KPI, success metric.
- **Appetite**: How many tokens a person will spend on a pursuit; a budget, never an estimate. _Avoid_: estimate, points, story points, time box.
- **Change**: One small, atomic change to the product, usually one commit in its own PR, anchored to exactly one capability. _Avoid_: change set, ticket, leaf, work item.
- **Acceptance**: The runnable checks a planned change must pass, with a failure case; the contract you are judged against. _Avoid_: acceptance criteria, definition of done, DoD.
- **Finding**: A problem a check reports: which check, at what level, what is wrong. _Avoid_: issue, comment, defect.
- **Call**: A small choice a run made on a person's behalf to keep going, with its reason, left for that person to check. _Avoid_: assumption, judgment call.

The glossary has the rest. Read `.physa/glossary.md` when a word in the map is new to you.

## Before changing code

1. Run `physa map index`. It lists every capability with its promise.
2. Choose the capabilities your task touches and run `physa map slice <capability>`, naming them all in one call.
3. The slice ends with its own "Reading further" section. That section is the slice skill: it says which entries to take in full and what to do with them. Follow it as written; it is not repeated here.

## A change that seems to need two capabilities

A change is anchored to exactly one capability. If yours seems to need two, either split it into two changes, one per capability, or ask the person which way to go. Do not name two capabilities in one commit.

## Committing

Write the commit message in the repository's or team's own convention. Physa adds one trailer, whatever the convention:

```
Capability: <capability>
```

It names the one capability the change belongs to, by its folder name in `.physa/capabilities`. That trailer is the anchor.

Hooks add the rest. A person or you run `physa map hooks` once per clone, and after that every commit carries a `Session:` trailer. After `physa pursuit work` has named the change you are on, commits also carry a `Change:` trailer. Do not write either by hand.

## Working a pursuit's planned change

1. `physa pursuit plan show <pursuit>` prints the pursuit's plan: its changes, in order, each with its acceptance.
2. `physa pursuit work <pursuit>/<change>` says which change this session works on, so commits carry it. `physa pursuit work --current` prints the one this session already has.
3. The change's acceptance is your contract. Make each check pass, including the failure case, and do nothing outside the change.

Only a person pursues an idea (`physa pursue`) or stops a pursuit (`physa pursuit stop`). Both are refused inside an agent's session. When the work calls for one of them, ask the person.

If you must choose something to keep going and cannot wait for an answer, make the choice, say why, and leave it as a call for the person to check.

## Adding an idea

`physa idea add "<text>"` records an idea in the words the person gave. It belongs to the person who adds it. `physa idea list` lists ideas and `physa idea show <slug>` shows one with its facts. You may draft a signal for an idea, but you do not pursue it.

## Reviewing learning's proposals

`physa map learn` reads the merged changes and their sessions, and proposes changes to the map on a `physa/learn` branch, or in a PR from it. To review one:

1. Check that each proposal cites its source, and that the source says what the proposal claims.
2. Run `physa check map` on the branch.
3. Leave the choice to a person: each proposal is accepted, edited or dropped by them. Nothing merges itself.

## Checking the work

Before you say the work is done, run `physa check`. It exits 0 when the map and the changes are sound, 1 when there are findings to fix, and 2 when it cannot read what it checks. Fix every finding, then run it again. `physa check changes <range>` checks the changes of a range, for example the commits of your branch, against the map.

## Finding more

- `physa help` lists the commands, and `--help` on any command prints its usage, for example `physa pursuit --help`.
- Without the CLI, read `.physa/README.md`.
- The docs in the repo (`docs/`) hold the product's vision and its settled designs.
