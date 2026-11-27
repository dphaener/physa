# Architecture 15: code-first init

Draft 0.2, 2026-11-26; settled with the owner, question by question (§10). Uses `.physa/glossary.md` terms only. Serves the owner's idea `code-first-init`. It replaces 02 §2.3 (propose), §2.4's sources, §2.7 (the estimate) and the coverage placement built on them; gather (§2.2), check (§2.5) and review as a PR (§2.6) stay.

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
gather ─▶ areas ─▶ walk each area ─▶ place ─▶ recover each capability ─▶ consolidate ─▶ check ─▶ review as a PR
 (02 §2.2)  1 session   1 session per area   no session  1 session per capability   1 per area      (02 §2.5)  (02 §2.6, 12)
```

1. **Gather**, as today (02 §2.2), with a larger PR window (§7).
2. **Areas.** One session reads the list of every source file and divides the repo into areas, each a group of promises with the files that serve them, by promise and across layers. Every source file falls in exactly one area. When the listing is more than one session holds, the session gets it in parts and groups by promise across all of them, never one area set per framework (Q8; Discourse is the first repo run this way, before this is built). An area is init's own scaffolding: it never appears in the map and needs no glossary term (owner: "area is a temporary grouping that drops off after we're done initializing").
   Built as the change areas: `physa init --areas` lists the repo's tracked files and leaves out those marked `linguist-vendored` or `linguist-generated` in `.gitattributes`, binary files (a NUL in the first 8000 bytes) and well-known lock files. One session then names the repo's test, doc and other files as path patterns, and code applies them. `dir/` takes every file under the folder. A pattern with no `/` matches a base name in any folder. Otherwise `*`, `?` and `[...]` match within one path segment, and `**` matches across segments. A file goes to the kind of its most specific pattern, a tie going to test, then doc, then other; a file no pattern names is source. A pattern that matches no file is reported as unused. A pattern that matches every file is dropped, since it would leave no source, and when no source is left the phase stops. The source files then go to one session, or, when the listing is larger than the part size, to parts of up to 70,000 characters (less where the agent's window is smaller). Each part's prompt says it is one of several parts, and gives the overview of every source folder, the areas so far and the part's files; a merge session follows the parts. Every source file must land in exactly one area: a file is claimed by its most specific claim, a tie claims it twice, and a file left out or claimed twice goes back to the same session, for up to two fix rounds, before the phase fails. The record in `.git/physa` holds the split and the areas for the later walk to read; a new gather clears them, and nothing is written to the map. `--areas` is a phase alone, not yet in the full run.
3. **Walk.** One session per area, with read-only tools over the area's code, tests and docs, names its capabilities, each a promise with a footprint: the source files it owns, and the tests and docs that bear on it. Every source file of the area is owned by one capability or named shared with a reason; none is left unclaimed. A session may split its area once into smaller ones, each walked on its own.
4. **Place.** No session. Each gathered PR and change goes to the capabilities that own most of its files: a source file counts whole, a test or doc shared by k capabilities counts 1/k, and the largest are taken until half the weight is covered, at most three. Placement is a fact of the footprint, not a judgment.
5. **Recover.** One session per capability, with read-only tools, is given its placed PRs and changes whole and reads its tests, docs and code comments, and writes its rules, decisions, questions and terms (§6).
6. **Consolidate** (Q10, the owner's: "we may need another pass at the end that de-dupes or merges rules that are similar. We may end up with noise."). Exact duplicates are found without a session. Then one session per area, given only its capabilities' entries, merges near-duplicates keeping every anchor, moves a rule to the capability it belongs to, and drops a question another entry answers. Rules repeated across areas are lifted to the product's rules. It never writes a new entry, and init's report says what it merged, moved, dropped and lifted.
7. **Check** and **review**, as today; review gains architecture 12's guide.

## 4. What a capability is

The walk works from the capability definition, settled by the owner ("I think that definition sounds right") and taken into the glossary's Capability entry:

> A promise the product makes to its users, stated as intended behaviour: something a user can rely on and would name, true however the code is arranged, and whose breaking someone would report. A capability can be large and is kept by many changes over time; a change that needs two capabilities usually means they are one.

With four tests: a user would name it; it survives a rewrite, so it never follows layers or folders; breaking it is a bug someone reports; a well-made change can belong to it alone. No prompt gives a count. The definition lives in the code, in the walk's skill, and a test fails when the glossary's Capability entry stops matching it (Q2, the owner: "code as source with a drift test"), so a change to the definition is one code PR that touches both, and is run before it ships. The glossary is never read into a prompt: Physa's map does not drive the binary. The owner, on promises that cut across layers: "For ME personally, I probably wouldn't architect a capability that way, but we can't put people into a box like that." So init assumes nothing about how a codebase is organized: in a codebase arranged by capability the walk finds promises and folders agree, and in a layered one it gathers each promise from every layer.

## 5. Init's record

The map holds no file paths (vision §4.1), so footprints and placement stay in init's record in `.git/physa`, beside the gather. Learning and architecture 12's review read them. A clone without the record (a teammate's, or one made after init) can rebuild it with the areas, walk and place phases alone, without recovering: on these repos that was $15 to $110 and under 90 minutes.

## 6. The why

A rule needs a reason where its sources give one. Recovery takes reasons from PRs, changes, docs and code comments that say why. Legacy code won't always say why, and the owner: "I don't know that we will ALWAYS get a why on legacy codebases, that is a lot to ask." So (Q4):

- **A rule backed only by tests or comments stays a rule when its source shows intent**: a test named for a regression, a comment saying what would break, a test tied to a PR. Its `Because:` says what the source shows. A test is strong evidence a rule holds; the owner: "a rule backed by a test or comment is pretty defensible because of where it came from".
- **It is dropped when it only restates the code**, since the code and tests already hold the what (vision §4.1).
- **A question is only for an important gap a person could fill**, never for every rule without a PR. Mastodon run 2 turned every such rule into a question and left 1,601 questions for 752 rules.

## 7. The PR window

A window of 1,000 PRs reached back one month on Discourse and two and a half on Mastodon, leaving 31% to 47% of capabilities with no PR. Mastodon run 2 gathered 3,000 and held them within the cap. The default becomes twelve months of history (Q5), with `--max-prs` and `--since` as today and gather's top-up for quiet areas kept.

Built as the change gather-window: a bare gather asks for twelve months with no PR limit, and `--max-prs N` still stops at N. Twelve months of a large repo is enough requests to meet the forge's rate limits, so gather lists merged PRs a page at a time and waits a limit out as GitHub's docs say ([REST](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#exceeding-the-rate-limit), [GraphQL](https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api#exceeding-the-rate-limit)): the `retry-after` given, else until the primary limit's reset, else a minute doubling on each repeat, up to 65 minutes in all. A gather the forge still stops is stored with every PR it listed, init says how far back they reach and why it stopped, and exits non-zero. The store marks it stopped with the date its PRs reach back to and the reason; `--propose` and `--recover` on it warn of that and still run, and a later gather starts again (02 §2.2). Resuming is the idea gather-resumes, not built.

## 8. The cap

No estimate. A person may give a run a cap, as the owner asked, and the run stops cleanly when the next session's budget would pass it. The cap is the person's to define, in the terms they pay in. The owner: "Subscription users probably don't care about a dollar amount but rather a percentage of their usage, or they may not want a cap at all, same goes for API users. This should be flexible." So a cap is one of:

- **none**, the default: init runs to the end;
- **an amount of money**, for people who pay by the token;
- **a share of their plan's usage**, for people on a subscription, written as a ceiling (`--cap 80%`: start nothing that would take the plan's usage past 80%). Claude Code reports it: `claude -p /usage --output-format stream-json --verbose` prints a `usage_report` whose rate limits give each window's percent spent and reset time, which the owner's afk reads to pause its runs near the limit;
- **tokens**, weighted by price as every spend in Physa is.

Whatever the unit, init counts spend as it always does and shows it in the unit the cap was given in. A bare run never asks for a cap (Q6): `--cap` is there for whoever wants one. Recovery goes in a seeded random order across areas, so a run stopped by the cap leaves a fair sample of the map rather than some areas whole and others untouched, and `physa init --recover` continues where it stopped. Init still says before it starts what a token is worth (02 §2.7) and after it ends what it spent. This replaces 02 §2.7's estimate, its ratio records and its "past the estimate" message.

## 9. What init reports

Its output and PR body report the measures the runs used: files in a capability, shared and unclaimed; PRs and changes placed; PRs in one capability (the change tenet, vision §4.2: "a change belongs to exactly one capability"); capabilities with no PR or change; where the rules' reasons came from; and spend per phase.

## 10. Settled with the owner (2026-11-26)

Walked through one by one; sources in the owner's words.

1. **The phases** of §3 replace propose and the coverage placement, and their code goes. "Yes to Q1".
2. **The definition** of §4 is settled, goes into the glossary's Capability entry now, lives in the walk's skill as the source, and a drift test holds the glossary to it. "Yes, code as source with a drift test".
3. **Init's record stays in `.git/physa`**, and a clone without it rebuilds it from areas, walk and place, without recovering. "Yes to Q3". The hosted layer may later keep it for a team.
4. **The why**, as §6, revised from the draft with the owner. "Your recommendation on 1 is probably good enough. 2. Yes 3. Yes".
5. **Twelve months of PRs by default.** "Yes to Q5".
6. **The cap is the person's choice of unit**, none by default and never asked for, as §8. "Yes to Q6".
7. **Flat folders**: one folder per capability, with no area level. "Q7 - Yes".
8. **A repo too big for one areas session** gets its listing in parts, grouped by promise across them, and is run before this is built. "Q8 - Yes".
9. **The signal** for `code-first-init`: claim, init on an existing codebase gives a map a person keeps; for, first maps merged with most of their capabilities kept; against, first maps reshaped heavily or abandoned; window 2027-03-01; resolver, Darin, reading init PRs and the review commands' commits. "Yes to Q9, drop the 60%": PRs in one capability stays a measure init reports (§9), not the signal.
10. **Consolidation** before check, as §3 step 6, added from the owner's ask. "It can be a new question, but we can also just answer it now."
