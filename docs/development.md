# Developing Physa

How to build, test and change the core. `AGENTS.md` holds the rules every change follows, for agents and people alike. This guide doesn't repeat them. It covers what you need on your machine and what each check is for.

## What you need

- Go 1.26 or newer (`go.mod` sets the version).
- git.
- For changes to `init` or learning, which run agent sessions: [Claude Code](https://claude.com/claude-code), with `claude` on your PATH. The tests use fakes and never start a real session, so you only need it to try those commands by hand.
- For anything that touches the forge: [`gh`](https://cli.github.com), signed in. The tests fake this too.

## The loop

```sh
go build ./...
go test ./...
go run ./cmd/physa map hooks      # once per clone, so your commits carry Session:
```

Before you push, run what CI runs (`.github/workflows/ci.yml`). Each of these has to pass:

```sh
go vet ./...
go test ./...
go run ./tools/codecheck          # import rules, size budgets, package docs
go run ./cmd/physa check map      # Physa's own map is sound
go run ./cmd/physa map fmt --check
go run ./cmd/physa check          # your branch's changes against the map
```

CI runs all of these except the last. It doesn't check trailers yet, so `physa check` (range `origin/main..HEAD` by default) is how you catch a missing or wrong `Capability:` trailer before review does.

A few timing tests (`TestLatency` in `internal/mapindex` and `internal/mapslice`) check that the map answers within the time an agent expects. On a loaded machine they can fail. Run them alone before you believe the failure.

## Before you change code

Read the map for the capabilities your change touches:

```sh
go run ./cmd/physa map index
go run ./cmd/physa map slice <capability>...
```

The slice gives the rules in full and the headings of the decisions and questions. Take the entries whose headings bear on your change with `map slice --entry <slug>`. If your change would break a rule or reverse a decision, ask the owner first. Then read the design in `docs/architecture/` that covers the area.

## Layout

```
cmd/physa/        the command line: one file per command group, and the golden test
internal/         every package of the core, each with one job
tools/codecheck/  the code rules CI enforces
tools/hooks/      the prepare-commit-msg hook that stamps Session: and Change:
tools/initstats/  measures a run of init on a repo whose map must stay private
.physa/           Physa's own map
docs/             vision, designs, the plan, recipes and research
```

The packages under `internal/` fall on two sides. `tools/codecheck` enforces the line between them.

- **The engine's side** is the list `engineSide` in `tools/codecheck/main.go`. It covers the engine process and its API (`engine`, `enginestart`, `api`, `apischema`, `workapi`, and the public `client` at the module's root), its state (`state` and the packages under it, `repos`), what ideas and pursuits are made of (`plan`, `progress`, `signal`, `tokens`, `evidence`), and the commands over all of it (`ideacli`, `pursuitcli`, `evidencecli`, `enginecli`). A new engine package goes on that list.
- **The map's side** is every other package. It covers reading and checking the map (`mapfile`, `mapfolder`, `mapcheck`, `mapindex`, `mapslice`), the change check (`history`, `prcheck`), init (`gather`, `discovery`, `mapping`, `shares`, `reading`, `recovery`, `recovering`, `initcost`, `initrun`, `initbranch`, `newcodebase`) and learning (`learn`, `learnsession`, `draft`, `propose`, `learnbranch`, `learnreport`). It also holds the foundations both sides use: version control (`vcs`, `vcs/git`), the forge (`forge`, `forge/github`), the agent adapter (`adapter`, `adapter/claude`), the store, output, spend, and the command glue (`command`).

codecheck also enforces these rules:

- Nothing under `internal/` imports `cmd/`.
- Nothing on the map's side imports the engine's side, so the map works with no engine at all.
- Only `internal/vcs/git` runs git.
- Only `internal/state` writes the engine's tables.
- Every package has a doc comment stating its promise. `go doc ./internal/<pkg>` is the quickest way to learn what a package is for.

## Commands

Commands are built on Cobra through `internal/command`. A command is made with `command.New` and its flags with `command.Declare`. A flag that's on or off is a `command.Switch`: bare `--x` turns it on, and nothing else is accepted except `--x=true`. Each command's usage lines in `--help` are written by hand. A drift test fails when they name a flag the command doesn't declare, or the command declares one they don't name.

`cmd/physa/testdata/cli.golden` records what every command line prints and how it exits. When you change a command's output on purpose, rewrite the golden file and check the diff line by line:

```sh
go test ./cmd/physa -run TestCommandLineGolden -update
git diff cmd/physa/testdata/cli.golden
```

Any golden line that changes goes in the PR description, with the reason. Help text quoted in `docs/` has to stay true.

## Budgets

Every package stays under 800 non-blank lines of non-test Go. When a package goes over, cut its scope or split it along a real seam. The budget itself stays where it is. `go run ./tools/codecheck` names any package over budget, with its size.

## Commits and PRs

- One small change per commit, usually one commit per PR. Stack PRs when a piece of work takes several.
- Messages follow Conventional Commits (`type(scope): Subject`, with a short prose body). Older commits in the history don't, so don't copy them.
- Each commit carries `Capability: <folder>` naming the one capability it serves, from `.physa/capabilities/`. A change that seems to need two should be split.
- `Session:` and `Change:` are added by the hook, never by hand.
- Use the glossary's words in commits, PRs, docs and code. See `.physa/glossary.md`.

When a change settles something, record it where it belongs: a design in `docs/architecture/`, what's built in `docs/plan.md`, and a rule or decision in the map for the capability. The glossary changes only when the owner means to change it.

## Trying it on another repo

Build a binary to a scratch path and run it from inside the other repo:

```sh
go build -o /tmp/physa ./cmd/physa
cd ../some-repo && /tmp/physa init
```

`init` keeps its phases' work in the store and changes no file in the working tree. `--write` commits the map to the `physa/init` branch, never to the branch you're on. To measure a run on a repo whose map must stay private, see `docs/recipes/measure-init.md`.
