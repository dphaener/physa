# Architecture 12: review after init

Draft 0.1, 2026-11-26; proposed by Myxo for the owner to review. Nothing here is settled. Uses `.physa/glossary.md` terms only. Serves the idea `review-after-init`, the owner's: "for init, we clearly can't be striving for perfection. We should be striving for a good starting point. … Make it easy for users to review and update capabilities after the first init so they can have a clean map to start with."

Today a person reviews init's map by editing its PR by hand (02 §2.6). That works for rewording a promise or deleting a rule. It breaks down on the reshaping a first map most often needs: merging two capabilities that are one promise, splitting one that is two, renaming, dropping. Each moves entries between folders, has to keep slugs unique and contents and links right, and leaves init's record of which files and PRs each capability covers out of date. By hand, that is where the map breaks, and why a person gives up and keeps the map init wrote.

This design gives the core what one person needs to reshape a first map safely, and says what the hosted layer adds.

## 1. The line

Vision §11: the core is "everything one developer or one repo needs, fully working". One developer reviewing their first map is that, so the core has:

- a review guide in init's PR, saying where to look first (§2);
- commands that reshape the map and keep it valid (§3).

The hosted layer adds review as a team (§5).

## 2. Where to look first

Init's PR body gets a section, *Where to look*, built from what init already knows. It names candidates with their reasons and decides nothing.

- **Capabilities that change together.** Pairs that the gathered PRs often touch together, with the count. A change belongs to exactly one capability (vision §4), so a pair most changes need is probably one promise: a merge candidate.
- **PRs that land in one capability**, as one share for the whole map. On three public repos it was 73%, 57% and 39%; a low share says the capabilities are cut too fine or along the wrong lines.
- **Capabilities no PR or change reached.** Recovered from code, tests and docs alone, they are the thinnest and the likeliest to be wrong.
- **Capabilities over a size limit or near it** (09 §6): split candidates.
- **Capabilities that are mostly questions**: where init found what the code does but not why. A person who knows the why answers them.

## 3. Commands

Under `physa map`, as verbs on the map (01 §8: groups are nouns, subcommands verbs, one way to do each thing).

- `physa map merge <capability> <capability>... --into <name>` makes one capability of several. Their rules, decisions, questions and terms are kept, each kind in its file. The promise is written by the person: in `$VISUAL` or `$EDITOR` on a terminal, seeded with the old promises, or with `--promise`. Slugs that collide are renamed and listed. Stewards are combined.
- `physa map split <capability>` makes two. On a terminal a picker lists the entries and the person chooses which go to the new capability, then names it and writes its promise. Off a terminal, or for an agent, `--from <file>` lists the slugs that move, with the name and promise.
- `physa map rename <capability> <name>` renames a capability's folder and every reference to it.
- `physa map drop <capability>` removes a capability with its entries. `--into <capability>` moves its entries there instead.

Rewording a promise or an entry stays an edit by hand: it touches one file, and the check catches mistakes.

Each command:

- writes the map in its format (`physa map fmt`) and runs the checks of `physa check map`, refusing to leave a map that fails them, with the reason (a merge over a size limit names the entries to prune, or proposes the split);
- changes the working tree only. The person reviews the diff and commits, on init's branch or after, as with any change to the map;
- updates init's record in `.git/physa` of the files, PRs and changes each capability covers, so learning keeps placing new changes on the right capability.

An agent can run the same commands. A person can ask their agent to work through *Where to look* and propose each merge or split with its reason, then accept or reject them one by one.

## 4. After changes name a capability

Right after init no change names a capability: changes made before the map are exempt from the change check (02 §2.6). So renaming, merging and dropping during the first review break nothing.

Later they would. Older commits' `Capability:` trailers would name a capability that no longer exists, and `physa map changes` would report them. That needs a way to follow a capability through a rename or merge, which this draft leaves open (Q5).

## 5. Hosted

The web app for people who don't live in git (vision §11) holds the team's review: the index and each capability's page, comments on a capability or an entry, the *Where to look* candidates, and each reviewer's sign-off. A merge, split, rename or drop chosen there becomes a commit on init's branch through the GitHub App, made with the same rules as §3. Agents run on the team's side, never in the hosted layer.

## 6. Questions for the owner

Each has a recommended answer, worded so "yes" accepts it.

1. **The line.** The guide and the commands are core; review as a team is hosted. Yes?
2. **Four commands under `physa map`**: `merge`, `split`, `rename`, `drop`, with promise rewording left to the editor. Yes?
3. **Working tree only.** The commands never commit; the person does. Yes?
4. **The guide in init's PR body**, with the five kinds of candidate in §2. Yes?
5. **Following a capability after a rename or merge** (§4). Recommended: decide it when a real map first needs it. Until then, `rename`, `merge` and `drop` warn when changes already name the capability, and go ahead. Yes?
6. **Size limits stay hard.** A merge that would put `rules.md` over 150 lines fails and names what to prune or split, though a capability "can be large". Yes?
