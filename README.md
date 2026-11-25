# Physa

Physa is product management rebuilt for agents. It treats a product as a network of ideas, and an idea's strength comes from evidence, not from estimates or status fields. The name comes from Physarum, the slime mold whose networks gave Physa its model: tubes that carry flow thicken, and tubes that carry nothing wither. `docs/vision.md` makes the full case.

This repo holds the open-source core: one Go binary, `physa`, that contains two products. **The map** keeps a team's product knowledge in its repo and hands agents the part their task needs. It works on its own. **The engine** tracks ideas, pursuits and evidence, and is built on top of the map. A hosted team layer, for several people and several repos, lives in its own repo.

## Where it stands

Physa is early. What exists today is usable on a real repo, and Physa's own map in `.physa/` is kept with it. Most of the engine is still to be designed. The plan in `docs/plan.md` is the source of truth. As of 2026-11-25:

**Built**

- **The map, read by agents.** `.physa/` holds one folder per capability, each with its rules, decisions and questions. `physa map index` lists the capabilities, and `physa map slice` gives an agent the slice its task needs in one call. `physa check map` and `physa map fmt` keep the map in its format.
- **Changes checked against the map.** Every commit names the capability it serves in a `Capability:` trailer. `physa check` checks a range of changes, making sure each one names a capability that exists in the map as it leaves it, and that the map stays sound.
- **Learning.** `physa map learn` reads merged changes and the agent sessions behind them, then drafts one batched PR to `physa/learn` with proposed map entries for the stewards to review.
- **Init.** `physa init` builds a first map for an existing repo. It finds the repo's own sources, proposes capabilities, then recovers each one's rules, decisions, questions and terms, with every entry citing its source. It estimates its cost and asks before the large spend. On a large repo, the propose step runs in shares, then merges them.
- **The engine, without agents.** One engine runs per machine, with a local store, events and durable intents. You can add ideas and pursue them from the CLI, give a pursuit its plan of changes, count spend from each agent's cost, and collect engineering evidence from git and GitHub.

**Not built yet**

- How strength is worked out, and which evidence counts toward it.
- How a signal is read and resolved.
- How a team plugs in its own workflow.
- Sharing ideas and pursuits between people. `physa sync` is a placeholder.
- Runs: agents doing a pursuit's work through a harness, and the views.

Designs set aside at the reset of 2026-11-23 are in `docs/archive/`. They're reference, not settled.

## Install

On macOS or Linux, install the latest release to `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/dphaener/physa/main/install.sh | sh
physa version
```

The script checks the download against the release's checksums and never uses sudo. Set `PHYSA_INSTALL_DIR` to install somewhere else. Every merge to main is released, so this is always what's on main.

To build from source instead, with Go 1.26 or newer:

```sh
git clone https://github.com/dphaener/physa.git
cd physa
go build -o ~/.local/bin/physa ./cmd/physa
```

The map commands need only git. `init` and `map learn` run agent sessions through [Claude Code](https://claude.com/claude-code), so its `claude` command must be on your PATH and signed in. Anything that reads or opens PRs (`--pr`, learning's PR signals, the evidence of merged PRs) goes through the [`gh`](https://cli.github.com) CLI, signed in to GitHub.

## A short tour

Run these from inside a git repo. Every command takes `--help`, and most take `--json`.

```sh
physa init                       # estimate, ask, then gather, propose and recover a map
physa init --write --pr          # commit the map to physa/init and open its PR

physa map                        # the index: every capability with its promise
physa map slice slices           # what an agent needs before changing the slices capability
physa map slice --entry agent-chooses   # one entry, by its slug
physa check                      # check the map, and origin/main..HEAD against it
physa map hooks                  # stamp Session: on commits made in Claude Code
physa map learn --write --pr     # draft what merged changes teach the map, as a PR

physa idea add "Retry webhooks for a day" --slug retry-webhooks
physa pursue retry-webhooks      # name its signal: claim, direction, window, resolver
physa pursuit show
physa evidence
```

The engine starts on its own when a command needs it. `physa engine` shows its status, and `physa engine stop` stops it. Its settings, the list of repos it serves and its socket are in `~/.config/physa`, or `$XDG_CONFIG_HOME/physa` if that's set. Each repo's records are in one SQLite file under the repo's git directory (`.git/physa/physa.db`). It's never committed and every worktree shares it.

`.github/workflows/ci.yml` shows the map checks running in CI. `docs/recipes/learn-on-github.md` sets up learning on GitHub.

## Reading further

- `.physa/glossary.md`: the words Physa uses for its concepts, and the ones it avoids. The docs and the CLI use no others.
- `docs/vision.md`: what Physa is and why, and the core it defends (§5).
- `docs/architecture/`: the settled designs, numbered in order.
- `docs/plan.md`: what's built and what comes next.
- `docs/development.md`: building, testing and changing Physa.
- `AGENTS.md`: the rules an agent working in this repo follows. Most of them bind people too.

## License

Apache-2.0. See `LICENSE` and `NOTICE`.
