# Architecture 12: review after init

Draft 0.2, 2026-11-26; settled with the owner, who accepted every recommendation in §6. Uses `.physa/glossary.md` terms only. Serves the idea `review-after-init`, the owner's: "for init, we clearly can't be striving for perfection. We should be striving for a good starting point. … Make it easy for users to review and update capabilities after the first init so they can have a clean map to start with."

Today a person reviews init's map by editing its PR by hand (02 §2.6). That works for rewording a promise or deleting a rule. It breaks down on the reshaping a first map most often needs: merging two capabilities that are one promise, splitting one that is two, renaming, dropping. Each moves entries between folders, has to keep slugs unique and contents and links right, and leaves init's record of which files and PRs each capability covers out of date. By hand, that is where the map breaks, and why a person gives up and keeps the map init wrote.

This design gives the core what one person needs to reshape a first map safely, and says what the hosted layer adds.

## 1. The line

Vision §11: the core is "everything one developer or one repo needs, fully working". One developer reviewing their first map is that, so the core has:

- a review guide, `physa map review`, saying where to look first, with an overview in init's PR (§2);
- commands that reshape the map and keep it valid (§3);
- a skill that lets a person's agent work through the review with them (§3).

The hosted layer adds review as a team (§5).

## 2. Where to look first

`physa map review` lists where to look first, built from the map and init's record in `.git/physa`. It names candidates with their reasons and decides nothing. It reads the map as it stands, so after each merge or split the list is current again.

On a large repo the full list is long: Discourse's 2,335 capabilities would give hundreds of candidates. So the full guide is a command, not the PR body:

- on a terminal, a styled list grouped by kind, where choosing a candidate shows its reasons and offers the command that acts on it;
- `--json` for agents and the skill (§3);
- `--format md` for a person who wants to read it somewhere else.

Init's PR body gets only an overview, *Where to look*: the count of each kind of candidate, the top few of each, and the line to run for the rest. It is not checked in: the map holds only what is still true (vision §4), and a review report is out of date after the first merge.

The kinds of candidate:

- **Capabilities that change together.** Pairs that the gathered PRs often touch together, with the count. A change belongs to exactly one capability (vision §4), so a pair most changes need is probably one promise: a merge candidate.
- **PRs that land in one capability**, as one share for the whole map. On three public repos it was 73%, 57% and 39%; a low share says the capabilities are cut too fine or along the wrong lines.
- **Capabilities no PR or change reached.** Recovered from code, tests and docs alone, they are the thinnest and the likeliest to be wrong.
- **Capabilities over a size limit or near it** (09 §6): split candidates.
- **Capabilities that are mostly questions**: where init found what the code does but not why. A person who knows the why answers them.

## 3. Commands

Under `physa map`, as verbs on the map (01 §8: groups are nouns, subcommands verbs, one way to do each thing).

On a terminal every command is built with Charm's libraries (03 §16), as the owner asked: "No unstyled text and easy to use pickers and forms and confirmations." A capability left out is chosen from a filterable picker; names and promises are typed into forms; anything that removes or moves entries shows what will change and asks to confirm. In a pipe, in CI or for an agent the commands take flags and print plain text or `--json`, and a missing argument is an error listing the choices.

- `physa map merge <capability> <capability>... --into <name>` makes one capability of several. Their rules, decisions, questions and terms are kept, each kind in its file. The promise is written by the person: in `$VISUAL` or `$EDITOR` on a terminal, seeded with the old promises, or with `--promise`. Slugs that collide are renamed and listed. Stewards are combined.
- `physa map split <capability>` makes two. On a terminal a picker lists the entries and the person chooses which go to the new capability, then names it and writes its promise. Off a terminal, or for an agent, `--from <file>` lists the slugs that move, with the name and promise.
- `physa map rename <capability> <name>` renames a capability's folder and every reference to it.
- `physa map drop <capability>` removes a capability with its entries. `--into <capability>` moves its entries there instead.

Rewording a promise or an entry stays an edit by hand: it touches one file, and the check catches mistakes.

Each command:

- writes the map in its format (`physa map fmt`) and runs the checks of `physa check map`, refusing to leave a map that fails them, with the reason (a merge over a size limit names the entries to prune, or proposes the split);
- changes the working tree only. The person reviews the diff and commits, on init's branch or after, as with any change to the map;
- updates init's record in `.git/physa` of the files, PRs and changes each capability covers, so learning keeps placing new changes on the right capability.

An agent can run the same commands, and Physa's skill has a recipe for it, `reviewing` (architecture 13 §3). It reads `physa map review --json` and works through the candidates with the person: it proposes each merge, split, rename or drop with its reason, the person accepts or rejects it, and the agent runs the command. The skill comes from the binary and uses glossary terms only; how it reaches a person's agent is architecture 13.

## 4. After changes name a capability

Right after init no change names a capability: changes made before the map are exempt from the change check (02 §2.6). So renaming, merging and dropping during the first review break nothing.

Later they would. Older commits' `Capability:` trailers would name a capability that no longer exists, and `physa map changes` would report them. That needs a way to follow a capability through a rename or merge, which waits until a real map needs it (§6.5).

## 5. Hosted

The web app for people who don't live in git (vision §11) holds the team's review: the index and each capability's page, comments on a capability or an entry, the *Where to look* candidates, and each reviewer's sign-off. A merge, split, rename or drop chosen there becomes a commit on init's branch through the GitHub App, made with the same rules as §3. Agents run on the team's side, never in the hosted layer.

## 6. Settled with the owner (2026-11-26)

The owner answered "Yes to all seven", accepting each recommendation:

1. **The line.** The guide and the commands are core; review as a team is hosted.
2. **Four commands under `physa map`**: `merge`, `split`, `rename`, `drop`, with promise rewording left to the editor.
3. **Working tree only.** The commands never commit; the person does.
4. **The full guide is `physa map review`**, styled on a terminal, with `--json` and `--format md`. Init's PR body gets only an overview, and nothing is checked in.
5. **Following a capability after a rename or merge** (§4) is decided when a real map first needs it. Until then, `rename`, `merge` and `drop` warn when changes already name the capability, and go ahead.
6. **Size limits stay hard.** A merge that would put `rules.md` over 150 lines fails and names what to prune or split, though a capability "can be large".
7. **A `reviewing` skill ships with this**, for the person's agent. How Physa ships skills to people's agents is designed on its own, with this as the first: the owner's idea `ship-core-skills`. Since settled in architecture 13 §7.4: it is a recipe in Physa's one skill.
