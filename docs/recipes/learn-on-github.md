# Recipe: learn on GitHub

A recipe to copy, not something Physa installs (`physa init` does not add it). It runs `physa map learn` after every push to the default branch, in a GitHub Actions workflow with the agent installed. See `docs/architecture/08-learning.md` §6 and §9.

Save this as `.github/workflows/learn.yml`:

```yaml
name: learn

on:
  push:
    branches: [main]

concurrency:
  group: physa-learn
  cancel-in-progress: false

permissions:
  contents: write
  pull-requests: write

jobs:
  learn:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: stable

      - uses: actions/setup-node@v4
        with:
          node-version: 22

      - name: Install Physa
        run: go install github.com/dphaener/physa/cmd/physa@latest

      - name: Install the agent
        run: npm install -g @anthropic-ai/claude-code

      - name: Set the identity for the learning commit
        run: |
          git config user.name "physa-learning"
          git config user.email "physa-learning@users.noreply.github.com"

      - name: Learn
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          GH_TOKEN: ${{ github.token }}
        run: $(go env GOPATH)/bin/physa map learn --write --push --pr --appetite 40000
```

The command is:

```
physa map learn --write --push --pr --appetite <tokens>
```

Replace `main` with your default branch and `40000` with the tokens you are willing to spend on one run.

## What each step is for

- **`on: push`** to the default branch: every merged change is a chance to learn. A run learns from every change since the last one it recorded, so a missed run loses nothing.
- **`concurrency`** lets one learn run go at a time and never cancels one that has started. A queued run waits, then learns only what is new.
- **`permissions`**: `contents: write` pushes the `physa/learn` branch; `pull-requests: write` opens and refreshes the PR.
- **`fetch-depth: 0`** gives Physa the whole history, which it needs to find the last change learned from and the branch's earlier commits.
- **Go and `go install`** build the `physa` binary. **Node and `npm install`** put the agent (Claude Code) where Physa can call it.
- **The git identity** is the author of the learning commit; git refuses to commit without one.
- **`ANTHROPIC_API_KEY`** is the agent's credential, from your repository secrets. **`GH_TOKEN`** lets Physa's forge adapter use `gh` to open the PR.
- **`--appetite`** is the most tokens the run may spend, counted in and out, shown before the draft starts. The circuit breaker stops the run at the limit.

## What to expect

- Physa **never merges** the PR (`no-self-merge`). A steward accepts, edits or drops proposals like any PR.
- A PR created with the workflow token **does not trigger other workflows**, so your checks will not run on it automatically. Close and reopen it, or use a personal access token or a GitHub App token as `GH_TOKEN`, if you need them to.
- A run with **nothing to learn costs no tokens**: most changes carry no signal, and a run with no signals never starts the agent.
- **No proposals, no PR.** A run whose changes yielded no proposals still records how far it has learned, on the branch, but opens no PR.
- **Exit codes**: `0` the run wrote what it had to write, or had nothing to write; `1` the circuit breaker stopped the run and nothing was written; `2` the run failed. The workflow fails on `1` and `2`.
