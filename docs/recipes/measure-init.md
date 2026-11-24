# Recipe: measure init on a repo whose map stays private

A recipe for trying `physa init` on a codebase whose code and map can't leave it, such as a work repo, while bringing back the numbers that tune init: each phase's time and tokens, what it read and found, and the repo's shape. `tools/initstats` runs init's three phases and prints a record built from an allowlist: counts, tokens, seconds, statuses, model names and file extensions. No name, path, promise, question or line of map text can reach it.

## Keep the map local

Init without `--write` commits nothing: the phases keep their work in the repo's store (`.git/physa`). To keep a map you build from it out of git without changing the repo, list it in `.git/info/exclude`, which is never committed:

```sh
printf '.physa/\n' >> .git/info/exclude
```

`.gitignore` would be a change to the repo itself.

## Run it

Build it once from your clone of Physa, the one you build `physa` from (the record notes that clone's commit, since source builds of `physa` report `dev`):

```sh
go -C /path/to/physa build -o ~/bin/initstats ./tools/initstats
```

Then, with `physa` on the PATH, in the repo to measure:

```sh
initstats -label repo-a > /tmp/repo-a.json
```

`-label` is the repo's only name in the record: lowercase letters, digits and dashes. `-dir` runs it on another folder, `-agent` passes `--agent` to init, `-physa` names another binary. `-since` and `-max-prs` pass a shorter window to gather's `--since` and `--max-prs`: on a repo whose history or PRs are too large for propose's one session, they read fewer of them, each still whole. The record shows the bound used.

Each phase's raw output stays in `.git/physa/initstats/<time>/`, with its time and exit in `meta.json`. A phase that fails ends the run, and the record holds what ran. `-from` prints the record of a kept run again without spending anything:

```sh
initstats -label repo-a -from .git/physa/initstats/<time>
```

## Bring the record back

Read the record before it leaves: it is short JSON. Then share it with whoever is tuning init. Records of private repos stay out of this repo.

## What the record holds

- `repo`: files init could read (tracked, or untracked and not ignored), their bytes, and the ten commonest extensions.
- `phases`: each phase's wall time in seconds and its exit code.
- `gather`: the docs, history and PRs gathered, discovery's counts and tokens, and the estimate of the sources' tokens.
- `proposal`: how many capabilities and questions were proposed, dropped or malformed, the prompt's and the sources' tokens, and the session's model and tokens.
- `recovery`: tokens used, and per capability, by position and never by name, its status, attempts, model, tokens, what it recovered and how much was dropped.
