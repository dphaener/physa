# Architecture 15: code-first init

Draft 0.1, 2026-11-26; proposed by Myxo for the owner to review, from five runs on three public repos. Settled so far, in the owner's words below: capabilities come from the code and the PRs give the why; a capability is cut by promise, across layers; areas are init's own scaffolding; init is capped, not estimated. The rest is open (§10). Uses `.physa/glossary.md` terms only. It replaces 02 §2.3 (propose), §2.4's sources, §2.7 (the estimate) and the coverage placement built on them; gather (§2.2), check (§2.5) and review as a PR (§2.6) stay.

## 1. Why

Init proposed capabilities from PRs: one session read every gathered PR and change, in shares on a large repo, and named the capabilities they showed. On Rails it found 50. The owner: "I would also expect there to be well over 2-300 capabilities. its a huge framework. 50 capabilities is extremely low." And: "i don't know that deriving capabilities from prs makes sense. you can map rules and decisions and questions to capabilities with prs, but prs don't necessarily define capabilities. I think the codebase is the best place to find that." Then: "The code defines WHAT it does, and the PRs give the WHY", and "a codebase that is well documented and tested gives you a lot of why."

The owner also asked for less code in init ("the init code seems to be getting complex and bloated") and for no estimate on a legacy codebase: "I'm more in favor of a cap than an estimate."

## 2. The runs

All with one model, no estimate, a hard cap of $150, recovery in a seeded random order, local only. Reports in the owner's run folders.

| | Rails | Discourse | Mastodon 1 | Mastodon 2 |
|---|---|---|---|---|
| source files | 1,855 | 10,539 | 2,568 | 2,568 |
| cut by | framework | framework | framework | **promise** |
| capabilities | 550 | 2,335 | 435 | **290** |
| files in a capability | 90% | 94% | 93% | 87% |
| PRs gathered · placed | 1,001 · 92% | 1,012 · 56% | 1,052 · 50% | 3,003 · 49% |
| PRs in one capability | 73% | 39% | 57% | **63%** |
| recovered | all | 142, a sample | all | all |
| rules · questions | 4,184 · 1,469 | sample | 1,732 · 1,579 | 752 · 1,601 |
| rules citing a PR or change | 28% | 34% | 22% | **97%** |
| `physa check map` | ok | ok | ok | ok |
| cost | $136.69 | $147.65, capped | $82.73 | $70.88 |

A blind judge graded 40 entries of the Rails map against the PR-first map's: as often backed by their sources (32 of 40 each), but a reason given in 24 of 40 against 39 of 40, because rules taken from tests and comments say what the code does, not why. Run 2's rule for that (§6) lifted PR-backed rules to 97% and left the map mostly questions, which §6 now narrows.

## 3. The phases

```
gather ─▶ areas ─▶ walk each area ─▶ place ─▶ recover each capability ─▶ check ─▶ review as a PR
 (02 §2.2)  1 session   1 session per area   no session  1 session per capability   (02 §2.5)  (02 §2.6, 12)
```

1. **Gather**, as today (02 §2.2), with a larger PR window (§7).
2. **Areas.** One session reads the list of every source file and divides the repo into areas, each a group of promises with the files that serve them, by promise and across layers. Every source file falls in exactly one area. An area is init's own scaffolding: it never appears in the map and needs no glossary term (owner: "area is a temporary grouping that drops off after we're done initializing").
3. **Walk.** One session per area, with read-only tools over the area's code, tests and docs, names its capabilities, each a promise with a footprint: the source files it owns, and the tests and docs that bear on it. Every source file of the area is owned by one capability or named shared with a reason; none is left unclaimed. A session may split its area once into smaller ones, each walked on its own.
4. **Place.** No session. Each gathered PR and change goes to the capabilities that own most of its files: a source file counts whole, a test or doc shared by k capabilities counts 1/k, and the largest are taken until half the weight is covered, at most three. Placement is a fact of the footprint, not a judgment.
5. **Recover.** One session per capability, with read-only tools, is given its placed PRs and changes whole and reads its tests, docs and code comments, and writes its rules, decisions, questions and terms (§6).
6. **Check** and **review**, as today; review gains architecture 12's guide.

