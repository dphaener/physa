# Architecture 13: Physa's skill

Draft 0.1, 2026-11-26; settled with the owner, who chose one skill shipped the way Rex ships its own, added Skill to the glossary, and accepted every recommendation in §7. Uses `.physa/glossary.md` terms only. Serves the owner's idea `ship-core-skills`: "Ship skills for all of Physa's core elements, so a person's agent can drive each one."

Today a person's agent learns about Physa from the pointer init writes into `AGENTS.md` (01 §7): two lines saying to read the index and a slice. Everything else (anchoring a change, the trailers, what only a person may do, reviewing init's map, adding an idea) it has to find in `--help` or get wrong. Physa's own steps have skills (02 §4); a person's agent has none.

## 1. Rex's model

Rex teaches agents to use it with one skill and one hook, and the owner likes how it works:

- `rex skill setup` installs, once per account, a stub of about a dozen lines into each agent it finds: Claude Code in `~/.claude/skills/rex`, and Codex, OpenCode and Pi in `~/.agents/skills/rex`. The stub has the skill's name, the description that tells the agent when to load it, and one instruction: run the installed `rex` to print the guide and follow it. If that fails because the installed Rex is older than the stub, it falls back to `--help`.
- The guide's text lives in the binary, so upgrading Rex updates what every agent is told, and nobody edits a copy.
- Where the agent supports hooks, a SessionStart hook tells each session where it is running.
- `rex skill status` shows each agent's skill and hook as current, stale or missing; `rex skill show` prints the guide; `rex skill remove` takes them out.
- The guide is one document: Rex's terms, recipes named after what people ask for, how to verify a result, and where to look for more (`--help`, the API's own descriptions, the docs).

## 2. Commands

`physa skill`, a glossary noun with verbs (01 §8):

- `physa skill setup` installs the stub and the hook (§4) into every agent it supports on this account. Claude Code first, through its adapter; then the agents that read `~/.agents/skills`.
- `physa skill status` shows, for each agent, whether its stub and hook are current, stale or missing.
- `physa skill show` prints the guide.
- `physa skill remove` takes the stub and hook out, and nothing else.

On a terminal they use Charm's libraries (03 §16): setup lists what it will install and asks to confirm. Off a terminal, plain text or `--json`.

Setup is per account, not per repo: the same stub serves every repo, and the guide says what to do when a repo has no map. The `AGENTS.md` pointer stays, for agents without skills and for teammates who haven't run setup.

## 3. The guide

One skill, as the owner chose, not one per part of Physa. The guide's parts:

- **When it applies**: a repo with a `.physa/` folder. Without one, `physa init` is how a map starts.
- **Physa's terms**: the glossary words an agent uses most, and the ones to avoid.
- **Recipes**, named after what a person asks or a task needs:
  - before changing code: the index, then the slice;
  - a change that seems to need two capabilities: split it, or ask;
  - committing: the `Capability:` trailer and what the hooks add;
  - working a pursuit's planned change: `physa pursuit work`, and what only a person may do (pursue, stop);
  - adding an idea, and shaping it into a pitch;
  - running init, and answering its questions;
  - reviewing init's map (architecture 12);
  - reviewing learning's proposals.
- **Checking the work**: `physa check` before saying it's done, and what its findings mean.
- **Finding more**: `--help` for every command, and the docs.

The slice skill (09 §5) stays in the slice's output, where an agent needs it, and the guide points to it. Architecture 12's `reviewing` skill becomes the guide's recipe for reviewing init's map, not a skill of its own.

The guide is held to the same discipline as the map: no size budget until a real guide shows it needs one.

## 4. The session hook

Where an agent supports a hook at the start of a session, setup installs one that runs `physa`. In a repo with a map it prints a few lines: that there is a map and how many capabilities it has, the pursuit and change this session works on when there is one (`physa pursuit work --current`), and the reminder to read the slice before changing code. Outside a repo with a map it prints nothing. It never fails the session: it has a short timeout, and any error is silent.

It is not `physa map hooks`, which points the repo's git hooks at the one that stamps `Session:` on commits. That is per repo and for git; this is per account and for the agent.

## 5. Init

When `physa init` finds no Physa skill installed for the agent it runs with, it says so in its output and its PR body, with the line to run. It never installs it: that is the person's account, not the repo.

## 6. The glossary

Added with this draft, as the owner chose (2026-11-26):

> **Skill**: A guide Physa ships in its binary to teach an agent its part: the one a person's agent loads to use Physa's commands and terms as Physa means them, and one for each of Physa's own agent steps (mapping, recovering, learning).

01 §8 makes command groups glossary nouns, so `physa skill` needs the word.

## 7. Settled with the owner (2026-11-26)

The owner answered "Yes to all of these", accepting each recommendation:

1. **Setup is per account**, and the `AGENTS.md` pointer stays as the fallback.
2. **Claude Code first**, then the agents that read `~/.agents/skills`.
3. **The session hook** as §4: a few lines in a repo with a map, silent elsewhere, never failing.
4. **Architecture 12's `reviewing` skill becomes a recipe** in the one guide.
5. **The slice skill stays in the slice's output**, and the guide points to it.
6. **Init says when the skill is missing** and never installs it.