## 4. What a capability is

The walk works from the capability definition the owner has accepted for now ("sounds good for now but I'm not fully settled yet"):

> A promise the product makes to its users, stated as intended behaviour: something a user can rely on and would name, true however the code is arranged, and whose breaking someone would report. A capability can be large and is kept by many changes over time; a change that needs two capabilities usually means they are one.

With four tests: a user would name it; it survives a rewrite, so it never follows layers or folders; breaking it is a bug someone reports; a well-made change can belong to it alone. No prompt gives a count. The owner, on promises that cut across layers: "For ME personally, I probably wouldn't architect a capability that way, but we can't put people into a box like that." So init assumes nothing about how a codebase is organized: in a codebase arranged by capability the walk finds promises and folders agree, and in a layered one it gathers each promise from every layer.

## 5. Init's record

The map holds no file paths (vision §4.1), so footprints and placement stay in init's record in `.git/physa`, beside the gather. Learning and architecture 12's review read them. A clone without the record (a teammate's, or one made after init) can rebuild it with the areas, walk and place phases alone, without recovering: on these repos that was $15 to $110 and under 90 minutes.

## 6. The why

A rule needs a reason. Recovery takes reasons from PRs, changes, docs and code comments that say why. A rule whose only anchors are tests or comments that say what, not why, becomes a question asking why **only when the agent judges the reason matters and a person could answer it**; otherwise it is dropped, since the code and tests already hold the what (vision §4.1). The owner agreed: "Your recommendation seems sound to me". Run 2's rule turned every such rule into a question, and left 1,601 questions for 752 rules.

## 7. The PR window

A window of 1,000 PRs reached back one month on Discourse and two and a half on Mastodon, leaving 31% to 47% of capabilities with no PR. Mastodon run 2 gathered 3,000 and held them within the cap. The default becomes a window of time, reaching back far enough for most capabilities to have history, with `--max-prs` and `--since` as today.

## 8. The cap

No estimate. A run has a cap, as the owner asked, and the run stops cleanly when the next session's budget would pass it. Recovery goes in a seeded random order across areas, so a run stopped by the cap leaves a fair sample of the map rather than some areas whole and others untouched, and `physa init --recover` continues where it stopped. Init still says before it starts what a token is worth (02 §2.7) and after it ends what it spent. This replaces 02 §2.7's estimate, its ratio records and its "past the estimate" message.

## 9. What init reports

Its output and PR body report the measures the runs used: files in a capability, shared and unclaimed; PRs and changes placed; PRs in one capability (the change tenet, vision §4.2: "a change belongs to exactly one capability"); capabilities with no PR or change; where the rules' reasons came from; and spend per phase.

## 10. Questions for the owner

Each has a recommended answer, worded so "yes" accepts it.

1. **The phases** of §3 replace propose and the coverage placement, and their code goes. Yes?
2. **The definition and its four tests** go into the walk's skill as written, and into the glossary's Capability entry when you settle the wording. Yes?
3. **Init's record stays in `.git/physa`**, and a clone without it rebuilds it from areas, walk and place, without recovering. Yes?
4. **The why**, as §6. Yes?
5. **The default PR window is 12 months**, with `--max-prs` and `--since` kept. Yes?
6. **The cap is in tokens weighted by price, shown with its cost** like every spend in Physa, given with `--cap`, with none meaning no cap; on a terminal a bare run asks for one. Yes?
7. **Flat folders**: the map stays one folder per capability, with no area level. Yes?
8. **Untested yet**: a repo too big for one areas session (Discourse needed one per framework, which cuts by layer). Recommended: the areas session gets the listing in parts and is told to group by promise across them, and the first such repo is a run before this is built. Yes?
9. **The signal**, for the idea this serves: claim, init on an existing codebase gives a map a person keeps; for, first maps merged with their capabilities mostly kept, and PRs in one capability at 60% or more; against, first maps reshaped heavily or abandoned; window 2027-03-01; resolver, Darin, reading init PRs and the review commands' commits. Yes?
